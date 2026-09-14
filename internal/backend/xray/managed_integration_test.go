package xray

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/backend"
	"github.com/design-maestro/fastlane/internal/domain"
)

func TestXrayV26728ManagedSwitchKeepsPIDAndExistingTCP(t *testing.T) {
	source := strings.TrimSpace(os.Getenv("FASTLANE_XRAY_SOURCE"))
	if source == "" {
		t.Skip("set FASTLANE_XRAY_SOURCE to the Xray v26.7.28 source tree")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "xray")
	build := exec.Command("go", "build", "-o", binary, "./main")
	build.Dir = source
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build pinned Xray: %v: %s", err, output)
	}
	version := exec.Command(binary, "version")
	versionOutput, err := version.CombinedOutput()
	if err != nil || !strings.Contains(string(versionOutput), "Xray 26.7.28") {
		t.Fatalf("unexpected Xray build: %v: %s", err, versionOutput)
	}
	t.Setenv("FASTLANE_XRAY_BINARY", binary)

	releaseA := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(releaseA) })
	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "A-first\n")
		flusher.Flush()
		<-releaseA
		_, _ = io.WriteString(w, "A-second\n")
		flusher.Flush()
	}))
	defer serverA.Close()
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "B\n") }))
	defer serverB.Close()
	directServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "DIRECT\n") }))
	defer directServer.Close()
	udpA := startUDPEchoAtHTTPPort(t, serverA.URL, "A-")
	defer udpA.Close()
	udpB := startUDPEchoAtHTTPPort(t, serverB.URL, "B-")
	defer udpB.Close()

	nodeA := freedomRedirectNode(t, "a", serverA.URL)
	nodeB := freedomRedirectNode(t, "b", serverB.URL)
	configPath := filepath.Join(dir, "config.json")
	runtimeBackend := NewRuntimeBackend(configPath, nil)
	runtimeBackend.tester = CommandTester{BinaryPath: binary}
	req := backend.ConfigRequest{Nodes: []domain.Node{nodeA}, SelectedNodeID: nodeA.ID, SOCKSPort: 10808, HTTPPort: 10809}
	if err := runtimeBackend.PersistConfig(context.Background(), req); err != nil {
		rendered, _ := runtimeBackend.GenerateConfig(req)
		t.Fatalf("persist initial config: %v\n%s", err, rendered)
	}

	process := exec.Command(binary, "run", "-config", configPath)
	if output, err := process.StderrPipe(); err == nil {
		go func() { _, _ = io.Copy(io.Discard, output) }()
	}
	if err := process.Start(); err != nil {
		t.Fatalf("start Xray: %v", err)
	}
	defer func() {
		_ = process.Process.Kill()
		_, _ = process.Process.Wait()
	}()
	pid := process.Process.Pid
	var tagA string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, tagA, _ = managedOutboundForNode(nodeA, 0)
		if present, _ := runtimeBackend.outboundPresent(context.Background(), tagA); present {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := runtimeBackend.SelectOutbound(context.Background(), tagA); err != nil {
		t.Fatalf("select initial outbound: %v", err)
	}

	client := proxyClient(t, 10809)
	resp, err := client.Get("http://probe.invalid/stream")
	if err != nil {
		t.Fatalf("open stream through A: %v", err)
	}
	reader := bufio.NewReader(resp.Body)
	if line, _ := reader.ReadString('\n'); line != "A-first\n" {
		t.Fatalf("first stream line = %q", line)
	}
	associationA := newSocksUDPAssociation(t, 10808)
	defer associationA.Close()
	if got := associationA.Exchange(t, "before"); got != "A-before" {
		t.Fatalf("initial UDP used wrong outbound: %q", got)
	}

	tagB, err := runtimeBackend.PrepareOutbound(context.Background(), nodeB, 0)
	if err != nil {
		t.Fatalf("prepare B: %v", err)
	}
	if err := runtimeBackend.SetProbeOutbound(context.Background(), 0, tagB); err != nil {
		t.Fatalf("route probe to B: %v", err)
	}
	if body := httpGetBody(t, proxyClient(t, 10810), "http://probe.invalid/"); body != "B\n" {
		t.Fatalf("probe used wrong outbound: %q", body)
	}
	if err := runtimeBackend.SelectOutbound(context.Background(), tagB); err != nil {
		t.Fatalf("switch to B: %v", err)
	}
	if process.Process.Pid != pid {
		t.Fatalf("Xray PID changed: %d -> %d", pid, process.Process.Pid)
	}
	if body := httpGetBody(t, client, "http://probe.invalid/"); body != "B\n" {
		t.Fatalf("new request used wrong outbound: %q", body)
	}
	if err := runtimeBackend.RemoveOutbound(context.Background(), tagA); err != nil {
		t.Fatalf("remove old outbound: %v", err)
	}
	if got := associationA.Exchange(t, "after"); got != "A-after" {
		t.Fatalf("existing UDP association was interrupted after switch/remove: %q", got)
	}
	associationB := newSocksUDPAssociation(t, 10808)
	defer associationB.Close()
	if got := associationB.Exchange(t, "new"); got != "B-new" {
		t.Fatalf("new UDP association used wrong outbound: %q", got)
	}
	releaseOnce.Do(func() { close(releaseA) })
	if line, _ := reader.ReadString('\n'); line != "A-second\n" {
		t.Fatalf("existing TCP stream was interrupted after switch/remove: %q", line)
	}
	_ = resp.Body.Close()
	if err := runtimeBackend.SelectDirect(context.Background()); err != nil {
		t.Fatalf("select direct fail-open: %v", err)
	}
	directReq := backend.ConfigRequest{Nodes: []domain.Node{nodeB}, SelectedNodeID: nodeB.ID, SOCKSPort: 10808, HTTPPort: 10809, StartDirect: true}
	if err := runtimeBackend.PersistConfig(context.Background(), directReq); err != nil {
		t.Fatalf("persist direct startup config: %v", err)
	}
	if body := httpGetBody(t, client, directServer.URL); body != "DIRECT\n" {
		t.Fatalf("direct fail-open did not bypass VPN outbound: %q", body)
	}
	if err := process.Process.Kill(); err != nil {
		t.Fatalf("stop Xray for direct restart check: %v", err)
	}
	_, _ = process.Process.Wait()
	restarted := exec.Command(binary, "run", "-config", configPath)
	if output, err := restarted.StderrPipe(); err == nil {
		go func() { _, _ = io.Copy(io.Discard, output) }()
	}
	if err := restarted.Start(); err != nil {
		t.Fatalf("restart Xray in direct mode: %v", err)
	}
	defer func() {
		_ = restarted.Process.Kill()
		_, _ = restarted.Process.Wait()
	}()
	if restarted.Process.Pid == pid {
		t.Fatal("restart check did not create a new Xray process")
	}
	if body := waitHTTPBody(t, client, directServer.URL, 10*time.Second); body != "DIRECT\n" {
		t.Fatalf("persisted direct startup leaked into VPN: %q", body)
	}
	if _, err := runtimeBackend.PrepareOutbound(context.Background(), nodeB, 0); err != nil {
		t.Fatalf("prepare B after direct restart: %v", err)
	}
	if err := runtimeBackend.SelectOutbound(context.Background(), tagB); err != nil {
		t.Fatalf("restore VPN after direct mode: %v", err)
	}
	if body := httpGetBody(t, client, directServer.URL); body != "B\n" {
		t.Fatalf("VPN recovery did not restore selected outbound: %q", body)
	}
}

