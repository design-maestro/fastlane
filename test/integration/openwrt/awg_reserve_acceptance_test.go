package openwrt_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
)

// Run only against a new disposable QEMU guest. Both VPN servers are real:
// configureAWGNamespaceStand generates the AWG keys, and the local VLESS
// listener below gets a freshly generated client ID. Nothing uses a production
// subscription or the unroutable integrationRawVLESSFixture.
//
//	FASTLANE_RUN_OPENWRT_INTEGRATION=1 go test ./test/integration/openwrt \
//	  -run '^TestOpenWrtAWGReserveAcceptance$' -count=1 -timeout=30m -v
//
// Optional FASTLANE_AWG_RESERVE_REBOOT=1 also checks cold fail-open after both
// test namespaces disappear. PID continuity is intentionally not a reboot claim.
func TestOpenWrtAWGReserveAcceptance(t *testing.T) {
	if os.Getenv("FASTLANE_RUN_OPENWRT_INTEGRATION") != "1" {
		t.Skip("requires isolated OpenWrt/QEMU stand")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 28*time.Minute)
	defer cancel()
	h, err := newOpenWRTHarness(t)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	defer func() {
		if t.Failed() {
			logAWGReserveDiagnostics(t, h)
		}
	}()
	if err := h.Start(ctx); err != nil {
		t.Fatal(err)
	}
	command := func(label, script string) {
		t.Helper()
		if err := h.sshCommand(ctx, script); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	command("install base dependencies", "opkg update && opkg install ca-bundle nftables kmod-nft-tproxy rpcd-mod-file")
	command("install DNS nftset support", "opkg list-installed | grep -q '^dnsmasq-full ' || opkg install dnsmasq-full >/dev/null 2>&1 || { opkg remove dnsmasq && opkg install dnsmasq-full; }")
	if err := h.InstallFastLane(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.PauseFastLaneDaemon(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.InstallXray(ctx); err != nil {
		t.Fatal(err)
	}
	command("enable Xray at boot", "/etc/init.d/xray enable")
	if err := installAWGStandPackages(ctx, h); err != nil {
		t.Fatal(err)
	}
	if err := configureAWGNamespaceStand(ctx, h); err != nil {
		t.Fatal(err)
	}
	subID, nodeID := installAWGReserveVLESS(t, ctx, h)
	// These two namespaces are upstream test infrastructure, not LAN clients.
	// Use the supported persisted policy to exempt their egress and endpoints;
	// never exempt fastlane_client (10.77.0.2) or the public HTTPS probe targets.
	command("configure routing/checks", fastlaneRemoteBinary+" firewall set bypass example.com 198.19.0.2 192.0.2.2 --exclude-host 198.19.0.2 --exclude-host 192.0.2.2 && "+fastlaneRemoteBinary+" settings set refresh-interval 24h && "+fastlaneRemoteBinary+" settings set health-check-interval 0s")
	command("VLESS infrastructure HTTPS after firewall apply", "ip netns exec vlessreserve curl -4 -fsS --max-time 20 --noproxy '*' https://www.gstatic.com/generate_204 -o /dev/null")
	command("independent real VLESS HTTPS before application connect", `set -eu
xray run -test -config /tmp/awg-reserve-acceptance/client.json >/dev/null
xray run -config /tmp/awg-reserve-acceptance/client.json </dev/null >/tmp/awg-reserve-acceptance/client.log 2>&1 &
client_pid=$!
trap 'kill "$client_pid" 2>/dev/null || true; wait "$client_pid" 2>/dev/null || true' EXIT
found=0
for attempt in $(seq 1 20); do
  if awk '$2 ~ /:525A$/ && $4 == "0A" { found=1 } END { exit !found }' /proc/net/tcp; then found=1; break; fi
  sleep 1
done
test "$found" = 1
curl -4 -fsS --max-time 20 --noproxy '' --proxy http://127.0.0.1:21082 https://www.gstatic.com/generate_204 -o /dev/null
curl -4 -fsS --max-time 20 --noproxy '' --proxy http://127.0.0.1:21082 https://cp.cloudflare.com/generate_204 -o /dev/null`)
	t.Log("independent VLESS transport: certificate-checked gstatic and Cloudflare HTTPS passed")
	// This first successful connection proves the reserve is usable before the
	// failure trial. The application must subsequently verify it as a reserve;
	// the test never writes health, runtime state or prepared-outbound metadata.
	if err := h.Connect(ctx, subID, nodeID); err != nil {
		t.Fatalf("connect real VLESS server: %v", err)
	}
	assertAWGReserveHTTPS(t, ctx, h, false)
	command("import/check/connect AWG", fastlaneRemoteBinary+" awg import --file /var/run/fastlane/test-client.conf --name 'AWG reserve acceptance' && "+fastlaneRemoteBinary+" awg check && "+fastlaneRemoteBinary+" awg connect")
	assertAWGReserveHTTPS(t, ctx, h, true)
	if err := assertAWGStandDNS(ctx, h); err != nil {
		t.Fatalf("positive A answer via active AWG: %v", err)
	}
	command("start actual connection watcher", "FASTLANE_SCHEDULER_TICK=1h service fastlane start")
	ready := waitAWGReserveState(t, ctx, h, 180*time.Second, "AWG active with verified VLESS reserve", func(s awgReserveSnapshot) bool {
		if !awgReserveManual(s) || !s.State.Connected || s.State.ActiveConnectionKind != "amneziawg" || s.State.OperationalMode != domain.OperationalModeVPN {
			return false
		}
		for _, outbound := range s.State.RuntimeOutbounds {
			if outbound.SubscriptionID == subID && outbound.NodeID == nodeID && outbound.Role == "reserve" && !outbound.VerifiedAt.IsZero() && s.State.Health[nodeID].Healthy {
				return true
			}
		}
		return false
	})
	var reserveTag string
	for _, outbound := range ready.State.RuntimeOutbounds {
		if outbound.NodeID == nodeID && outbound.Role == "reserve" {
			reserveTag = outbound.Tag
		}
	}
	if reserveTag == "" {
		t.Fatal("verified reserve has no runtime tag")
	}
	before := readAWGReservePIDs(t, ctx, h)
	assertAWGReserveSelected(t, ctx, h, ready.State.SelectedOutboundTag)
	started := time.Now()
	command("stop only AWG server", "ip netns exec awgserver ip link set awgsrv down")
	fallback := waitAWGReserveState(t, ctx, h, 90*time.Second, "automatic AWG to VLESS reserve", func(s awgReserveSnapshot) bool {
		return awgReserveManual(s) && s.State.Connected && s.State.OperationalMode == domain.OperationalModeVPN && s.State.ActiveConnectionKind == "xray" && s.State.ActiveSubscriptionID == subID && s.State.ActiveNodeID == nodeID && s.State.SelectedOutboundTag == reserveTag && s.State.CurrentOperation == nil
	})
	if !strings.Contains(fallback.State.LastSwitchReason, "verified reserve") {
		t.Fatalf("did not use verified-reserve path: %q", fallback.State.LastSwitchReason)
	}
	t.Logf("AWG -> verified local VLESS reserve: %s", time.Since(started).Round(time.Millisecond))
	assertAWGReserveSelected(t, ctx, h, reserveTag)
	requestsBefore := readAWGReserveHTTPSCount(t, ctx, h)
	assertAWGReserveHTTPS(t, ctx, h, true)
	assertAWGReserveLANDNS(t, ctx, h)
	// Require the actual VLESS listener to observe a new HTTPS connection. The
	// explicit proxy request and selected balancer tag above exclude a curl
	// direct-connect fallback; the server log is additional transport evidence.
	if requestsAfter := readAWGReserveHTTPSCount(t, ctx, h); requestsAfter <= requestsBefore {
		t.Fatalf("reserve observed no new HTTPS connection: before=%d after=%d", requestsBefore, requestsAfter)
	}
	assertAWGReservePIDs(t, ctx, h, before)

	// With AWG still down, remove only the reserve server's network link. The
	// router WAN and synthetic LAN client remain operational for fail-open proof.
	command("stop VLESS reserve network", "ip netns exec vlessreserve ip link set vrsvwan down")
	waitAWGReserveState(t, ctx, h, 120*time.Second, "both VPN servers failed -> managed direct", awgReserveDirect)
	assertAWGReserveSelected(t, ctx, h, "fastlane-direct")
	assertAWGReserveHTTPS(t, ctx, h, true)
	assertAWGReserveLANDNS(t, ctx, h)
	assertAWGReservePIDs(t, ctx, h, before)

	// The HTTP listener opens after RestoreRuntime returns. A new daemon PID
	// plus this fresh listener prevents stale state.json from passing restart.
	command("restart service while both servers remain down", "before=$(pidof fastlane); test -n \"$before\" || exit 1; FASTLANE_SCHEDULER_TICK=1h FASTLANE_MANAGEMENT_LISTEN=127.0.0.1:9080 service fastlane restart || exit 1; found=0; for attempt in $(seq 1 90); do after=$(pidof fastlane || true); if test -n \"$after\" && test \"$after\" != \"$before\" && curl -fsS --max-time 2 http://127.0.0.1:9080/api/v1/state >/dev/null; then found=1; break; fi; sleep 1; done; test $found = 1")
	waitAWGReserveState(t, ctx, h, 120*time.Second, "service restart preserves fail-open", awgReserveDirect)
	assertAWGReserveSelected(t, ctx, h, "fastlane-direct")
	assertAWGReserveHTTPS(t, ctx, h, true)
	assertAWGReserveLANDNS(t, ctx, h)
	assertAWGReservePIDs(t, ctx, h, before)

	if os.Getenv("FASTLANE_AWG_RESERVE_REBOOT") == "1" {
		// Neither test server is a boot service. Their disappearance is the
		// intended outage; do not recreate them or silently reconnect a VPN.
		if err := h.RebootAndWait(ctx); err != nil {
			t.Fatal(err)
		}
		waitAWGReserveState(t, ctx, h, 120*time.Second, "cold boot with unavailable VPN servers", awgReserveDirect)
		waitAWGReserveSelected(t, ctx, h, 120*time.Second, "fastlane-direct")
		assertAWGReserveHTTPS(t, ctx, h, false)
		command("positive DNS answer after reboot", "answer=$(dig +time=5 +tries=1 +short @127.0.0.1 example.com A) || exit 1; printf '%s\\n' \"$answer\" | grep -Eq '^[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+$'")
	}
}

// Only fixed, non-secret network facts and aggregate log categories leave the
// disposable guest. Do not dump profiles, Xray configs, raw logs or status JSON.
func logAWGReserveDiagnostics(t *testing.T, h *openWRTHarness) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	script := `printf 'processes\n'
pidof fastlane xray dnsmasq || true
printf 'router routes\n'
ip -4 route show
ip -4 rule show
printf 'reserve routes/listener\n'
ip netns exec vlessreserve ip -4 route show
ip netns exec vlessreserve awk '$2 ~ /:5259$/ { counts[$4]++ } END { for (state in counts) print "server socket state=" state, "count=" counts[state] }' /proc/net/tcp
printf 'reserve error categories (counts only)\n'
awk '/failed/ { failed++ } /timeout/ { timeout++ } /refused/ { refused++ } /network is unreachable/ { unreachable++ } /lookup/ { lookup++ } /EOF/ { eof++ } END { print "failed=" failed+0, "timeout=" timeout+0, "refused=" refused+0, "unreachable=" unreachable+0, "lookup=" lookup+0, "EOF=" eof+0 }' /tmp/awg-reserve-acceptance/server.log
printf 'reserve accepted probe counts\n'
awk '/cp.cloudflare.com:443/ { cloudflare++ } /www.gstatic.com:443/ { gstatic++ } END { print "cloudflare=" cloudflare+0, "gstatic=" gstatic+0 }' /tmp/awg-reserve-acceptance/access.log
printf 'reserve direct HTTPS\n'
ip netns exec vlessreserve curl -4 -sS --max-time 8 --noproxy '*' https://www.gstatic.com/generate_204 -o /dev/null -w 'http=%{http_code}\n'
printf 'managed proxy HTTPS\n'
curl -4 -sS --max-time 8 --noproxy '' --proxy http://127.0.0.1:10809 https://www.gstatic.com/generate_204 -o /dev/null -w 'http=%{http_code}\n'
true`
	output, err := h.sshOutput(ctx, script)
	t.Logf("isolated reserve diagnostics: %s (read error: %v)", output, err)
}

type awgReserveSnapshot struct {
	State    domain.RuntimeState `json:"state"`
	Settings domain.Settings     `json:"settings"`
}

func awgReserveManual(s awgReserveSnapshot) bool {
	return s.State.Mode == domain.SelectionModeManual && s.Settings.Mode == domain.SelectionModeManual && !s.Settings.AutoMode && s.State.AutoScope == ""
}

func awgReserveDirect(s awgReserveSnapshot) bool {
	return awgReserveManual(s) && !s.State.Connected && s.State.OperationalMode == domain.OperationalModeDirect && s.State.SelectedOutboundTag == "fastlane-direct" && s.State.CurrentOperation == nil
}

func waitAWGReserveState(t *testing.T, parent context.Context, h *openWRTHarness, limit time.Duration, label string, accept func(awgReserveSnapshot) bool) awgReserveSnapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()
	var last awgReserveSnapshot
	var lastErr error
	for ctx.Err() == nil {
		output, err := h.sshOutput(ctx, fastlaneRemoteBinary+" --json status")
		if err == nil {
			err = json.Unmarshal(output, &last)
			if err == nil && accept(last) {
				return last
			}
		}
		lastErr = err
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	// No configurations, credentials, raw server logs or profile dumps on failure.
	t.Fatalf("%s: timeout; mode=%s connection=%s connected=%t operation=%s tag=%s last read error=%v", label, last.State.Mode, last.State.ActiveConnectionKind, last.State.Connected, last.State.OperationalMode, last.State.SelectedOutboundTag, lastErr)
	return last
}

func assertAWGReserveHTTPS(t *testing.T, ctx context.Context, h *openWRTHarness, lan bool) {
	t.Helper()
	// Certificate verification stays enabled. --noproxy prevents environment
	// exemptions from bypassing the explicit managed Xray HTTP inbound.
	if err := h.sshCommand(ctx, "curl -4 -fsS --max-time 20 --noproxy '' --proxy http://127.0.0.1:10809 https://cp.cloudflare.com/generate_204 -o /dev/null"); err != nil {
		t.Fatalf("actual HTTPS through selected Xray outbound: %v", err)
	}
	if lan {
		if err := h.sshCommand(ctx, "ip netns exec fastlane_client curl -4 -fsS --max-time 20 --noproxy '*' https://cp.cloudflare.com/generate_204 -o /dev/null"); err != nil {
			t.Fatalf("actual transparent LAN HTTPS: %v", err)
		}
	}
}

func assertAWGReserveLANDNS(t *testing.T, ctx context.Context, h *openWRTHarness) {
	t.Helper()
	if err := h.sshCommand(ctx, "answer=$(ip netns exec fastlane_client dig +time=5 +tries=1 +short @10.77.0.1 example.com A) || exit 1; printf '%s\\n' \"$answer\" | grep -Eq '^[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+$'"); err != nil {
		t.Fatalf("positive LAN DNS answer: %v", err)
	}
}

func assertAWGReserveSelected(t *testing.T, ctx context.Context, h *openWRTHarness, tag string) {
	t.Helper()
	if tag == "" {
		t.Fatal("empty expected balancer target")
	}
	output, err := h.sshOutput(ctx, "xray api bi --server=127.0.0.1:10085 --json fastlane-main")
	if err != nil {
		t.Fatalf("read actual Xray balancer: %v", err)
	}
	// Same contract as RuntimeBackend.SelectedOutbound: finding a tag elsewhere
	// in the balancer JSON does not prove it is the selected override.
	var result struct {
		Balancer struct {
			Override struct {
				Target string `json:"target"`
			} `json:"override"`
		} `json:"balancer"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode actual balancer: %v", err)
	}
	if result.Balancer.Override.Target != tag {
		t.Fatalf("Xray selected %q, want %q", result.Balancer.Override.Target, tag)
	}
}

func waitAWGReserveSelected(t *testing.T, parent context.Context, h *openWRTHarness, limit time.Duration, tag string) {
	t.Helper()
	if tag == "" {
		t.Fatal("empty expected balancer target")
	}
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()
	var lastErr error
	for ctx.Err() == nil {
		output, err := h.sshOutput(ctx, "xray api bi --server=127.0.0.1:10085 --json fastlane-main")
		if err == nil {
			var result struct {
				Balancer struct {
					Override struct {
						Target string `json:"target"`
					} `json:"override"`
				} `json:"balancer"`
			}
			err = json.Unmarshal(output, &result)
			if err == nil && result.Balancer.Override.Target == tag {
				return
			}
			if err == nil {
				err = fmt.Errorf("selected %q, want %q", result.Balancer.Override.Target, tag)
			}
		}
		lastErr = err
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	t.Fatalf("actual Xray balancer did not become ready after reboot: %v", lastErr)
}

func readAWGReserveHTTPSCount(t *testing.T, ctx context.Context, h *openWRTHarness) int {
	t.Helper()
	output, err := h.sshOutput(ctx, `awk 'index($0,"cp.cloudflare.com:443"){n++} END{print n+0}' /tmp/awg-reserve-acceptance/access.log`)
	if err != nil {
		t.Fatal(err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatalf("parse reserve HTTPS count: %v", err)
	}
	return count
}

func readAWGReservePIDs(t *testing.T, ctx context.Context, h *openWRTHarness) string {
	t.Helper()
	output, err := h.sshOutput(ctx, "set -eu; pid=$(cat /var/run/xray.pid); kill -0 \"$pid\"; printf 'xray=%s\\n' \"$pid\"; pids=$(pidof dnsmasq); test -n \"$pids\"; printf '%s\\n' \"$pids\" | tr ' ' '\\n' | sort -n")
	if err != nil {
		t.Fatalf("read managed Xray/dnsmasq PIDs: %v", err)
	}
	return strings.TrimSpace(string(output))
}

func assertAWGReservePIDs(t *testing.T, ctx context.Context, h *openWRTHarness, before string) {
	t.Helper()
	if after := readAWGReservePIDs(t, ctx, h); after != before {
		t.Fatalf("managed Xray or dnsmasq restarted: before=%q after=%q", before, after)
	}
}

func installAWGReserveVLESS(t *testing.T, ctx context.Context, h *openWRTHarness) (string, string) {
	t.Helper()
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatal(err)
	}
	key[6] = key[6]&0x0f | 0x40
	key[8] = key[8]&0x3f | 0x80
	id := fmt.Sprintf("%x-%x-%x-%x-%x", key[:4], key[4:6], key[6:8], key[8:10], key[10:])
	config := map[string]any{
		"log": map[string]any{"access": "/tmp/awg-reserve-acceptance/access.log", "loglevel": "info"},
		// The synthetic upstream has IPv4 forwarding/NAT only. Make the real
		// server's resolver match that topology rather than attempting IPv6.
		"dns":       map[string]any{"servers": []string{"10.0.2.3"}, "queryStrategy": "UseIPv4"},
		"inbounds":  []any{map[string]any{"listen": "198.19.0.2", "port": 21081, "protocol": "vless", "tag": "reserve-server", "settings": map[string]any{"clients": []any{map[string]any{"id": id}}, "decryption": "none"}}},
		"outbounds": []any{map[string]any{"protocol": "freedom", "tag": "reserve-egress", "settings": map[string]any{"domainStrategy": "UseIPv4"}}},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(t.TempDir(), "reserve-server.json")
	if err := os.WriteFile(local, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.sshCommand(ctx, "mkdir -p /tmp/awg-reserve-acceptance && chmod 0700 /tmp/awg-reserve-acceptance"); err != nil {
		t.Fatal(err)
	}
	if err := h.scpFile(ctx, local, "/tmp/awg-reserve-acceptance/server.json"); err != nil {
		t.Fatal(err)
	}
	// A short-lived independent client distinguishes fixture/transport failure
	// from Fast Lane's generated runtime. It is stopped before app connect and
	// never supplies health evidence or a fallback path to the application.
	client := map[string]any{
		"log":      map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": 21082, "protocol": "http"}},
		"outbounds": []any{map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{
			"address": "198.19.0.2", "port": 21081, "users": []any{map[string]any{"id": id, "encryption": "none"}},
		}}}}},
	}
	clientData, err := json.Marshal(client)
	if err != nil {
		t.Fatal(err)
	}
	clientLocal := filepath.Join(t.TempDir(), "reserve-client.json")
	if err := os.WriteFile(clientLocal, clientData, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.scpFile(ctx, clientLocal, "/tmp/awg-reserve-acceptance/client.json"); err != nil {
		t.Fatal(err)
	}
	// A second namespace has independent egress, so shutting down the AWG
	// server cannot also take down the reserve. Only this fresh guest is touched.
	script := `set -eu
umask 077
ip netns add vlessreserve
ip link add vrsvhost type veth peer name vrsvwan
ip addr add 198.19.0.1/24 dev vrsvhost
ip link set vrsvhost up
ip link set vrsvwan netns vlessreserve
ip netns exec vlessreserve ip link set lo up
ip netns exec vlessreserve ip addr add 198.19.0.2/24 dev vrsvwan
ip netns exec vlessreserve ip link set vrsvwan up
ip netns exec vlessreserve ip route add default via 198.19.0.1
sysctl -w net.ipv4.conf.vrsvhost.forwarding=1 >/dev/null
sysctl -w net.ipv4.conf.vrsvhost.rp_filter=0 >/dev/null
wan_device="$(ip -4 route show default | awk 'NR==1 {print $5}')"
test -n "$wan_device"
nft add rule ip awgtest postrouting ip saddr 198.19.0.0/24 oifname "$wan_device" masquerade
mkdir -p /etc/netns/vlessreserve /etc/netns/fastlane_client
printf 'nameserver 10.0.2.3\n' > /etc/netns/vlessreserve/resolv.conf
printf 'nameserver 10.77.0.1\n' > /etc/netns/fastlane_client/resolv.conf
ip netns exec vlessreserve xray run -test -config /tmp/awg-reserve-acceptance/server.json >/dev/null
ip netns exec vlessreserve xray run -config /tmp/awg-reserve-acceptance/server.json </dev/null >/tmp/awg-reserve-acceptance/server.log 2>&1 &
printf '%s\n' "$!" > /tmp/awg-reserve-acceptance/server.pid
found=0
for attempt in $(seq 1 20); do
  if ip netns exec vlessreserve awk '$2 ~ /:5259$/ && $4 == "0A" { found=1 } END { exit !found }' /proc/net/tcp; then found=1; break; fi
  sleep 1
done
test "$found" = 1
ip netns exec vlessreserve curl -4 -fsS --max-time 20 --noproxy '*' https://cp.cloudflare.com/generate_204 -o /dev/null`
	if err := h.sshCommand(ctx, script); err != nil {
		t.Fatalf("create isolated VLESS server: %v", err)
	}
	// The link is plaintext VLESS only over this synthetic VM-local veth. The
	// acceptance payload and all health probes are real certificate-checked HTTPS.
	subID, nodeID, err := h.AddSubscription(ctx, "vless://"+id+"@198.19.0.2:21081?encryption=none&security=none&type=tcp#AWG%20acceptance%20reserve")
	if err != nil {
		t.Fatalf("add generated VLESS reserve: %v", err)
	}
	return subID, nodeID
}
