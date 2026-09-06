package release_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUninstallScriptRemovesFastLaneAndXrayArtifacts(t *testing.T) {
	t.Parallel()

	scriptPath := filepath.Join(repoRoot(t), "scripts", "uninstall.sh")
	installRoot := t.TempDir()
	serviceLogPath := filepath.Join(t.TempDir(), "services.log")
	opkgStatePath := filepath.Join(t.TempDir(), "opkg-state.txt")

	binDir := t.TempDir()
	writeInstallOpkgStub(t, filepath.Join(binDir, "opkg"), "mipsel_24kc")

	writeExecutable(t, filepath.Join(installRoot, "usr", "bin", "fastlane"), "#!/bin/sh\nset -eu\nprintf 'fastlane-bin:%s\\n' \"$*\" >> \"${FASTLANE_TEST_SERVICE_LOG:?}\"\n")
	writeServiceStub(t, filepath.Join(installRoot, "etc", "init.d", "fastlane"))
	writeExecutable(t, filepath.Join(installRoot, "usr", "bin", "xray"), "#!/bin/sh\nexit 0\n")
	writeServiceStub(t, filepath.Join(installRoot, "etc", "init.d", "xray"))
	writeServiceStub(t, filepath.Join(installRoot, "etc", "init.d", "zapret"))
	writeFile(t, serviceLogPath, "", 0o644)
	writeServiceStub(t, filepath.Join(installRoot, "etc", "init.d", "cron"))
	writeServiceStub(t, filepath.Join(installRoot, "etc", "init.d", "rpcd"))
	writeServiceStub(t, filepath.Join(installRoot, "etc", "init.d", "uhttpd"))

	writeFile(t, filepath.Join(installRoot, "etc", "fastlane", "subscriptions.json"), "{}\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "fastlane", "settings.json"), "{}\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "fastlane", "state.json"), "{}\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "fastlane", "speedtest.lock"), "", 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "fastlane", "install-manifest.txt"), strings.Join([]string{
		"pkg=ca-bundle",
		"pkg=curl",
		"pkg=nftables",
		"pkg=kmod-nft-tproxy",
		"pkg=dnsmasq-full",
		"pkg=unzip",
		"pkg=zapret",
		"runtime=xray",
		"restore=dnsmasq",
		"",
	}, "\n"), 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "xray", "config.json"), "{}\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "xray", "config.json.last-known-good"), "{}\n", 0o644)
	cronHelper, err := os.ReadFile(filepath.Join(repoRoot(t), "openwrt", "root", "usr", "libexec", "fastlane-cron"))
	if err != nil {
		t.Fatalf("read fastlane cron helper: %v", err)
	}
	writeExecutable(t, filepath.Join(installRoot, "usr", "libexec", "fastlane-cron"), string(cronHelper))
	selfUpdateHelper, err := os.ReadFile(filepath.Join(repoRoot(t), "openwrt", "root", "usr", "libexec", "fastlane-self-update"))
	if err != nil {
		t.Fatalf("read fastlane self-update helper: %v", err)
	}
	writeExecutable(t, filepath.Join(installRoot, "usr", "libexec", "fastlane-self-update"), string(selfUpdateHelper))
	xrayUpdateHelper, err := os.ReadFile(filepath.Join(repoRoot(t), "openwrt", "root", "usr", "libexec", "fastlane-xray-update"))
	if err != nil {
		t.Fatalf("read fastlane xray update helper: %v", err)
	}
	writeExecutable(t, filepath.Join(installRoot, "usr", "libexec", "fastlane-xray-update"), string(xrayUpdateHelper))
	writeFile(t, filepath.Join(installRoot, "etc", "crontabs", "root"), strings.Join([]string{
		"15 4 * * * echo keep",
		"# fastlane:xray-log-retention:start",
		"0 * * * * [ -f /var/log/xray.log ] && : > /var/log/xray.log",
		"# fastlane:xray-log-retention:end",
		"",
	}, "\n"), 0o644)
	writeFile(t, filepath.Join(installRoot, "var", "log", "xray.log"), "log\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "var", "run", "xray.pid"), "123\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "usr", "share", "luci", "menu.d", "luci-app-fastlane.json"), "{}\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "usr", "share", "rpcd", "acl.d", "luci-app-fastlane.json"), "{}\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "www", "luci-static", "resources", "fastlane", "ui.js"), "'use strict';\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "www", "luci-static", "resources", "view", "fastlane", "subscriptions.js"), "'use strict';\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "rc.d", "S95fastlane"), "", 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "rc.d", "S95xray"), "", 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "rc.d", "S95zapret"), "", 0o644)
	writeFile(t, filepath.Join(installRoot, "tmp", "fastlane-firewall.nft"), "table inet fastlane {}\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "tmp", "fastlane-speedtest-123", "config.json"), "{}\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "tmp", "xray-cache"), "cache\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "tmp", "lock", "procd_fastlane.lock"), "", 0o644)
	writeFile(t, filepath.Join(installRoot, "tmp", "lock", "procd_zapret.lock"), "", 0o644)
	writeFile(t, filepath.Join(installRoot, "tmp", "luci-indexcache"), "cache\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "tmp", "luci-modulecache", "index"), "cache\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "opt", "zapret", "ipset", "zapret-hosts-user.txt"), "youtube.com\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "opt", "zapret", "ipset", "zapret-hosts-user.txt.fastlane.bak"), "original.example.com\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "opt", "zapret", "ipset", "zapret-ip-user.txt"), "91.108.0.0/16\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "opt", "zapret", "ipset", "zapret-ip-user.txt.fastlane.bak"), "203.0.113.0/24\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "config", "zapret"), "config zapret 'base'\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "hotplug.d", "iface", "90-zapret"), "#!/bin/sh\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "sysctl.d", "99-fastlane-ipv6.conf"), "# Managed by Fast Lane\nnet.ipv6.conf.all.disable_ipv6=1\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "init.d", "fastlane.bak.20260327-233221"), "#!/bin/sh\n", 0o755)
	writeFile(t, filepath.Join(installRoot, "etc", "opkg", "customfeeds.conf"), "src/gz fastlane https://github.com/design-maestro/fastlane/releases/download/v0.1.4\nsrc/gz other https://example.invalid/feed\nsrc/gz fastlane https://github.com/design-maestro/fastlane/releases/download/v0.1.4\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "opkg", "keys", "9e842876f8b9501d"), "untrusted comment: Fast Lane opkg feed\nPUBLICKEY\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "fastlane", "zapret-managed.json"), "{\"domains\":[\"youtube.com\"]}\n", 0o644)
	writeFile(t, opkgStatePath, strings.Join([]string{
		"base-files",
		"ca-bundle",
		"curl",
		"nftables",
		"kmod-nft-tproxy",
		"dnsmasq-full",
		"unzip",
		"zapret",
		"",
	}, "\n"), 0o644)

	stdout, stderr, err := runUninstallScriptWithEnv(
		t,
		scriptPath,
		installRoot,
		serviceLogPath,
		binDir,
		map[string]string{
			"FASTLANE_TEST_BIN_DIR":      binDir,
			"FASTLANE_TEST_INSTALL_ROOT": installRoot,
			"FASTLANE_TEST_OPKG_STATE":   opkgStatePath,
		},
	)
	if err != nil {
		t.Fatalf("run uninstall script: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	for _, path := range []string{
		filepath.Join(installRoot, "usr", "bin", "fastlane"),
		filepath.Join(installRoot, "etc", "init.d", "fastlane"),
		filepath.Join(installRoot, "etc", "fastlane"),
		filepath.Join(installRoot, "usr", "bin", "xray"),
		filepath.Join(installRoot, "etc", "init.d", "xray"),
		filepath.Join(installRoot, "etc", "xray"),
		filepath.Join(installRoot, "usr", "libexec", "fastlane-cron"),
		filepath.Join(installRoot, "usr", "libexec", "fastlane-self-update"),
		filepath.Join(installRoot, "usr", "libexec", "fastlane-xray-update"),
		filepath.Join(installRoot, "var", "log", "xray.log"),
		filepath.Join(installRoot, "var", "run", "xray.pid"),
		filepath.Join(installRoot, "usr", "share", "luci", "menu.d", "luci-app-fastlane.json"),
		filepath.Join(installRoot, "usr", "share", "rpcd", "acl.d", "luci-app-fastlane.json"),
		filepath.Join(installRoot, "www", "luci-static", "resources", "fastlane"),
		filepath.Join(installRoot, "www", "luci-static", "resources", "view", "fastlane"),
		filepath.Join(installRoot, "etc", "rc.d", "S95fastlane"),
		filepath.Join(installRoot, "etc", "rc.d", "S95xray"),
		filepath.Join(installRoot, "etc", "sysctl.d", "99-fastlane-ipv6.conf"),
		filepath.Join(installRoot, "etc", "init.d", "fastlane.bak.20260327-233221"),
		filepath.Join(installRoot, "tmp", "fastlane-firewall.nft"),
		filepath.Join(installRoot, "tmp", "fastlane-speedtest-123"),
		filepath.Join(installRoot, "tmp", "xray-cache"),
		filepath.Join(installRoot, "tmp", "lock", "procd_fastlane.lock"),
		filepath.Join(installRoot, "tmp", "luci-indexcache"),
		filepath.Join(installRoot, "tmp", "luci-modulecache"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err=%v", path, err)
		}
	}
	for _, path := range []string{
		filepath.Join(installRoot, "etc", "init.d", "zapret"),
		filepath.Join(installRoot, "opt", "zapret"),
		filepath.Join(installRoot, "etc", "rc.d", "S95zapret"),
		filepath.Join(installRoot, "etc", "config", "zapret"),
		filepath.Join(installRoot, "etc", "hotplug.d", "iface", "90-zapret"),
		filepath.Join(installRoot, "tmp", "lock", "procd_zapret.lock"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected external Zapret artifact %s to be preserved: %v", path, err)
		}
	}

	serviceLog, err := os.ReadFile(serviceLogPath)
	if err != nil {
		t.Fatalf("read service log: %v", err)
	}

	for _, want := range []string{
		"fastlane-bin:--root ",
		" disconnect",
		" firewall disable",
		"cron:restart",
		"fastlane:stop",
		"fastlane:disable",
		"xray:stop",
		"xray:disable",
		"opkg:remove:unzip",
		"opkg:remove:dnsmasq-full",
		"opkg:remove:kmod-nft-tproxy",
		"opkg:remove:nftables",
		"opkg:remove:curl",
		"opkg:remove:ca-bundle",
		"opkg:install:dnsmasq",
		"rpcd:reload",
		"uhttpd:reload",
	} {
		if !strings.Contains(string(serviceLog), want) {
			t.Fatalf("expected service log to contain %q, got %q", want, string(serviceLog))
		}
	}

	if !strings.Contains(stdout, "external Xray and Zapret were preserved") {
		t.Fatalf("expected completion message in stdout, got %q", stdout)
	}

	opkgState, err := os.ReadFile(opkgStatePath)
	if err != nil {
		t.Fatalf("read opkg state: %v", err)
	}
	for _, unwanted := range []string{
		"ca-bundle",
		"curl",
		"nftables",
		"kmod-nft-tproxy",
		"dnsmasq-full",
		"unzip",
	} {
		if strings.Contains(string(opkgState), unwanted+"\n") {
			t.Fatalf("expected %q to be removed from opkg state, got %q", unwanted, string(opkgState))
		}
	}
	if !strings.Contains(string(opkgState), "zapret\n") {
		t.Fatalf("expected external Zapret package to remain, got %q", string(opkgState))
	}
	if !strings.Contains(string(opkgState), "dnsmasq\n") {
		t.Fatalf("expected dnsmasq to be restored, got %q", string(opkgState))
	}
	if !strings.Contains(string(opkgState), "base-files\n") {
		t.Fatalf("expected unrelated packages to remain, got %q", string(opkgState))
	}

	crontabPath := filepath.Join(installRoot, "etc", "crontabs", "root")
	contents, err := os.ReadFile(crontabPath)
	if err != nil {
		t.Fatalf("read crontab: %v", err)
	}
	if strings.Contains(string(contents), "fastlane:xray-log-retention") {
		t.Fatalf("expected managed cron block to be removed, got %q", string(contents))
	}
	if !strings.Contains(string(contents), "15 4 * * * echo keep") {
		t.Fatalf("expected unrelated cron entry to remain, got %q", string(contents))
	}

	customfeeds, err := os.ReadFile(filepath.Join(installRoot, "etc", "opkg", "customfeeds.conf"))
	if err != nil {
		t.Fatalf("read customfeeds.conf: %v", err)
	}
	if strings.Contains(string(customfeeds), "fastlane") {
		t.Fatalf("expected fastlane feed entries to be removed, got %q", string(customfeeds))
	}
	if !strings.Contains(string(customfeeds), "src/gz other https://example.invalid/feed") {
		t.Fatalf("expected unrelated feed entry to remain, got %q", string(customfeeds))
	}
	if _, err := os.Stat(filepath.Join(installRoot, "etc", "opkg", "keys", "9e842876f8b9501d")); !os.IsNotExist(err) {
		t.Fatalf("expected fastlane opkg key to be removed, stat err=%v", err)
	}
}