func waitHTTPBody(t *testing.T, client *http.Client, target string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(target)
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr == nil {
				return string(body)
			}
			lastErr = readErr
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for proxy response: %v", lastErr)
	return ""
}

func startUDPEchoAtHTTPPort(t *testing.T, rawURL, prefix string) *net.UDPConn {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: mustPort(t, parsed.Port())})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		buffer := make([]byte, 2048)
		for {
			n, peer, err := conn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			_, _ = conn.WriteToUDP(append([]byte(prefix), buffer[:n]...), peer)
		}
	}()
	return conn
}

func mustPort(t *testing.T, value string) int {
	t.Helper()
	var port int
	if _, err := fmt.Sscanf(value, "%d", &port); err != nil {
		t.Fatal(err)
	}
	return port
}

type socksUDPAssociation struct {
	control net.Conn
	udp     *net.UDPConn
	relay   *net.UDPAddr
}

func newSocksUDPAssociation(t *testing.T, port int) *socksUDPAssociation {
	t.Helper()
	control, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(control, greeting); err != nil || greeting[1] != 0 {
		t.Fatalf("SOCKS greeting failed: %v %#v", err, greeting)
	}
	if _, err := control.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	header := make([]byte, 4)
	if _, err := io.ReadFull(control, header); err != nil || header[1] != 0 {
		t.Fatalf("SOCKS UDP associate failed: %v %#v", err, header)
	}
	host := readSocksHost(t, control, header[3])
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(control, portBytes); err != nil {
		t.Fatal(err)
	}
	relayPort := int(portBytes[0])<<8 | int(portBytes[1])
	if host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	udp, err := net.ListenUDP("udp", nil)
	if err != nil {
		t.Fatal(err)
	}
	return &socksUDPAssociation{control: control, udp: udp, relay: &net.UDPAddr{IP: net.ParseIP(host), Port: relayPort}}
}

