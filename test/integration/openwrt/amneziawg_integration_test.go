package openwrt_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const awgReleaseBaseURL = "https://github.com/Slava-Shchipunov/awg-openwrt/releases/download/v24.10.5/"

var awgPackages = []struct{ name, sha256 string }{
	{"kmod-amneziawg_v24.10.5_x86_64_x86_64.ipk", "a52e3a492cc5d9d9cb8ee17f52e3bceee51e6b6689158c41c9112976154bb999"},
	{"amneziawg-tools_v24.10.5_x86_64_x86_64.ipk", "31617338b02846c2111f40dee3ce445ff3609eee7c39554226d28f3cfec21ec8"},
	{"luci-proto-amneziawg_v24.10.5_x86_64_x86_64.ipk", "0c843597b97cea5e7ec96b1af3559e6fe6a51552600c7c05b0e473113e92bc94"},
}

func TestOpenWrtAmneziaWGPrototype(t *testing.T) {
	if os.Getenv("FASTLANE_RUN_OPENWRT_INTEGRATION") != "1" {
		t.Skip("set FASTLANE_RUN_OPENWRT_INTEGRATION=1 to run OpenWrt/QEMU integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	harness, err := newOpenWRTHarness(t)
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	if err := harness.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := harness.sshCommand(ctx, "opkg update && opkg install ca-bundle nftables kmod-nft-tproxy rpcd-mod-file"); err != nil {
		t.Fatal(err)
	}
	if err := harness.sshCommand(ctx, "opkg list-installed | grep -q '^dnsmasq-full ' || opkg install dnsmasq-full >/dev/null 2>&1 || { opkg remove dnsmasq && opkg install dnsmasq-full; }"); err != nil {
		t.Fatalf("install dnsmasq-full for domain exclusion test: %v", err)
	}
	if err := harness.InstallFastLane(ctx); err != nil {
		t.Fatal(err)
	}
	// This scenario drives the single Fast Lane coordinator through CLI calls.
	// Stop the periodic daemon so it cannot race the deterministic hot-switch
	// assertions while QEMU is installing additional kernel packages.
	if err := harness.sshCommand(ctx, "service fastlane stop"); err != nil {
		t.Fatal(err)
	}
	if err := harness.InstallXray(ctx); err != nil {
		t.Fatal(err)
	}
	if err := installAWGStandPackages(ctx, harness); err != nil {
		t.Fatal(err)
	}
	legacyProfile := os.Getenv("FASTLANE_OPENWRT_AWG_PROFILE") == "legacy"
	if err := configureAWGNamespaceStand(ctx, harness, legacyProfile); err != nil {
		t.Fatal(err)
	}
	if err := harness.sshCommand(ctx, fastlaneRemoteBinary+" firewall set bypass example.com"); err != nil {
		t.Fatalf("configure direct exclusion: %v", err)
	}

	profileName := "QEMU AWG 2.0"
	if legacyProfile {
		profileName = "QEMU AWG Legacy"
	}
	if err := harness.sshCommand(ctx, fastlaneRemoteBinary+" awg import --file /var/run/fastlane/test-client.conf --name "+shellQuote(profileName)); err != nil {
		t.Fatal(err)
	}
	if err := harness.sshCommand(ctx, "test ! -e /var/run/fastlane/test-client.conf && ls -l /etc/fastlane/amneziawg.conf | grep -q '^-rw-------'"); err != nil {
		t.Fatalf("profile security: %v", err)
	}
	if err := harness.sshCommand(ctx, fastlaneRemoteBinary+" awg check"); err != nil {
		diagnostics, _ := harness.sshOutput(ctx, "echo CLIENT_TUNNEL_PING; ip -4 rule add from 198.18.0.2 table 51821 priority 10899 2>/dev/null || true; ping -I 198.18.0.2 -c 1 -W 3 198.18.0.1 2>&1 || true; ping -I 198.18.0.2 -c 1 -W 3 1.1.1.1 2>&1 || true; ip -4 rule del from 198.18.0.2 table 51821 priority 10899 2>/dev/null || true; echo STATUS; ubus call network.interface.fastlane_awg status 2>&1; echo CLIENT; awg show fastlane_awg 2>&1; echo SERVER; ip netns exec awgserver awg show awgsrv 2>&1; echo SERVER_ROUTES; ip netns exec awgserver ip -4 route show; echo SERVER_SOURCE_PING; ip netns exec awgserver ping -I 198.18.0.1 -c 1 -W 3 1.1.1.1 2>&1 || true; echo SOCKETS; ss -lunp 2>&1 | grep -E '51820|51821' || true; echo RULES; ip -4 rule show; ip -4 route show table 51821; echo NFT; nft list ruleset 2>&1; echo LINKS; ip -d link show 2>&1; logread -e amneziawg | tail -30")
		t.Fatalf("AWG HTTPS check: %v\nstand diagnostics:\n%s", err, diagnostics)
	}
	pidBefore, err := harness.sshOutput(ctx, "pidof xray")
	if err != nil {
		t.Fatal(err)
	}
	stateBeforeConnect, _ := harness.sshOutput(ctx, fastlaneRemoteBinary+" --json awg status")
	transferBefore, err := harness.sshOutput(ctx, "awg show fastlane_awg transfer | awk '{print $2+$3}'")
	if err != nil {
		t.Fatal(err)
	}
	connectStarted := time.Now()
	if err := harness.sshCommand(ctx, fastlaneRemoteBinary+" awg connect"); err != nil {
		t.Fatalf("AWG connect: %v", err)
	}
	t.Logf("AWG verified hot-switch time: %s", time.Since(connectStarted).Round(time.Millisecond))
	if err := harness.sshCommand(ctx, "curl -fsS --proxy http://127.0.0.1:10809 https://cp.cloudflare.com/generate_204 -o /dev/null"); err != nil {
		t.Fatalf("TCP through Xray/AWG: %v", err)
	}
	if err := harness.sshCommand(ctx, "example_ip=$(dig +short @10.0.2.3 example.com A | head -n 1); test -n \"$example_ip\" || exit 2; timeout 8 tcpdump -ni fastlane_awg -c 1 \"host $example_ip and tcp port 443\" >/tmp/awg-bypass-capture.log 2>&1 & capture=$!; sleep 1; curl_result=0; ip netns exec fastlane_client curl -4 -fsS --max-time 10 --resolve example.com:443:$example_ip https://example.com/ -o /dev/null || curl_result=$?; if wait $capture; then cat /tmp/awg-bypass-capture.log; exit 1; fi; test $curl_result = 0"); err != nil {
		t.Fatalf("direct exclusion leaked into AWG: %v", err)
	}
	if err := harness.sshCommand(ctx, "test \"$(pidof xray)\" = "+shellQuote(strings.TrimSpace(string(pidBefore)))); err != nil {
		diagnostics, _ := harness.sshOutput(ctx, "echo PID_BEFORE="+shellQuote(strings.TrimSpace(string(pidBefore)))+"; echo PID_AFTER=$(pidof xray); echo PROCESSES; ps w | grep '[x]ray'; echo SERVICE; ubus call service list '{\"name\":\"xray\"}' 2>&1; echo AWG_AFTER; "+fastlaneRemoteBinary+" --json awg status 2>&1; echo SELECTED; xray api bi --server=127.0.0.1:10085 --json fastlane-main 2>&1; echo KERNEL; dmesg | tail -80; echo SYSTEM_LOG; logread | tail -160")
		t.Fatalf("Xray PID changed: %v\nAWG status before connect: %s\n%s", err, strings.TrimSpace(string(stateBeforeConnect)), diagnostics)
	}
	if metrics, metricErr := harness.sshOutput(ctx, "pid=$(pidof xray | awk '{print $1}'); awk '/VmRSS|VmSize/ {printf \"%s=%s%s \", $1, $2, $3}' /proc/$pid/status; awk '{printf \"load1=%s\", $1}' /proc/loadavg"); metricErr == nil {
		t.Logf("QEMU runtime metrics (not NanoPi): %s", strings.TrimSpace(string(metrics)))
	}
	throughput, err := harness.sshOutput(ctx, "curl -fsS --max-time 30 --proxy http://127.0.0.1:10809 -o /dev/null -w '%{speed_download}' 'https://speed.cloudflare.com/__down?bytes=1048576'")
	if err != nil {
		t.Fatalf("AWG throughput sample: %v", err)
	}
	bytesPerSecond, err := strconv.ParseFloat(strings.TrimSpace(string(throughput)), 64)
	if err != nil || bytesPerSecond <= 0 {
		t.Fatalf("invalid AWG throughput sample %q: %v", strings.TrimSpace(string(throughput)), err)
	}
	t.Logf("QEMU AWG throughput sample (not NanoPi): %.2f Mbit/s", bytesPerSecond*8/1_000_000)
	transferAfter, err := harness.sshOutput(ctx, "awg show fastlane_awg transfer | awk '{print $2+$3}'")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(transferBefore)) == strings.TrimSpace(string(transferAfter)) {
		t.Fatalf("AWG counters did not increase: before=%s after=%s", transferBefore, transferAfter)
	}
	if err := harness.sshCommand(ctx, "ip -4 rule add from 198.18.0.2 table 51821 priority 10899; result=0; printf 'fastlane-udp' | socat -T 3 - UDP4:198.18.0.1:9999,bind=198.18.0.2 | grep -q fastlane-udp || result=$?; ip -4 rule del from 198.18.0.2 table 51821 priority 10899; exit $result"); err != nil {
		t.Fatalf("marked UDP through AWG: %v", err)
	}
	if err := assertAWGStandDNS(ctx, harness); err != nil {
		t.Fatalf("DNS through AWG route table: %v", err)
	}
	dnsmasqPID, err := harness.sshOutput(ctx, "pidof dnsmasq")
	if err != nil {
		t.Fatal(err)
	}
	failureStarted := time.Now()
	if err := harness.sshCommand(ctx, "ip netns exec awgserver ip link set awgsrv down; FASTLANE_SCHEDULER_TICK=1h service fastlane start"); err != nil {
		t.Fatalf("start AWG failure scenario: %v", err)
	}
	if err := harness.sshCommand(ctx, "found=0; for i in $(seq 1 45); do if grep -q '\"operational_mode\": \"direct\"' /etc/fastlane/state.json; then found=1; break; fi; sleep 1; done; test $found = 1"); err != nil {
		diagnostics, _ := harness.sshOutput(ctx, "cat /etc/fastlane/state.json; logread | tail -120")
		t.Fatalf("AWG did not fail open to direct: %v\n%s", err, diagnostics)
	}
	t.Logf("AWG failure to managed direct: %s", time.Since(failureStarted).Round(time.Millisecond))
	if err := harness.sshCommand(ctx, "curl -fsS --max-time 15 --proxy http://127.0.0.1:10809 https://cp.cloudflare.com/generate_204 -o /dev/null && dig +time=5 +tries=1 +short @127.0.0.1 example.com A | grep -Eq '^[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+$'"); err != nil {
		t.Fatalf("direct fallback must carry HTTPS and resolve DNS, not merely set state: %v", err)
	}
	recoveryStarted := time.Now()
	if err := harness.sshCommand(ctx, "ip netns exec awgserver ip link set awgsrv up"); err != nil {
		t.Fatalf("restore AWG server: %v", err)
	}
	if err := harness.sshCommand(ctx, "found=0; for i in $(seq 1 75); do if grep -q '\"operational_mode\": \"vpn\"' /etc/fastlane/state.json && grep -q '\"active_connection_kind\": \"amneziawg\"' /etc/fastlane/state.json; then found=1; break; fi; sleep 1; done; service fastlane stop; test $found = 1"); err != nil {
		diagnostics, _ := harness.sshOutput(ctx, "service fastlane stop >/dev/null 2>&1 || true; cat /etc/fastlane/state.json; awg show; logread | tail -160")
		t.Fatalf("AWG did not recover from direct mode: %v\n%s", err, diagnostics)
	}
	t.Logf("managed direct to confirmed AWG recovery: %s", time.Since(recoveryStarted).Round(time.Millisecond))
	if err := harness.sshCommand(ctx, "test \"$(pidof xray)\" = "+shellQuote(strings.TrimSpace(string(pidBefore)))+"; test \"$(pidof dnsmasq)\" = "+shellQuote(strings.TrimSpace(string(dnsmasqPID)))); err != nil {
		t.Fatalf("Xray or dnsmasq restarted during AWG failover/recovery: %v", err)
	}
	if err := harness.sshCommand(ctx, fastlaneRemoteBinary+" awg disconnect && "+fastlaneRemoteBinary+" awg remove"); err != nil {
		t.Fatalf("AWG cleanup: %v", err)
	}
	if err := harness.sshCommand(ctx, "test ! -e /etc/fastlane/amneziawg.conf; ! uci -q get network.fastlane_awg >/dev/null; ! ip -4 rule show | grep -q 'lookup 51821'"); err != nil {
		t.Fatalf("owned resources remained: %v", err)
	}
}

func assertAWGStandDNS(ctx context.Context, h *openWRTHarness) error {
	// Preserve the query's failure and require an actual A record. The previous
	// final route-delete command masked failed dig exits and empty DNS answers.
	return h.sshCommand(ctx, "ip -4 rule add from 198.18.0.2 table 51821 priority 10899 || exit 1; answer=$(dig +time=3 +tries=1 +short -b 198.18.0.2 @1.1.1.1 example.com A); result=$?; ip -4 rule del from 198.18.0.2 table 51821 priority 10899 || exit 1; test $result = 0 && printf '%s\\n' \"$answer\" | grep -Eq '^[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+$'")
}

func installAWGStandPackages(ctx context.Context, h *openWRTHarness) error {
	if err := h.sshCommand(ctx, "opkg install ip-full bind-dig socat curl tcpdump kmod-veth"); err != nil {
		return err
	}
	for _, pkg := range awgPackages {
		local := filepath.Join(h.cacheDir, "awg", pkg.name)
		if err := downloadFile(awgReleaseBaseURL+pkg.name, local); err != nil {
			return err
		}
		data, err := os.ReadFile(local)
		if err != nil {
			return err
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != pkg.sha256 {
			return fmt.Errorf("%s sha256=%s, want %s", pkg.name, got, pkg.sha256)
		}
		if err := h.scpFile(ctx, local, "/tmp/"+pkg.name); err != nil {
			return err
		}
	}
	install := make([]string, 0, len(awgPackages))
	for _, pkg := range awgPackages {
		install = append(install, "/tmp/"+pkg.name)
	}
	if err := h.sshCommand(ctx, "opkg install "+strings.Join(install, " ")); err != nil {
		return err
	}
	// The upstream package adds a new netifd protocol handler. A one-time
	// network restart is required after package installation so netifd registers
	// it; Fast Lane itself only reloads configuration afterwards.
	if err := h.sshCommand(ctx, "service network restart >/dev/null 2>&1 &"); err != nil {
		return err
	}
	time.Sleep(5 * time.Second)
	return h.waitForSSH(ctx)
}

func configureAWGNamespaceStand(ctx context.Context, h *openWRTHarness, legacy bool) error {
	script := `set -eu
umask 077
service firewall stop >/dev/null 2>&1 || true
nft delete table inet fw4 2>/dev/null || true
sysctl -w net.ipv4.ip_forward=1 >/dev/null
sysctl -w net.ipv4.conf.all.rp_filter=0 >/dev/null
sysctl -w net.ipv4.conf.default.rp_filter=0 >/dev/null
modprobe amneziawg
ip netns del awgserver 2>/dev/null || true
ip netns del fastlane_client 2>/dev/null || true
ip link del awgwan 2>/dev/null || true
ip link del flclient_host 2>/dev/null || true
ip netns add awgserver
ip link add awgwan type veth peer name awgsrvwan
ip addr add 192.0.2.1/24 dev awgwan
ip link set awgwan up
ip link set awgsrvwan netns awgserver
ip netns exec awgserver ip link set lo up
ip netns exec awgserver sysctl -w net.ipv4.ip_forward=1 >/dev/null
ip netns exec awgserver sysctl -w net.ipv4.conf.all.rp_filter=0 >/dev/null
ip netns exec awgserver sysctl -w net.ipv4.conf.default.rp_filter=0 >/dev/null
ip netns exec awgserver ip addr add 192.0.2.2/24 dev awgsrvwan
ip netns exec awgserver ip link set awgsrvwan up
ip netns exec awgserver ip route add default via 192.0.2.1
ip netns add fastlane_client
ip link add flclient_host type veth peer name flclient_lan
ip addr add 10.77.0.1/24 dev flclient_host
ip link set flclient_host up
ip link set flclient_lan netns fastlane_client
ip netns exec fastlane_client ip link set lo up
ip netns exec fastlane_client ip addr add 10.77.0.2/24 dev flclient_lan
ip netns exec fastlane_client ip link set flclient_lan up
ip netns exec fastlane_client ip route add default via 10.77.0.1
server_private="$(awg genkey)"
server_public="$(printf '%s' "$server_private" | awg pubkey)"
client_private="$(awg genkey)"
client_public="$(printf '%s' "$client_private" | awg pubkey)"
if ! ip link add dev awgsrv type amneziawg; then
	echo '--- loaded modules ---'
	grep amneziawg /proc/modules || true
	echo '--- module metadata ---'
	modinfo amneziawg 2>&1 || true
	echo '--- kernel log ---'
	dmesg | tail -40
	exit 92
fi
printf '%s\n' '[Interface]' "PrivateKey=$server_private" 'ListenPort=51820' 'Jc=4' 'Jmin=40' 'Jmax=70' 'S1=12' 'S2=8' 'S3=6' 'S4=4' 'H1=1' 'H2=2' 'H3=3' 'H4=4' '' '[Peer]' "PublicKey=$client_public" 'AllowedIPs=198.18.0.2/32' > /tmp/awg-server.conf
awg setconf awgsrv /tmp/awg-server.conf
ip link set awgsrv netns awgserver
ip netns exec awgserver sysctl -w net.ipv4.conf.awgsrv.forwarding=1 >/dev/null
ip netns exec awgserver sysctl -w net.ipv4.conf.awgsrv.rp_filter=0 >/dev/null
ip netns exec awgserver sysctl -w net.ipv4.conf.awgsrvwan.forwarding=1 >/dev/null
ip netns exec awgserver sysctl -w net.ipv4.conf.awgsrvwan.rp_filter=0 >/dev/null
ip netns exec awgserver ip addr add 198.18.0.1/24 dev awgsrv
ip netns exec awgserver ip link set awgsrv up
ip netns exec awgserver ip route replace 198.18.0.2/32 dev awgsrv
ip netns exec awgserver nft add table ip awgserver_nat
ip netns exec awgserver nft 'add chain ip awgserver_nat postrouting { type nat hook postrouting priority srcnat; policy accept; }'
ip netns exec awgserver nft add rule ip awgserver_nat postrouting ip saddr 198.18.0.0/24 oifname awgsrvwan masquerade
ip route add 198.18.0.0/24 via 192.0.2.2 dev awgwan
sysctl -w net.ipv4.conf.awgwan.forwarding=1 >/dev/null
sysctl -w net.ipv4.conf.awgwan.rp_filter=0 >/dev/null
wan_device="$(ip -4 route show default | awk 'NR==1 {print $5}')"
nft delete table ip awgtest 2>/dev/null || true
nft delete table inet awgforward 2>/dev/null || true
nft add table ip awgtest
nft 'add chain ip awgtest postrouting { type nat hook postrouting priority srcnat; policy accept; }'
nft add rule ip awgtest postrouting ip saddr 198.18.0.0/24 oifname "$wan_device" masquerade
nft add rule ip awgtest postrouting ip saddr 192.0.2.0/24 oifname "$wan_device" masquerade
nft add rule ip awgtest postrouting ip saddr 10.77.0.0/24 oifname "$wan_device" masquerade
nft add table inet awgforward
nft 'add chain inet awgforward forward { type filter hook forward priority filter; policy accept; }'
ip netns exec awgserver ping -c 1 -W 3 1.1.1.1 >/dev/null
ip netns exec awgserver ping -I 198.18.0.1 -c 1 -W 3 1.1.1.1 >/dev/null
ip netns exec awgserver sh -c "socat UDP4-LISTEN:9999,reuseaddr,fork EXEC:/bin/cat >/tmp/awg-udp.log 2>&1 &"
mkdir -p /var/run/fastlane
chmod 0700 /var/run/fastlane
printf '%s\n' '[Interface]' "PrivateKey=$client_private" 'Address=198.18.0.2/32' 'MTU=1380' 'Jc=4' 'Jmin=40' 'Jmax=70' 'S1=12' 'S2=8' 'S3=6' 'S4=4' 'H1=1' 'H2=2' 'H3=3' 'H4=4' '' '[Peer]' "PublicKey=$server_public" 'AllowedIPs=0.0.0.0/0' 'Endpoint=127.0.0.1:51820' 'PersistentKeepalive=5' > /var/run/fastlane/test-client.conf
chmod 0600 /var/run/fastlane/test-client.conf`
	if legacy {
		script = strings.ReplaceAll(script, " 'S3=6' 'S4=4'", "")
		script = strings.Replace(script, "'Address=198.18.0.2/32'", "'Address=198.18.0.2'", 1)
	}
	return h.sshCommand(ctx, script)
}
