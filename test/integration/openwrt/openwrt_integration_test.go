package openwrt_test

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	openWrtVersion             = "24.10.5"
	openWrtImageURL            = "https://downloads.openwrt.org/releases/24.10.5/targets/x86/64/openwrt-24.10.5-x86-64-generic-ext4-combined.img.gz"
	xrayVersion                = "v26.2.6"
	xrayLinuxAMD64URL          = "https://github.com/XTLS/Xray-core/releases/download/v26.2.6/Xray-linux-64.zip"
	integrationRawVLESSFixture = "vless://11111111-1111-1111-1111-111111111111@203.0.113.10:443?encryption=none&security=tls&sni=edge.example.com&type=ws&path=%2Fproxy&host=cdn.example.com#OpenWrt%20Integration"
	fastlaneRemoteBinary       = "/usr/bin/fastlane"
	xrayRemoteBinary           = "/usr/bin/xray"
	xrayRemoteService          = "/etc/init.d/xray"
	fastlaneRemoteService      = "/etc/init.d/fastlane"
	xrayRemoteConfigDir        = "/etc/xray"
	luciTestPassword           = "fastlane-smoke"
	consoleLoginPrompt         = "login:"
	consoleRootPrompt          = "root@"
	openWrtBootTimeout         = 10 * time.Minute
	sshRetryDelay              = 2 * time.Second
	sshRetryAttempts           = 5
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestOpenWrtEndToEnd(t *testing.T) {
	if os.Getenv("FASTLANE_RUN_OPENWRT_INTEGRATION") != "1" {
		t.Skip("set FASTLANE_RUN_OPENWRT_INTEGRATION=1 to run OpenWrt/QEMU integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	harness, err := newOpenWRTHarness(t)
	if err != nil {
		t.Fatalf("create integration harness: %v", err)
	}
	defer harness.Close()

	if err := harness.Start(ctx); err != nil {
		t.Fatalf("start OpenWrt VM: %v", err)
	}
	t.Log("OpenWrt VM booted")
	if err := harness.InstallLuCI(ctx); err != nil {
		t.Fatalf("install LuCI: %v", err)
	}
	t.Log("LuCI installed")
	if err := harness.InstallFastLane(ctx); err != nil {
		t.Fatalf("install fastlane: %v", err)
	}
	t.Log("Fast Lane installed")
	if err := harness.InstallXray(ctx); err != nil {
		t.Fatalf("install xray: %v", err)
	}
	t.Log("Xray installed")
	if err := harness.AssertLuCIVPNPage(ctx, "Fast Lane", "VPN", "Все серверы", "Добавить подписку", "Добавьте первую подписку"); err != nil {
		t.Fatalf("browser smoke VPN empty state: %v", err)
	}
	if err := harness.AssertLuCIVPNAddDialog(ctx); err != nil {
		t.Fatalf("browser smoke VPN add dialog action: %v", err)
	}
	t.Log("LuCI VPN empty state and add dialog verified")
	if err := harness.AssertLuCIDiagnosticsPage(ctx, "Fast Lane"); err != nil {
		t.Fatalf("browser smoke diagnostics page: %v", err)
	}
	if err := harness.AssertLuCIRoutingPage(ctx, "Fast Lane", "Маршруты", "Россия напрямую", "Импорт и расширенные правила", "Проверить ссылку"); err != nil {
		t.Fatalf("browser smoke routing page: %v", err)
	}
	if err := harness.AssertLuCIRoutingHAPPPreview(ctx); err != nil {
		t.Fatalf("browser smoke routing HAPP preview: %v", err)
	}
	if err := harness.AssertLuCISettingsPage(ctx, "Fast Lane", "Настройки Fast Lane", "Подписки и проверки", "Автовыбор", "Адрес URL-теста", "Строгая проверка интернета", "Сохранить"); err != nil {
		t.Fatalf("browser smoke settings page: %v", err)
	}

	subID, nodeID, err := harness.AddSubscription(ctx, integrationRawVLESSFixture)
	if err != nil {
		t.Fatalf("add subscription: %v", err)
	}
	if err := harness.AssertLuCIVPNPage(ctx, "Все серверы", "OpenWrt Integration", "VLESS", "Готов"); err != nil {
		t.Fatalf("browser smoke VPN populated state: %v", err)
	}
	if err := harness.SetStrictEgressCheckViaLuCI(ctx, false); err != nil {
		t.Fatalf("disable strict egress check through LuCI for unroutable integration fixture: %v", err)
	}
	if err := harness.Connect(ctx, subID, nodeID); err != nil {
		t.Fatalf("connect fastlane: %v", err)
	}
	if err := harness.AssertXrayRunning(ctx); err != nil {
		t.Fatalf("assert xray running after connect: %v", err)
	}
	if err := harness.AssertLuCIVPNPage(ctx, "VPN включён", "OpenWrt Integration", "АКТИВЕН", "Вручную"); err != nil {
		t.Fatalf("browser smoke VPN connected state: %v", err)
	}
	if err := harness.AssertLuCIDiagnosticsPage(ctx, "OpenWrt Integration"); err != nil {
		t.Fatalf("browser smoke diagnostics connected state: %v", err)
	}
	if err := harness.ApplyDefaultDNS(ctx); err != nil {
		t.Fatalf("apply default dns: %v", err)
	}
	if err := harness.AssertDNSRuntimeActive(ctx); err != nil {
		t.Fatalf("assert dns runtime active: %v", err)
	}
	if err := harness.EnableFirewallTargets(ctx, "1.1.1.1"); err != nil {
		t.Fatalf("enable firewall targets: %v", err)
	}
	if err := harness.AssertFirewallTableContains(ctx, "ip daddr @proxy_target_v4"); err != nil {
		t.Fatalf("assert firewall table: %v", err)
	}
	if err := harness.EnableFirewallAntiTargets(ctx, "youtube.com"); err != nil {
		t.Fatalf("enable firewall anti-targets: %v", err)
	}
	for _, needle := range []string{
		"chain prerouting_mangle",
		"tproxy ip to :12345",
	} {
		if err := harness.AssertFirewallTableContains(ctx, needle); err != nil {
			t.Fatalf("assert anti-target firewall table: %v", err)
		}
	}

	if err := harness.RebootAndWait(ctx); err != nil {
		t.Fatalf("reboot and wait: %v", err)
	}
	if err := harness.AssertFastLaneRestore(ctx, "tproxy ip to :12345"); err != nil {
		t.Fatalf("assert restore after reboot: %v", err)
	}

	if err := harness.Disconnect(ctx); err != nil {
		t.Fatalf("disconnect fastlane: %v", err)
	}
	if err := harness.AssertLuCIVPNPage(ctx, "VPN выключен"); err != nil {
		t.Fatalf("browser smoke VPN disconnected state: %v", err)
	}
	if err := harness.AssertDNSRuntimeDisabled(ctx); err != nil {
		t.Fatalf("assert dns runtime disabled: %v", err)
	}
	if err := harness.AssertFirewallTableRemoved(ctx); err != nil {
		t.Fatalf("assert firewall table removed: %v", err)
	}
}

type openWRTHarness struct {
	t             *testing.T
	repoRoot      string
	workDir       string
	cacheDir      string
	sshPort       int
	httpPort      int
	qemuImagePath string
	qemuBin       string
	fastlaneBin   string
	xrayBin       string
	sshKeyPath    string
	qemuCmd       *exec.Cmd
	console       *consoleLog
	consoleStdin  io.WriteCloser
}

func newOpenWRTHarness(t *testing.T) (*openWRTHarness, error) {
	t.Helper()

	repoRoot, err := repoRoot()
	if err != nil {
		return nil, err
	}
	qemuBin, err := exec.LookPath("qemu-system-x86_64")
	if err != nil {
		return nil, fmt.Errorf("find qemu-system-x86_64: %w", err)
	}

	workDir := t.TempDir()
	cacheDir := filepath.Join(repoRoot, ".cache", "openwrt-integration")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}

	sshPort, err := freeTCPPort()
	if err != nil {
		return nil, err
	}
	httpPort, err := freeTCPPort()
	if err != nil {
		return nil, err
	}

	fastlaneBin, err := buildFastLaneLinuxAMD64(t, repoRoot, workDir)
	if err != nil {
		return nil, err
	}

	xrayBin, err := ensureXrayLinuxAMD64(cacheDir)
	if err != nil {
		return nil, err
	}

	qemuImagePath, err := ensureOpenWrtImage(cacheDir, workDir)
	if err != nil {
		return nil, err
	}

	sshKeyPath := filepath.Join(workDir, "integration-key")
	if err := generateSSHKeyPair(sshKeyPath); err != nil {
		return nil, err
	}

	return &openWRTHarness{
		t:             t,
		repoRoot:      repoRoot,
		workDir:       workDir,
		cacheDir:      cacheDir,
		sshPort:       sshPort,
		httpPort:      httpPort,
		qemuImagePath: qemuImagePath,
		qemuBin:       qemuBin,
		fastlaneBin:   fastlaneBin,
		xrayBin:       xrayBin,
		sshKeyPath:    sshKeyPath,
	}, nil
}

func (h *openWRTHarness) Start(ctx context.Context) error {
	stdoutReader, stdoutWriter := io.Pipe()
	cmd := exec.CommandContext(ctx, h.qemuBin,
		"-accel", "tcg",
		"-m", "512",
		"-display", "none",
		"-monitor", "none",
		"-serial", "stdio",
		"-drive", fmt.Sprintf("file=%s,format=raw", h.qemuImagePath),
		"-nic", fmt.Sprintf("user,model=e1000,hostfwd=tcp::%d-:22,hostfwd=tcp::%d-:80", h.sshPort, h.httpPort),
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("open qemu stdin: %w", err)
	}
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stdoutWriter

	h.console = newConsoleLog(stdoutReader)
	h.consoleStdin = stdin
	h.qemuCmd = cmd

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start qemu: %w", err)
	}

	bootCtx, cancel := context.WithTimeout(ctx, openWrtBootTimeout)
	defer cancel()

	if err := h.ensureConsoleRoot(bootCtx, 0); err != nil {
		return fmt.Errorf("wait for initial OpenWrt console shell: %w", err)
	}

	if err := h.ConsoleCommand(bootCtx, "/etc/init.d/firewall stop"); err != nil {
		return err
	}
	if err := h.ConsoleCommand(bootCtx, "mkdir -p /etc/dropbear"); err != nil {
		return err
	}
	publicKey, err := os.ReadFile(h.sshKeyPath + ".pub")
	if err != nil {
		return fmt.Errorf("read ssh public key: %w", err)
	}
	if err := h.ConsoleCommand(bootCtx, "printf '%s\n' "+shellQuote(strings.TrimSpace(string(publicKey)))+" > /etc/dropbear/authorized_keys"); err != nil {
		return err
	}
	if err := h.ConsoleCommand(bootCtx, fmt.Sprintf("printf '%%s\\n%%s\\n' %s %s | passwd root", shellQuote(luciTestPassword), shellQuote(luciTestPassword))); err != nil {
		return err
	}
	if err := h.ConsoleCommand(bootCtx, "/etc/init.d/dropbear restart"); err != nil {
		return err
	}
	if err := h.ConsoleCommand(bootCtx, "uci set network.lan.proto='dhcp'"); err != nil {
		return err
	}
	if err := h.ConsoleCommand(bootCtx, "uci -q delete network.lan.ipaddr"); err != nil {
		return err
	}
	if err := h.ConsoleCommand(bootCtx, "uci -q delete network.lan.netmask"); err != nil {
		return err
	}
	if err := h.ConsoleCommand(bootCtx, "uci -q delete network.lan.gateway"); err != nil {
		return err
	}
	if err := h.ConsoleCommand(bootCtx, "uci -q delete network.lan.dns"); err != nil {
		return err
	}
	if err := h.ConsoleCommand(bootCtx, "uci commit network"); err != nil {
		return err
	}
	if err := h.ConsoleCommand(bootCtx, "service network restart"); err != nil {
		return err
	}
	if err := h.ConsoleCommand(bootCtx, "sleep 5"); err != nil {
		return err
	}
	if err := h.ConsoleCommand(bootCtx, "/etc/init.d/dropbear restart"); err != nil {
		return err
	}

	if err := h.waitForSSH(bootCtx); err != nil {
		return err
	}
	return nil
}