func readSocksHost(t *testing.T, reader io.Reader, atyp byte) string {
	t.Helper()
	switch atyp {
	case 1:
		value := make([]byte, 4)
		_, _ = io.ReadFull(reader, value)
		return net.IP(value).String()
	case 4:
		value := make([]byte, 16)
		_, _ = io.ReadFull(reader, value)
		return net.IP(value).String()
	case 3:
		length := []byte{0}
		_, _ = io.ReadFull(reader, length)
		value := make([]byte, int(length[0]))
		_, _ = io.ReadFull(reader, value)
		return string(value)
	default:
		t.Fatalf("unsupported SOCKS address type %d", atyp)
		return ""
	}
}

func (a *socksUDPAssociation) Exchange(t *testing.T, payload string) string {
	t.Helper()
	packet := append([]byte{0, 0, 0, 1, 1, 1, 1, 1, 0, 53}, []byte(payload)...)
	_ = a.udp.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := a.udp.WriteToUDP(packet, a.relay); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 2048)
	n, err := a.udp.Read(response)
	if err != nil {
		t.Fatal(err)
	}
	if n < 10 {
		t.Fatalf("short SOCKS UDP response: %d", n)
	}
	offset := 4
	switch response[3] {
	case 1:
		offset += 4
	case 4:
		offset += 16
	case 3:
		offset += 1 + int(response[4])
	default:
		t.Fatalf("unsupported SOCKS UDP response address type %d", response[3])
	}
	offset += 2
	return string(response[offset:n])
}

func (a *socksUDPAssociation) Close() {
	_ = a.udp.Close()
	_ = a.control.Close()
}

func freedomRedirectNode(t *testing.T, id, target string) domain.Node {
	t.Helper()
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"protocol": "freedom", "settings": map[string]any{"redirect": parsed.Host}})
	if err != nil {
		t.Fatal(err)
	}
	return domain.Node{ID: id, Protocol: domain.ProtocolVLESS, Address: "127.0.0.1", Port: 1, RawOutbound: raw}
}

func proxyClient(t *testing.T, port int) *http.Client {
	t.Helper()
	proxyURL, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}
}

func httpGetBody(t *testing.T, client *http.Client, target string) string {
	t.Helper()
	resp, err := client.Get(target)
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
