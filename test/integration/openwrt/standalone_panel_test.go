package openwrt_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// Real management HTTP -> the daemon's service -> Xray -> kernel AWG. Only the
// disposable QEMU guest is modified. Test keys are generated inside its stand.
func TestOpenWrtStandaloneAWGPanel(t *testing.T) {
	if os.Getenv("FASTLANE_RUN_OPENWRT_INTEGRATION") != "1" {
		t.Skip("requires isolated OpenWrt/QEMU stand")
	}
	duration := 20 * time.Minute
	previewAddress := os.Getenv("FASTLANE_STANDALONE_PREVIEW_ADDR")
	if previewAddress != "" {
		duration = 13 * time.Hour
	}
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	h, err := newOpenWRTHarness(t)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err = h.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Log("Isolated OpenWrt booted")
	if previewAddress != "" {
		// The stock image has a tiny root partition. Keep optional Geo downloads
		// in guest RAM for this disposable, non-persistent interactive stand.
		if err = h.sshCommand(ctx, "mkdir -p /usr/share/xray && mount -t tmpfs -o size=128m tmpfs /usr/share/xray"); err != nil {
			t.Fatal("prepare preview Geo storage: ", err)
		}
	}
	if err = h.sshCommand(ctx, "opkg update && opkg install ca-bundle nftables kmod-nft-tproxy rpcd-mod-file"); err != nil {
		t.Fatal(err)
	}
	if err = h.sshCommand(ctx, "opkg list-installed | grep -q '^dnsmasq-full ' || opkg install dnsmasq-full >/dev/null 2>&1 || { opkg remove dnsmasq && opkg install dnsmasq-full; }"); err != nil {
		t.Fatal("install DNS nftset support for domain exclusions: ", err)
	}
	if err = h.InstallFastLane(ctx); err != nil {
		t.Fatal(err)
	}
	if err = h.InstallXray(ctx); err != nil {
		t.Fatal(err)
	}
	if err = installAWGStandPackages(ctx, h); err != nil {
		t.Fatal(err)
	}
	if err = configureAWGNamespaceStand(ctx, h); err != nil {
		t.Fatal(err)
	}
	t.Log("Xray and AWG namespace server installed")
	if err = h.sshCommand(ctx, fastlaneRemoteBinary+" settings set refresh-interval 24h && "+fastlaneRemoteBinary+" settings set health-check-interval 0s"); err != nil {
		t.Fatal(err)
	}
	if err = h.AssertManagementHTTP(ctx); err != nil {
		t.Fatal(err)
	}
	const token = "0123456789abcdef0123456789abcdef"
	call := func(method, path string, payload any) map[string]any {
		t.Helper()
		var body []byte
		if payload != nil {
			body, err = json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://127.0.0.1:%d/api/v1/%s", h.managementPort, path), bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var result map[string]any
		if err = json.NewDecoder(res.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			t.Fatalf("%s %s failed: HTTP %d code=%v", method, path, res.StatusCode, result["error"])
		}
		return result
	}
	job := func(path string, payload any) {
		t.Helper()
		started := call("POST", path, payload)
		seq := started["sequence"]
		deadline := time.Now().Add(2 * time.Minute)
		for time.Now().Before(deadline) {
			state := call("GET", "state", nil)
			current := state["job"].(map[string]any)
			if current["sequence"] != seq {
				t.Fatal("job result replaced")
			}
			if current["running"] == false {
				if current["succeeded"] != true {
					t.Fatalf("%s failed: %v", path, current["error"])
				}
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatalf("%s timed out", path)
	}
	profile, err := h.sshOutput(ctx, "cat /var/run/fastlane/test-client.conf")
	if err != nil {
		t.Fatal("read generated stand fixture")
	}
	job("awg", map[string]any{"name": "QEMU AWG 2.0", "config": string(profile)})
	profile = nil
	job("awg/check", nil)
	pid, err := h.sshOutput(ctx, "pidof xray")
	if err != nil {
		t.Fatal(err)
	}
	job("awg/connect", nil)
	if err = h.sshCommand(ctx, "curl -fsS --max-time 15 --proxy http://127.0.0.1:10809 https://cp.cloudflare.com/generate_204 -o /dev/null"); err != nil {
		t.Fatal("HTTPS through HTTP-selected AWG failed")
	}
	after, err := h.sshOutput(ctx, "pidof xray")
	if err != nil || strings.TrimSpace(string(after)) != strings.TrimSpace(string(pid)) {
		t.Fatal("HTTP connect restarted Xray")
	}
	awg := call("GET", "awg", nil)
	if awg["active"] != true {
		t.Fatal("HTTP status does not confirm selected AWG")
	}
	t.Log("HTTP AWG import/check/connect and HTTPS egress passed, Xray PID unchanged")
	job("routing/groups", map[string]any{"name": "panel-test", "domains": []string{"example.com"}, "cidrs": []string{}})
	job("routing/groups/toggle", map[string]any{"name": "panel-test", "enabled": false})
	job("routing/groups/delete", map[string]any{"name": "panel-test"})
	job("awg/disconnect", nil)
	t.Log("Real HTTP AWG import/check/connect, HTTPS egress, unchanged Xray PID, routing-group CRUD and disconnect passed")
	if previewAddress != "" {
		host, _, err := net.SplitHostPort(previewAddress)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			t.Fatal("preview must bind an explicit loopback address")
		}
		target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", h.managementPort))
		proxy := httputil.NewSingleHostReverseProxy(target)
		originalDirector := proxy.Director
		proxy.Director = func(r *http.Request) { originalDirector(r); r.Header.Set("Authorization", "Bearer "+token) }
		proxy.ModifyResponse = func(r *http.Response) error {
			if r.Request.URL.Path != "/" || r.Request.Method != "GET" || r.StatusCode != 200 {
				return nil
			}
			data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			r.Body.Close()
			if err != nil {
				return err
			}
			banner := `<main id="main" class="fl-shell"><p class="preview-note">Изолированный OpenWrt-стенд · настоящий тестовый AWG 2.0 · домашняя сеть не изменяется</p>`
			data = bytes.Replace(data, []byte(`<main id="main" class="fl-shell">`), []byte(banner), 1)
			r.Body = io.NopCloser(bytes.NewReader(data))
			r.ContentLength = int64(len(data))
			r.Header.Del("Content-Length")
			return nil
		}
		listener, err := net.Listen("tcp", previewAddress)
		if err != nil {
			t.Fatal(err)
		}
		server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The preview injects a guest-only test credential. Host/origin checks must
			// remain here to prevent DNS rebinding from turning it into an open proxy.
			if r.Host != previewAddress || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+previewAddress) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				http.Error(w, "Forbidden", 403)
				return
			}
			proxy.ServeHTTP(w, r)
		})}
		defer server.Close()
		go server.Serve(listener)
		t.Logf("Working isolated stand ready at http://%s (12 hours)", previewAddress)
		select {
		case <-time.After(12 * time.Hour):
		case <-ctx.Done():
		}
	}
}