func (h *openWRTHarness) InstallLuCI(ctx context.Context) error {
	if err := h.sshCommand(ctx, "opkg update"); err != nil {
		return err
	}
	if err := h.sshCommand(ctx, "opkg install luci ca-bundle nftables kmod-nft-tproxy rpcd-mod-file"); err != nil {
		return err
	}
	if err := h.sshCommand(ctx, "opkg list-installed | grep -q '^dnsmasq-full ' || opkg install dnsmasq-full >/dev/null 2>&1 || { opkg remove dnsmasq && opkg install dnsmasq-full; }"); err != nil {
		return fmt.Errorf("install dnsmasq-full: %w", err)
	}
	if err := h.sshCommand(ctx, "dnsmasq --version | grep -q nftset"); err != nil {
		return fmt.Errorf("verify dnsmasq nftset support: %w", err)
	}
	return nil
}

func (h *openWRTHarness) InstallFastLane(ctx context.Context) error {
	if err := h.sshCommand(ctx, "mkdir -p /usr/bin /usr/libexec /etc/fastlane /usr/share/luci/menu.d /usr/share/rpcd/acl.d /www/luci-static/resources/fastlane/assets /www/luci-static/resources/view/fastlane"); err != nil {
		return err
	}
	if err := h.scpFile(ctx, h.fastlaneBin, fastlaneRemoteBinary); err != nil {
		return err
	}
	if err := h.scpFile(ctx, filepath.Join(h.repoRoot, "openwrt", "root", "etc", "init.d", "fastlane"), fastlaneRemoteService); err != nil {
		return err
	}
	for _, helperName := range []string{"fastlane-cron", "fastlane-geodata", "fastlane-self-update", "fastlane-xray-update"} {
		localPath := filepath.Join(h.repoRoot, "openwrt", "root", "usr", "libexec", helperName)
		if err := h.scpFile(ctx, localPath, "/usr/libexec/"+helperName); err != nil {
			return err
		}
	}
	if err := h.scpFile(ctx, filepath.Join(h.repoRoot, "luci-app-fastlane", "root", "usr", "share", "luci", "menu.d", "luci-app-fastlane.json"), "/usr/share/luci/menu.d/luci-app-fastlane.json"); err != nil {
		return err
	}
	if err := h.scpFile(ctx, filepath.Join(h.repoRoot, "luci-app-fastlane", "root", "usr", "share", "rpcd", "acl.d", "luci-app-fastlane.json"), "/usr/share/rpcd/acl.d/luci-app-fastlane.json"); err != nil {
		return err
	}
	for _, pattern := range []struct {
		glob      string
		remoteDir string
	}{
		{
			glob:      filepath.Join(h.repoRoot, "luci-app-fastlane", "htdocs", "luci-static", "resources", "fastlane", "*.js"),
			remoteDir: "/www/luci-static/resources/fastlane",
		},
		{
			glob:      filepath.Join(h.repoRoot, "luci-app-fastlane", "htdocs", "luci-static", "resources", "fastlane", "assets", "*.png"),
			remoteDir: "/www/luci-static/resources/fastlane/assets",
		},
	} {
		matches, err := filepath.Glob(pattern.glob)
		if err != nil {
			return fmt.Errorf("glob LuCI assets %q: %w", pattern.glob, err)
		}
		if len(matches) == 0 {
			return fmt.Errorf("glob LuCI assets %q: no files matched", pattern.glob)
		}
		for _, match := range matches {
			if err := h.scpFile(ctx, match, pattern.remoteDir+"/"+filepath.Base(match)); err != nil {
				return err
			}
		}
	}
	viewFiles, err := productionLuCIViewFiles(h.repoRoot)
	if err != nil {
		return err
	}
	for _, viewFile := range viewFiles {
		localPath := filepath.Join(h.repoRoot, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", filepath.FromSlash(viewFile))
		if err := h.scpFile(ctx, localPath, "/www/luci-static/resources/view/"+viewFile); err != nil {
			return err
		}
	}
	if err := h.sshCommand(ctx, "chmod 0755 "+fastlaneRemoteBinary+" "+fastlaneRemoteService+" /usr/libexec/fastlane-cron /usr/libexec/fastlane-geodata /usr/libexec/fastlane-self-update /usr/libexec/fastlane-xray-update"); err != nil {
		return err
	}
	if err := h.sshCommand(ctx, "test -s /www/luci-static/resources/fastlane/fastlane-20260904-v3.js && test -s /www/luci-static/resources/fastlane/assets/fastlane-mark.png"); err != nil {
		return err
	}
	for _, viewFile := range viewFiles {
		if err := h.sshCommand(ctx, "test -s "+shellQuote("/www/luci-static/resources/view/"+viewFile)); err != nil {
			return fmt.Errorf("verify production LuCI view %s: %w", viewFile, err)
		}
	}
	if err := h.sshCommand(ctx, "rm -f /tmp/luci-indexcache /tmp/luci-indexcache.* && rm -rf /tmp/luci-modulecache"); err != nil {
		return err
	}
	if err := h.sshCommand(ctx, "killall rpcd || true"); err != nil {
		return err
	}
	if err := h.sshCommand(ctx, "/etc/init.d/rpcd start"); err != nil {
		return err
	}
	if err := h.sshCommand(ctx, "/etc/init.d/uhttpd restart"); err != nil {
		return err
	}
	if err := h.sshCommand(ctx, fastlaneRemoteService+" enable"); err != nil {
		return err
	}
	return nil
}

func productionLuCIViewFiles(repoRoot string) ([]string, error) {
	menuPath := filepath.Join(repoRoot, "luci-app-fastlane", "root", "usr", "share", "luci", "menu.d", "luci-app-fastlane.json")
	payload, err := os.ReadFile(menuPath)
	if err != nil {
		return nil, fmt.Errorf("read production LuCI menu: %w", err)
	}
	var menu map[string]struct {
		Action struct {
			Type string `json:"type"`
			Path string `json:"path"`
		} `json:"action"`
	}
	if err := json.Unmarshal(payload, &menu); err != nil {
		return nil, fmt.Errorf("decode production LuCI menu: %w", err)
	}
	viewFiles := make([]string, 0, 4)
	seen := make(map[string]struct{})
	for route, entry := range menu {
		if !strings.HasPrefix(route, "admin/services/fastlane/") || entry.Action.Type != "view" {
			continue
		}
		viewFile := strings.TrimPrefix(filepath.ToSlash(entry.Action.Path), "/") + ".js"
		if !strings.HasPrefix(viewFile, "fastlane/") || strings.Contains(viewFile, "..") {
			return nil, fmt.Errorf("unsafe LuCI view path %q for route %q", entry.Action.Path, route)
		}
		if _, ok := seen[viewFile]; ok {
			continue
		}
		seen[viewFile] = struct{}{}
		viewFiles = append(viewFiles, viewFile)
	}
	if len(viewFiles) != 4 {
		return nil, fmt.Errorf("production LuCI menu exposes %d view files, want 4", len(viewFiles))
	}
	sort.Strings(viewFiles)
	return viewFiles, nil
}

func (h *openWRTHarness) InstallXray(ctx context.Context) error {
	if err := h.sshCommand(ctx, "mkdir -p "+xrayRemoteConfigDir+" /var/log"); err != nil {
		return err
	}
	if err := h.scpFile(ctx, h.xrayBin, xrayRemoteBinary); err != nil {
		return err
	}
	if err := h.scpFile(ctx, filepath.Join(h.repoRoot, "openwrt", "root", "etc", "init.d", "xray"), xrayRemoteService); err != nil {
		return err
	}
	if err := h.sshCommand(ctx, "chmod 0755 "+xrayRemoteBinary+" "+xrayRemoteService); err != nil {
		return err
	}
	return nil
}

func (h *openWRTHarness) AddSubscription(ctx context.Context, raw string) (string, string, error) {
	output, err := h.sshOutput(ctx, fastlaneRemoteBinary+" --json add --raw "+shellQuote(raw))
	if err != nil {
		return "", "", err
	}

	var response struct {
		ID    string `json:"id"`
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return "", "", fmt.Errorf("decode fastlane add response: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if response.ID == "" || len(response.Nodes) == 0 || response.Nodes[0].ID == "" {
		return "", "", fmt.Errorf("unexpected fastlane add response: %s", strings.TrimSpace(string(output)))
	}

	return response.ID, response.Nodes[0].ID, nil
}

func (h *openWRTHarness) Connect(ctx context.Context, subscriptionID, nodeID string) error {
	if err := h.sshCommand(ctx, fmt.Sprintf("%s connect --subscription %s --node %s", fastlaneRemoteBinary, shellQuote(subscriptionID), shellQuote(nodeID))); err != nil {
		return err
	}
	return nil
}

func (h *openWRTHarness) EnableFirewallTargets(ctx context.Context, target string) error {
	return h.sshCommand(ctx, fmt.Sprintf("%s firewall set targets %s", fastlaneRemoteBinary, shellQuote(target)))
}

func (h *openWRTHarness) EnableFirewallAntiTargets(ctx context.Context, target string) error {
	return h.sshCommand(ctx, fmt.Sprintf("%s firewall set anti-target %s", fastlaneRemoteBinary, shellQuote(target)))
}

func (h *openWRTHarness) Disconnect(ctx context.Context) error {
	if err := h.sshCommand(ctx, fastlaneRemoteBinary+" disconnect"); err != nil {
		return err
	}
	if err := h.sshCommand(ctx, fastlaneRemoteBinary+" firewall disable"); err != nil {
		return err
	}
	return nil
}

func (h *openWRTHarness) AssertXrayRunning(ctx context.Context) error {
	output, err := h.sshOutput(ctx, xrayRemoteService+" status")
	if err != nil {
		return err
	}
	if !strings.Contains(strings.ToLower(string(output)), "running") {
		return fmt.Errorf("unexpected xray status: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (h *openWRTHarness) ApplyDefaultDNS(ctx context.Context) error {
	return h.sshCommand(ctx, fastlaneRemoteBinary+" dns default")
}

func (h *openWRTHarness) AssertDNSRuntimeActive(ctx context.Context) error {
	diagnostics, err := h.sshOutput(ctx, fastlaneRemoteBinary+" --json diagnostics")
	if err != nil {
		return err
	}

	var snapshot struct {
		DNS struct {
			Active              bool   `json:"active"`
			LocalDNSListen      string `json:"local_dns_listen"`
			LocalDNSPort        int    `json:"local_dns_port"`
			DNSMasqSnippetFound bool   `json:"dnsmasq_snippet_found"`
		} `json:"dns"`
	}
	if err := json.Unmarshal(diagnostics, &snapshot); err != nil {
		return fmt.Errorf("decode diagnostics dns status: %w", err)
	}
	if !snapshot.DNS.Active || !snapshot.DNS.DNSMasqSnippetFound || snapshot.DNS.LocalDNSListen != "127.0.0.1" || snapshot.DNS.LocalDNSPort != 1053 {
		return fmt.Errorf("unexpected active dns runtime status: %s", strings.TrimSpace(string(diagnostics)))
	}
	if err := h.sshCommand(ctx, "netstat -lnp | grep -q '127.0.0.1:1053'"); err != nil {
		return fmt.Errorf("local dns listener not found: %w", err)
	}
	return nil
}

func (h *openWRTHarness) AssertDNSRuntimeDisabled(ctx context.Context) error {
	diagnostics, err := h.sshOutput(ctx, fastlaneRemoteBinary+" --json diagnostics")
	if err != nil {
		return err
	}

	var snapshot struct {
		DNS struct {
			Active              bool `json:"active"`
			DNSMasqSnippetFound bool `json:"dnsmasq_snippet_found"`
		} `json:"dns"`
	}
	if err := json.Unmarshal(diagnostics, &snapshot); err != nil {
		return fmt.Errorf("decode diagnostics dns status: %w", err)
	}
	if snapshot.DNS.Active || snapshot.DNS.DNSMasqSnippetFound {
		return fmt.Errorf("unexpected disabled dns runtime status: %s", strings.TrimSpace(string(diagnostics)))
	}
	if err := h.sshCommand(ctx, "! netstat -lnp | grep -q '127.0.0.1:1053'"); err != nil {
		return fmt.Errorf("local dns listener still present: %w", err)
	}
	return nil
}

func (h *openWRTHarness) AssertFirewallTableContains(ctx context.Context, needle string) error {
	output, err := h.sshOutput(ctx, "nft list table inet fastlane")
	if err != nil {
		return err
	}
	if !strings.Contains(string(output), needle) {
		return fmt.Errorf("firewall table missing %q\n%s", needle, strings.TrimSpace(string(output)))
	}
	return nil
}

func (h *openWRTHarness) AssertFirewallTableRemoved(ctx context.Context) error {
	output, err := h.sshOutput(ctx, "nft list table inet fastlane")
	if err == nil {
		return fmt.Errorf("expected fastlane nft table to be removed, got:\n%s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (h *openWRTHarness) RebootAndWait(ctx context.Context) error {
	rebootStart := h.console.Len()
	_ = h.sshCommand(ctx, fastlaneRemoteService+" start")
	_ = h.sshCommand(ctx, "sync")
	_ = h.sshCommand(ctx, "reboot")

	downCtx, cancelDown := context.WithTimeout(ctx, 90*time.Second)
	defer cancelDown()
	for {
		if downCtx.Err() != nil {
			return fmt.Errorf("wait for OpenWrt reboot shutdown: %w", downCtx.Err())
		}
		if err := h.sshCommand(downCtx, "true"); err != nil {
			break
		}
		time.Sleep(2 * time.Second)
	}

	upCtx, cancelUp := context.WithTimeout(ctx, 5*time.Minute)
	defer cancelUp()
	if err := h.ensureConsoleRoot(upCtx, rebootStart); err != nil {
		return fmt.Errorf("wait for reboot console shell: %w", err)
	}
	if err := h.ConsoleCommand(upCtx, "service network restart"); err != nil {
		return err
	}
	if err := h.ConsoleCommand(upCtx, "sleep 5"); err != nil {
		return err
	}
	if err := h.ConsoleCommand(upCtx, "/etc/init.d/dropbear restart"); err != nil {
		return err
	}
	if err := h.waitForSSH(upCtx); err != nil {
		return fmt.Errorf("%w\nconsole tail:\n%s", err, tail(h.console.SliceFrom(rebootStart), 4000))
	}
	return nil
}

func (h *openWRTHarness) AssertFastLaneRestore(ctx context.Context, firewallNeedle string) error {
	pollCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	for {
		if pollCtx.Err() != nil {
			return fmt.Errorf("wait for fastlane restore: %w", pollCtx.Err())
		}

		diagnostics, err := h.sshOutput(pollCtx, fastlaneRemoteBinary+" --json diagnostics")
		if err == nil {
			var snapshot struct {
				Status struct {
					State struct {
						Connected         bool   `json:"connected"`
						LastFailureReason string `json:"last_failure_reason"`
					} `json:"state"`
				} `json:"status"`
				Runtime struct {
					Running      bool   `json:"running"`
					ServiceState string `json:"service_state"`
				} `json:"runtime"`
			}
			if json.Unmarshal(diagnostics, &snapshot) == nil &&
				snapshot.Status.State.Connected &&
				snapshot.Runtime.Running &&
				snapshot.Status.State.LastFailureReason == "" {
				if err := h.AssertFirewallTableContains(pollCtx, firewallNeedle); err == nil {
					return nil
				}
			}
		}

		time.Sleep(3 * time.Second)
	}
}

func (h *openWRTHarness) ConsoleCommand(ctx context.Context, command string) error {
	marker := "__FASTLANE_DONE__"
	start := h.console.Len()
	if _, err := io.WriteString(h.consoleStdin, command+"; printf '"+marker+"\\n'\n"); err != nil {
		return fmt.Errorf("write console command %q: %w", command, err)
	}
	if err := h.console.WaitFor(ctx, start, marker); err != nil {
		return fmt.Errorf("run console command %q: %w", command, err)
	}
	return nil
}

func (h *openWRTHarness) ensureConsoleRoot(ctx context.Context, offset int) error {
	if err := h.waitForConsolePrompt(ctx, offset); err != nil {
		return err
	}
	if strings.Contains(h.console.SliceFrom(offset), consoleRootPrompt) {
		return nil
	}

	loginStart := h.console.Len()
	if _, err := io.WriteString(h.consoleStdin, "root\n"); err != nil {
		return fmt.Errorf("log into OpenWrt console: %w", err)
	}
	if err := h.console.WaitFor(ctx, loginStart, consoleRootPrompt); err != nil {
		return fmt.Errorf("wait for OpenWrt shell prompt: %w", err)
	}
	return nil
}

func (h *openWRTHarness) waitForConsolePrompt(ctx context.Context, offset int) error {
	if err := h.console.WaitForAny(ctx, offset, consoleLoginPrompt, consoleRootPrompt, "Please press Enter to activate this console."); err != nil {
		return fmt.Errorf("wait for OpenWrt console prompt: %w", err)
	}

	if !strings.Contains(h.console.SliceFrom(offset), "Please press Enter to activate this console.") {
		return nil
	}

	activateStart := h.console.Len()
	if _, err := io.WriteString(h.consoleStdin, "\n"); err != nil {
		return fmt.Errorf("activate OpenWrt console: %w", err)
	}
	if err := h.console.WaitForAny(ctx, activateStart, consoleLoginPrompt, consoleRootPrompt); err != nil {
		return fmt.Errorf("wait for OpenWrt login state: %w", err)
	}
	return nil
}

func (h *openWRTHarness) waitForSSH(ctx context.Context) error {
	var lastErr error
	for {
		if ctx.Err() != nil {
			if lastErr != nil {
				return fmt.Errorf("wait for ssh: %w; last error: %v", ctx.Err(), lastErr)
			}
			return fmt.Errorf("wait for ssh: %w", ctx.Err())
		}
		if err := h.sshCommand(ctx, "true"); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(2 * time.Second)
	}
}

func (h *openWRTHarness) sshCommand(ctx context.Context, remoteCommand string) error {
	var lastErr error
	for attempt := 1; attempt <= sshRetryAttempts; attempt++ {
		cmd := exec.CommandContext(ctx, "ssh",
			"-i", h.sshKeyPath,
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=5",
			"-o", "LogLevel=ERROR",
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			"-p", fmt.Sprintf("%d", h.sshPort),
			"root@127.0.0.1",
			remoteCommand,
		)
		if output, err := cmd.CombinedOutput(); err != nil {
			lastErr = fmt.Errorf("ssh %q: %w: %s", remoteCommand, err, strings.TrimSpace(string(output)))
			if !isRetryableSSHError(lastErr) || attempt == sshRetryAttempts || ctx.Err() != nil {
				return lastErr
			}
			time.Sleep(sshRetryDelay)
			continue
		}
		return nil
	}
	return lastErr
}

func (h *openWRTHarness) sshOutput(ctx context.Context, remoteCommand string) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= sshRetryAttempts; attempt++ {
		cmd := exec.CommandContext(ctx, "ssh",
			"-i", h.sshKeyPath,
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=5",
			"-o", "LogLevel=ERROR",
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			"-p", fmt.Sprintf("%d", h.sshPort),
			"root@127.0.0.1",
			remoteCommand,
		)
		output, err := cmd.CombinedOutput()
		if err != nil {
			lastErr = fmt.Errorf("ssh %q: %w: %s", remoteCommand, err, strings.TrimSpace(string(output)))
			if !isRetryableSSHError(lastErr) || attempt == sshRetryAttempts || ctx.Err() != nil {
				return nil, lastErr
			}
			time.Sleep(sshRetryDelay)
			continue
		}
		return output, nil
	}
	return nil, lastErr
}

func (h *openWRTHarness) scpFile(ctx context.Context, localPath, remotePath string) error {
	var lastErr error
	for attempt := 1; attempt <= sshRetryAttempts; attempt++ {
		cmd := exec.CommandContext(ctx, "scp",
			"-O",
			"-i", h.sshKeyPath,
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=5",
			"-o", "LogLevel=ERROR",
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			"-P", fmt.Sprintf("%d", h.sshPort),
			localPath,
			"root@127.0.0.1:"+remotePath,
		)
		if output, err := cmd.CombinedOutput(); err != nil {
			lastErr = fmt.Errorf("scp %s -> %s: %w: %s", localPath, remotePath, err, strings.TrimSpace(string(output)))
			if !isRetryableSSHError(lastErr) || attempt == sshRetryAttempts || ctx.Err() != nil {
				return lastErr
			}
			time.Sleep(sshRetryDelay)
			continue
		}
		return nil
	}
	return lastErr
}

func (h *openWRTHarness) Close() {
	if h.consoleStdin != nil {
		_ = h.consoleStdin.Close()
	}
	if h.qemuCmd != nil && h.qemuCmd.Process != nil {
		_ = h.qemuCmd.Process.Kill()
		_, _ = h.qemuCmd.Process.Wait()
	}
}

type consoleLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func newConsoleLog(reader io.Reader) *consoleLog {
	log := &consoleLog{}
	go func() {
		_, _ = io.Copy(log, reader)
	}()
	return log
}

func (c *consoleLog) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *consoleLog) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Len()
}

func (c *consoleLog) SliceFrom(offset int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if offset < 0 {
		offset = 0
	}
	data := c.buf.Bytes()
	if offset >= len(data) {
		return ""
	}
	return string(data[offset:])
}

func (c *consoleLog) WaitFor(ctx context.Context, offset int, needle string) error {
	for {
		if strings.Contains(c.SliceFrom(offset), needle) {
			return nil
		}
		if ctx.Err() != nil {
			return fmt.Errorf("wait for %q: %w\nconsole tail:\n%s", needle, ctx.Err(), tail(c.SliceFrom(0), 4000))
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (c *consoleLog) WaitForAny(ctx context.Context, offset int, needles ...string) error {
	for {
		chunk := c.SliceFrom(offset)
		for _, needle := range needles {
			if strings.Contains(chunk, needle) {
				return nil
			}
		}
		if ctx.Err() != nil {
			return fmt.Errorf("wait for any of %q: %w\nconsole tail:\n%s", strings.Join(needles, ", "), ctx.Err(), tail(c.SliceFrom(0), 4000))
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func buildFastLaneLinuxAMD64(t *testing.T, repoRoot, workDir string) (string, error) {
	t.Helper()

	if path := os.Getenv("FASTLANE_OPENWRT_FASTLANE_BIN"); path != "" {
		return path, nil
	}

	outputPath := filepath.Join(workDir, "fastlane-linux-amd64")
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", outputPath, "./cmd/fastlane")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS=linux",
		"GOARCH=amd64",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build fastlane linux/amd64 binary: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return outputPath, nil
}

func ensureOpenWrtImage(cacheDir, workDir string) (string, error) {
	gzipPath := filepath.Join(cacheDir, filepath.Base(openWrtImageURL))
	rawCachePath := strings.TrimSuffix(gzipPath, ".gz")
	if err := downloadFile(openWrtImageURL, gzipPath); err != nil {
		return "", err
	}
	if err := gunzipFile(gzipPath, rawCachePath); err != nil {
		_ = os.Remove(gzipPath)
		_ = os.Remove(rawCachePath)
		if retryErr := downloadFile(openWrtImageURL, gzipPath); retryErr != nil {
			return "", retryErr
		}
		if retryErr := gunzipFile(gzipPath, rawCachePath); retryErr != nil {
			return "", retryErr
		}
	}

	workingPath := filepath.Join(workDir, filepath.Base(rawCachePath))
	if err := copyFile(rawCachePath, workingPath); err != nil {
		return "", err
	}
	return workingPath, nil
}

func ensureXrayLinuxAMD64(cacheDir string) (string, error) {
	zipPath := filepath.Join(cacheDir, filepath.Base(xrayLinuxAMD64URL))
	binaryPath := filepath.Join(cacheDir, "xray-linux-64", "xray")

	if _, err := os.Stat(binaryPath); err == nil {
		return binaryPath, nil
	}

	if err := downloadFile(xrayLinuxAMD64URL, zipPath); err != nil {
		return "", err
	}
	if err := unzipSingleBinary(zipPath, "xray", binaryPath); err != nil {
		return "", err
	}
	return binaryPath, nil
}

func generateSSHKeyPair(path string) error {
	cmd := exec.Command("ssh-keygen", "-q", "-N", "", "-f", path)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("generate ssh key pair: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func downloadFile(url, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create download dir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), filepath.Base(dest)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create download temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	curlCmd := exec.CommandContext(ctx, "curl", "-fsSL", url)
	curlCmd.Stdout = tmp
	curlCmd.Stderr = os.Stderr
	if err := curlCmd.Run(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("download %s: %w", url, err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close download temp file: %w", err)
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return fmt.Errorf("rename download temp file: %w", err)
	}
	return nil
}

func gunzipFile(src, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create gunzip dir: %w", err)
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open gzip file %s: %w", src, err)
	}
	defer in.Close()

	reader, err := gzip.NewReader(in)
	if err != nil {
		return fmt.Errorf("open gzip reader %s: %w", src, err)
	}
	defer reader.Close()
	reader.Multistream(false)

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create decompressed file %s: %w", dest, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, reader); err != nil {
		return fmt.Errorf("decompress %s: %w", src, err)
	}
	return nil
}

func isRetryableSSHError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, needle := range []string{
		"connection reset by peer",
		"connection refused",
		"connection closed by remote host",
		"kex_exchange_identification",
		"operation timed out",
		"connection timed out",
		"broken pipe",
	} {
		if strings.Contains(message, needle) {
			return true
		}
	}
	return false
}

func unzipSingleBinary(zipPath, binaryName, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create unzip dir: %w", err)
	}

	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip %s: %w", zipPath, err)
	}
	defer reader.Close()

	for _, file := range reader.File {
		if filepath.Base(file.Name) != binaryName {
			continue
		}

		src, err := file.Open()
		if err != nil {
			return fmt.Errorf("open %s from %s: %w", binaryName, zipPath, err)
		}
		defer src.Close()

		out, err := os.Create(dest)
		if err != nil {
			return fmt.Errorf("create %s: %w", dest, err)
		}
		if _, err := io.Copy(out, src); err != nil {
			_ = out.Close()
			return fmt.Errorf("extract %s: %w", binaryName, err)
		}
		if err := out.Close(); err != nil {
			return fmt.Errorf("close %s: %w", dest, err)
		}
		if err := os.Chmod(dest, 0o755); err != nil {
			return fmt.Errorf("chmod %s: %w", dest, err)
		}
		return nil
	}

	return fmt.Errorf("binary %s not found in %s", binaryName, zipPath)
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s -> %s: %w", src, dest, err)
	}
	return nil
}

func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", "..", "..")), nil
}

func freeTCPPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve tcp port: %w", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func tail(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}
