package release_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageOpenWrtFallsBackToTarWhenBSDTarMissing(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	scriptSource, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts", "package-openwrt.sh"))
	if err != nil {
		t.Fatalf("read package-openwrt.sh: %v", err)
	}

	writeExecutable(t, filepath.Join(repoDir, "scripts", "package-openwrt.sh"), string(scriptSource))
	writeExecutable(t, filepath.Join(repoDir, "bin", "openwrt", "x86_64", "fastlane"), "#!/bin/sh\nprintf 'fastlane test binary\\n'\n")
	writeExecutable(t, filepath.Join(repoDir, "openwrt", "root", "etc", "init.d", "fastlane"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(repoDir, "openwrt", "root", "usr", "libexec", "fastlane-cron"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(repoDir, "openwrt", "root", "usr", "libexec", "fastlane-self-update"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(repoDir, "openwrt", "root", "usr", "libexec", "fastlane-xray-update"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(repoDir, "openwrt", "root", "usr", "libexec", "fastlane-geodata"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(repoDir, "scripts", "uninstall.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "root", "usr", "share", "luci", "menu.d", "luci-app-fastlane.json"), "{}\n", 0o644)
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "root", "usr", "share", "rpcd", "acl.d", "luci-app-fastlane.json"), "{}\n", 0o644)
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "po", "ru", "fastlane.po"), "msgid \"Settings\"\nmsgstr \"Настройки\"\n", 0o644)
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "htdocs", "luci-static", "resources", "fastlane", "ui.js"), "'use strict';\n", 0o644)
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "htdocs", "luci-static", "resources", "fastlane", "assets", "fastlane-mark.png"), "png", 0o644)
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "subscriptions.js"), "'use strict';\n", 0o644)
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "vpn.js"), "'use strict';\n", 0o644)
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "vpn-20260905-latency-v18.js"), "'use strict';\n", 0o644)
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "routing.js"), "'use strict';\n", 0o644)
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "routing-20260904-v3.js"), "'use strict';\n", 0o644)
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "firewall.js"), "'use strict';\n", 0o644)
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "dns.js"), "'use strict';\n", 0o644)
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "about.js"), "'use strict';\n", 0o644)
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "overview.js"), "'use strict';\n", 0o644)
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "diagnostics.js"), "'use strict';\n", 0o644)
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "diagnostics-20260904-v3.js"), "'use strict';\n", 0o644)
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "zapret.js"), "'use strict';\n", 0o644)
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "settings.js"), "'use strict';\n", 0o644)
	writeFile(t, filepath.Join(repoDir, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "settings-20260905-updates-v6.js"), "'use strict';\n", 0o644)

	toolDir := t.TempDir()
	writeExecutable(t, filepath.Join(toolDir, "po2lmo"), "#!/bin/sh\nprintf 'compiled translation' > \"$2\"\n")
	for _, name := range []string{"awk", "basename", "cat", "chmod", "cp", "date", "dirname", "find", "gzip", "mkdir", "rm", "shasum", "sort", "tar", "tr", "wc"} {
		target, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("find %s: %v", name, err)
		}
		if err := os.Symlink(target, filepath.Join(toolDir, name)); err != nil {
			t.Fatalf("symlink %s: %v", name, err)
		}
	}

	cmd := exec.Command("sh", filepath.Join(repoDir, "scripts", "package-openwrt.sh"))
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"PATH="+toolDir,
		"VERSION=1.2.3",
		"ARCH=x86_64",
		"BINARY_PATH="+filepath.Join(repoDir, "bin", "openwrt", "x86_64", "fastlane"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run package-openwrt.sh: %v\n%s", err, output)
	}

	if _, err := os.Stat(filepath.Join(repoDir, "dist", "fastlane_1.2.3_x86_64.ipk")); err != nil {
		t.Fatalf("expected ipk artifact: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "dist", "fastlane_1.2.3_x86_64.tar.gz")); err != nil {
		t.Fatalf("expected tarball artifact: %v", err)
	}
	control, err := os.ReadFile(filepath.Join(repoDir, "dist", "fastlane-ipk", "control", "control"))
	if err != nil {
		t.Fatalf("read generated package control: %v", err)
	}
	if !strings.Contains(string(control), "Depends: ca-bundle, nftables, kmod-nft-tproxy, rpcd-mod-file\n") {
		t.Fatalf("expected generated package control to declare OpenWrt runtime dependencies, got:\n%s", control)
	}
	if !strings.Contains(string(control), "This standalone IPK requires an existing /usr/bin/xray runtime") {
		t.Fatalf("expected generated package control to disclose the external Xray runtime, got:\n%s", control)
	}
	for _, path := range []string{
		filepath.Join(repoDir, "dist", "fastlane_1.2.3_x86_64.ipk.sha256"),
		filepath.Join(repoDir, "dist", "fastlane_1.2.3_x86_64.tar.gz.sha256"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected checksum artifact %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(repoDir, "dist", "fastlane-ipk", "data", "www", "luci-static", "resources", "fastlane", "ui.js")); err != nil {
		t.Fatalf("expected shared fastlane ui helper in package data: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "dist", "fastlane-ipk", "data", "www", "luci-static", "resources", "fastlane", "assets", "fastlane-mark.png")); err != nil {
		t.Fatalf("expected Fast Lane visual assets in package data: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "dist", "fastlane-ipk", "data", "www", "luci-static", "resources", "view", "fastlane", "vpn.js")); err != nil {
		t.Fatalf("expected Fast Lane VPN view in package data: %v", err)
	}
	translationPath := filepath.Join(repoDir, "dist", "fastlane-ipk", "data", "usr", "lib", "lua", "luci", "i18n", "fastlane.ru.lmo")
	if info, err := os.Stat(translationPath); err != nil {
		t.Fatalf("expected compiled Russian LuCI translation in package data: %v", err)
	} else if info.Size() == 0 {
		t.Fatal("compiled Russian LuCI translation must not be empty")
	}
	languageDefaultsPath := filepath.Join(repoDir, "dist", "fastlane-ipk", "data", "etc", "uci-defaults", "luci-i18n-fastlane-ru")
	if info, err := os.Stat(languageDefaultsPath); err != nil {
		t.Fatalf("expected Russian language registration in package data: %v", err)
	} else if info.Mode().Perm() != 0o755 {
		t.Fatalf("expected Russian language registration to be executable, got mode %o", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(repoDir, "dist", "fastlane-ipk", "data", "www", "luci-static", "resources", "view", "fastlane", "services.js")); err == nil {
		t.Fatal("services view must not be packaged anymore")
	}
	if _, err := os.Stat(filepath.Join(repoDir, "dist", "fastlane-ipk", "data", "www", "luci-static", "resources", "view", "fastlane", "diagnostics.js")); err != nil {
		t.Fatalf("expected diagnostics view in package data: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "dist", "fastlane-ipk", "data", "www", "luci-static", "resources", "view", "fastlane", "settings.js")); err != nil {
		t.Fatalf("expected settings view in package data: %v", err)
	}
	for _, name := range []string{
		"vpn-20260905-latency-v18.js",
		"routing-20260904-v3.js",
		"diagnostics-20260904-v3.js",
		"settings-20260905-updates-v6.js",
	} {
		path := filepath.Join(repoDir, "dist", "fastlane-ipk", "data", "www", "luci-static", "resources", "view", "fastlane", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected versioned menu view %s in package data: %v", name, err)
		}
	}
	for _, name := range []string{"subscriptions.js", "firewall.js", "dns.js", "about.js", "overview.js", "zapret.js"} {
		path := filepath.Join(repoDir, "dist", "fastlane-ipk", "data", "www", "luci-static", "resources", "view", "fastlane", name)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("obsolete MVP view %s must not be packaged", name)
		}
	}
	if _, err := os.Stat(filepath.Join(repoDir, "dist", "fastlane-ipk", "data", "usr", "libexec", "fastlane-cron")); err != nil {
		t.Fatalf("expected cron helper in package data: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "dist", "fastlane-ipk", "data", "usr", "libexec", "fastlane-self-update")); err != nil {
		t.Fatalf("expected self-update helper in package data: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "dist", "fastlane-ipk", "data", "usr", "libexec", "fastlane-xray-update")); err != nil {
		t.Fatalf("expected xray update helper in package data: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "dist", "fastlane-ipk", "data", "usr", "libexec", "fastlane-geodata")); err != nil {
		t.Fatalf("expected geodata helper in package data: %v", err)
	}
	if info, err := os.Stat(filepath.Join(repoDir, "dist", "fastlane-ipk", "data", "usr", "libexec", "fastlane-uninstall")); err != nil {
		t.Fatalf("expected uninstall helper in package data: %v", err)
	} else if info.Mode().Perm() != 0o755 {
		t.Fatalf("expected uninstall helper to be executable, got mode %o", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(repoDir, "dist", "fastlane-ipk", "data", "usr", "bin", "xray")); !os.IsNotExist(err) {
		t.Fatalf("standalone IPK must not imply that it bundles the Xray binary: %v", err)
	}
	postinstPath := filepath.Join(repoDir, "dist", "fastlane-ipk", "control", "postinst")
	postinst, err := os.ReadFile(postinstPath)
	if err != nil {
		t.Fatalf("read generated postinst: %v", err)
	}
	for _, want := range []string{
		"chmod 0700 /etc/fastlane",
		"/etc/fastlane/.fastlane.lock",
		"/etc/fastlane/speedtest.lock",
		"find /etc/fastlane -maxdepth 1 -type f -name '*.corrupt-*' -exec chmod 0600 {} \\;",
		"/etc/xray/config.json.last-known-good",
		"/www/luci-static/resources/view/fastlane/logs.js",
		"language_defaults=/etc/uci-defaults/luci-i18n-fastlane-ru",
	} {
		if !strings.Contains(string(postinst), want) {
			t.Fatalf("expected generated postinst to contain %q, got:\n%s", want, postinst)
		}
	}

	for _, forbidden := range []string{
		"/www/luci-static/resources/view/fastlane/dns.js",
	} {
		if strings.Contains(string(postinst), forbidden) {
			t.Fatalf("expected generated postinst to keep %q installed, got:\n%s", forbidden, postinst)
		}
	}

	if strings.Contains(string(postinst), "/www/luci-static/resources/view/fastlane/about.js") {
		t.Fatalf("expected generated postinst to keep about.js installed, got:\n%s", postinst)
	}
	if strings.Contains(string(postinst), "/www/luci-static/resources/view/fastlane/settings.js") {
		t.Fatalf("expected generated postinst to keep settings.js installed, got:\n%s", postinst)
	}

	testOpenWrtPostinstContract(t, string(postinst))
}

func testOpenWrtPostinstContract(t *testing.T, postinst string) {
	t.Helper()

	testCases := []struct {
		name        string
		staging     bool
		upgrade     bool
		running     bool
		xrayPresent bool
		wantCalls   string
		wantError   bool
	}{
		{
			name:        "fresh live install enables and starts",
			xrayPresent: true,
			wantCalls:   "enable\nstart\n",
		},
		{
			name:        "running upgrade restarts without enabling",
			upgrade:     true,
			running:     true,
			xrayPresent: true,
			wantCalls:   "running\nrestart\n",
		},
		{
			name:        "stopped upgrade remains stopped",
			upgrade:     true,
			xrayPresent: true,
			wantCalls:   "running\n",
		},
		{
			name:      "staging has no live side effects",
			staging:   true,
			wantCalls: "",
		},
		{
			name:      "missing Xray fails before enabling service",
			wantCalls: "",
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			installRoot := t.TempDir()
			callsPath := filepath.Join(installRoot, "fastlane-service.log")
			rewritten := rewritePostinstRoot(postinst, installRoot)
			postinstPath := filepath.Join(installRoot, "postinst")
			writeExecutable(t, postinstPath, rewritten)

			serviceScript := `#!/bin/sh
set -eu
printf '%s\n' "${1:-}" >> "${FASTLANE_TEST_SERVICE_LOG:?}"
if [ "${1:-}" = "running" ] && [ "${FASTLANE_TEST_RUNNING:-0}" != "1" ]; then
	exit 1
fi
exit 0
`
			writeExecutable(t, filepath.Join(installRoot, "etc", "init.d", "fastlane"), serviceScript)
			writeExecutable(t, filepath.Join(installRoot, "etc", "init.d", "rpcd"), "#!/bin/sh\nexit 0\n")
			writeExecutable(t, filepath.Join(installRoot, "etc", "init.d", "uhttpd"), "#!/bin/sh\nexit 0\n")
			writeExecutable(t, filepath.Join(installRoot, "usr", "libexec", "fastlane-cron"), "#!/bin/sh\nexit 0\n")
			if tc.xrayPresent {
				writeExecutable(t, filepath.Join(installRoot, "usr", "bin", "xray"), "#!/bin/sh\nexit 0\n")
			}

			cmd := exec.Command("sh", postinstPath, "configure")
			cmd.Env = []string{
				"PATH=" + os.Getenv("PATH"),
				"IPKG_INSTROOT=" + map[bool]string{true: installRoot}[tc.staging],
				"PKG_UPGRADE=" + map[bool]string{true: "1", false: "0"}[tc.upgrade],
				"FASTLANE_TEST_RUNNING=" + map[bool]string{true: "1", false: "0"}[tc.running],
				"FASTLANE_TEST_SERVICE_LOG=" + callsPath,
			}
			output, err := cmd.CombinedOutput()
			if tc.wantError {
				if err == nil {
					t.Fatalf("expected postinst failure, got success with output:\n%s", output)
				}
				if !strings.Contains(string(output), "required Xray runtime") {
					t.Fatalf("expected actionable Xray error, got:\n%s", output)
				}
			} else if err != nil {
				t.Fatalf("run postinst: %v\n%s", err, output)
			}

			calls, err := os.ReadFile(callsPath)
			if err != nil && !os.IsNotExist(err) {
				t.Fatalf("read service calls: %v", err)
			}
			if got := string(calls); got != tc.wantCalls {
				t.Fatalf("unexpected fastlane service calls:\nwant %q\n got %q", tc.wantCalls, got)
			}
		})
	}
}

func rewritePostinstRoot(postinst, root string) string {
	paths := []string{
		"/www/luci-static/resources/view/fastlane/logs.js",
		"/usr/libexec/fastlane-cron",
		"/etc/init.d/fastlane",
		"/etc/init.d/uhttpd",
		"/etc/init.d/rpcd",
		"/usr/bin/xray",
		"/etc/fastlane",
		"/etc/xray",
		"/tmp/luci-indexcache",
		"/tmp/luci-modulecache",
	}
	for _, path := range paths {
		postinst = strings.ReplaceAll(postinst, path, filepath.Join(root, strings.TrimPrefix(path, "/")))
	}
	return postinst
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