func TestUninstallScriptPreservesLegacyExternalZapretWithoutManifest(t *testing.T) {
	t.Parallel()

	scriptPath := filepath.Join(repoRoot(t), "scripts", "uninstall.sh")
	installRoot := t.TempDir()
	serviceLogPath := filepath.Join(t.TempDir(), "services.log")
	opkgStatePath := filepath.Join(t.TempDir(), "opkg-state.txt")

	binDir := t.TempDir()
	writeInstallOpkgStub(t, filepath.Join(binDir, "opkg"), "mipsel_24kc")

	writeServiceStub(t, filepath.Join(installRoot, "etc", "init.d", "zapret"))
	writeFile(t, serviceLogPath, "", 0o644)
	writeFile(t, filepath.Join(installRoot, "usr", "libexec", "fastlane-xray-update"), "#!/bin/sh\nexit 0\n", 0o755)
	writeFile(t, filepath.Join(installRoot, "etc", "init.d", "fastlane.bak.20260327-233221"), "#!/bin/sh\n", 0o755)
	writeFile(t, filepath.Join(installRoot, "etc", "sysctl.d", "99-fastlane-ipv6.conf"), "# Managed by Fast Lane\nnet.ipv6.conf.all.disable_ipv6=1\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "config", "zapret"), "config zapret 'base'\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "hotplug.d", "iface", "90-zapret"), "#!/bin/sh\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "opkg", "customfeeds.conf"), "src/gz fastlane https://github.com/design-maestro/fastlane/releases/download/v0.1.4\nsrc/gz keep https://example.invalid/feed\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "etc", "opkg", "keys", "9e842876f8b9501d"), "untrusted comment: Fast Lane opkg feed\nPUBLICKEY\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "opt", "zapret", "config"), "# config\n", 0o644)
	writeFile(t, filepath.Join(installRoot, "tmp", "lock", "procd_fastlane.lock"), "", 0o644)
	writeFile(t, filepath.Join(installRoot, "tmp", "lock", "procd_zapret.lock"), "", 0o644)
	writeFile(t, opkgStatePath, "base-files\nzapret\n", 0o644)

	stdout, stderr, err := runUninstallScriptWithEnv(
		t,
		scriptPath,
		installRoot,
		serviceLogPath,
		binDir,
		map[string]string{
			"FASTLANE_TEST_BIN_DIR":      binDir,
			"FASTLANE_TEST_INSTALL_ROOT": installRoot,
			"FASTLANE_TEST_OPKG_STATE":   opkgStatePath,
		},
	)
	if err != nil {
		t.Fatalf("run uninstall script: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	for _, path := range []string{
		filepath.Join(installRoot, "usr", "libexec", "fastlane-xray-update"),
		filepath.Join(installRoot, "etc", "init.d", "fastlane.bak.20260327-233221"),
		filepath.Join(installRoot, "etc", "sysctl.d", "99-fastlane-ipv6.conf"),
		filepath.Join(installRoot, "tmp", "lock", "procd_fastlane.lock"),
		filepath.Join(installRoot, "etc", "opkg", "keys", "9e842876f8b9501d"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err=%v", path, err)
		}
	}
	for _, path := range []string{
		filepath.Join(installRoot, "etc", "config", "zapret"),
		filepath.Join(installRoot, "etc", "hotplug.d", "iface", "90-zapret"),
		filepath.Join(installRoot, "opt", "zapret"),
		filepath.Join(installRoot, "tmp", "lock", "procd_zapret.lock"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected external Zapret artifact %s to be preserved: %v", path, err)
		}
	}

	customfeeds, err := os.ReadFile(filepath.Join(installRoot, "etc", "opkg", "customfeeds.conf"))
	if err != nil {
		t.Fatalf("read customfeeds.conf: %v", err)
	}
	if strings.Contains(string(customfeeds), "fastlane") {
		t.Fatalf("expected fastlane feed entries to be removed, got %q", string(customfeeds))
	}
	if !strings.Contains(string(customfeeds), "src/gz keep https://example.invalid/feed") {
		t.Fatalf("expected unrelated feed entry to remain, got %q", string(customfeeds))
	}

	serviceLog, err := os.ReadFile(serviceLogPath)
	if err != nil {
		t.Fatalf("read service log: %v", err)
	}
	for _, unwanted := range []string{"zapret:stop", "zapret:disable", "opkg:remove:zapret"} {
		if strings.Contains(string(serviceLog), unwanted) {
			t.Fatalf("external Zapret must not be changed; found %q in %q", unwanted, string(serviceLog))
		}
	}

	opkgState, err := os.ReadFile(opkgStatePath)
	if err != nil {
		t.Fatalf("read opkg state: %v", err)
	}
	if !strings.Contains(string(opkgState), "zapret\n") {
		t.Fatalf("expected Zapret to remain in opkg state, got %q", string(opkgState))
	}
	if !strings.Contains(stdout, "external Xray and Zapret were preserved") {
		t.Fatalf("expected completion message in stdout, got %q", stdout)
	}
}

func TestUninstallScriptSucceedsWhenArtifactsAreMissing(t *testing.T) {
	t.Parallel()

	scriptPath := filepath.Join(repoRoot(t), "scripts", "uninstall.sh")
	installRoot := t.TempDir()

	stdout, stderr, err := runUninstallScript(t, scriptPath, installRoot, filepath.Join(t.TempDir(), "services.log"))
	if err != nil {
		t.Fatalf("run uninstall script: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
}

func TestUninstallScriptRequiresExplicitConfirmationOnLiveRoot(t *testing.T) {
	t.Parallel()

	scriptPath := filepath.Join(repoRoot(t), "scripts", "uninstall.sh")
	cmd := exec.Command("sh", scriptPath, "--install-root", t.TempDir(), "--dry-run")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dry-run must remain available without confirmation: %v\n%s", err, output)
	}

	source, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read uninstall script: %v", err)
	}
	for _, want := range []string{
		`FASTLANE_CONFIRMED=0`,
		`--confirm)`,
		`refusing to uninstall Fast Lane without --confirm`,
	} {
		if !strings.Contains(string(source), want) {
			t.Fatalf("uninstall script missing confirmation guard %q", want)
		}
	}
}

func runUninstallScript(t *testing.T, scriptPath, installRoot, serviceLogPath string, extraArgs ...string) (string, string, error) {
	t.Helper()

	return runUninstallScriptWithEnv(t, scriptPath, installRoot, serviceLogPath, "", nil, extraArgs...)
}

func runUninstallScriptWithEnv(
	t *testing.T,
	scriptPath, installRoot, serviceLogPath, binDir string,
	extraEnv map[string]string,
	extraArgs ...string,
) (string, string, error) {
	t.Helper()

	args := append([]string{scriptPath, "--install-root", installRoot}, extraArgs...)
	cmd := exec.Command("sh", args...)
	env := append(os.Environ(),
		"FASTLANE_TEST_SERVICE_LOG="+serviceLogPath,
	)
	if binDir != "" {
		env = append(env, "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	for key, value := range extraEnv {
		env = append(env, key+"="+value)
	}
	cmd.Env = env

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}
