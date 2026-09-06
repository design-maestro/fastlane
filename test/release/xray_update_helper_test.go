package release_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestXrayUpdateHelperSkipsInstallWhenAlreadyLatest(t *testing.T) {
	t.Parallel()

	repoRoot := repoRoot(t)
	helperPath := filepath.Join(repoRoot, "openwrt", "root", "usr", "libexec", "fastlane-xray-update")
	helperSource, err := os.ReadFile(helperPath)
	if err != nil {
		t.Fatalf("read helper: %v", err)
	}

	workDir := t.TempDir()
	helperCopy := filepath.Join(workDir, "fastlane-xray-update")
	writeExecutable(t, helperCopy, string(helperSource))

	xrayStub := filepath.Join(workDir, "xray")
	writeExecutable(t, xrayStub, "#!/bin/sh\nset -eu\nprintf 'Xray 26.3.27 (Xray, Penetrates Everything.)\\n'\n")
	manifest := filepath.Join(workDir, "install-manifest.txt")
	writeFile(t, manifest, "runtime=xray\n", 0o600)

	wgetStub := filepath.Join(workDir, "wget")
	writeExecutable(t, wgetStub, "#!/bin/sh\nset -eu\nif [ \"$1\" = \"-qO-\" ]; then\n\t[ \"$2\" = \"https://example.invalid/releases/latest\" ] || { echo \"unexpected url: $2\" >&2; exit 1; }\n\tprintf '{\"tag_name\":\"v26.3.27\"}\\n'\n\texit 0\nfi\necho \"unexpected download\" >&2\nexit 1\n")

	stdout, stderr, err := runXrayUpdateHelper(t, helperCopy, map[string]string{
		"FASTLANE_XRAY_BINARY":           xrayStub,
		"FASTLANE_XRAY_WGET":             wgetStub,
		"FASTLANE_XRAY_RELEASES_API":     "https://example.invalid/releases/latest",
		"FASTLANE_XRAY_INSTALL_MANIFEST": manifest,
	})
	if err != nil {
		t.Fatalf("run xray update helper: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	if !strings.Contains(stdout, "FASTLANE_XRAY_UPDATE_STATUS=up-to-date") {
		t.Fatalf("expected up-to-date status, got stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Xray is up to date (26.3.27).") {
		t.Fatalf("expected up-to-date message, got stdout:\n%s", stdout)
	}
}

func TestXrayUpdateHelperPreservesExternalRuntime(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	source, err := os.ReadFile(filepath.Join(root, "openwrt", "root", "usr", "libexec", "fastlane-xray-update"))
	if err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	helper := filepath.Join(workDir, "fastlane-xray-update")
	writeExecutable(t, helper, string(source))
	xray := filepath.Join(workDir, "xray")
	writeExecutable(t, xray, "#!/bin/sh\nprintf 'Xray 26.2.6 (external)\\n'\n")
	wget := filepath.Join(workDir, "wget")
	writeExecutable(t, wget, "#!/bin/sh\nprintf '{\"tag_name\":\"v26.3.27\"}\\n'\n")
	stdout, stderr, err := runXrayUpdateHelper(t, helper, map[string]string{
		"FASTLANE_XRAY_BINARY": xray, "FASTLANE_XRAY_WGET": wget,
		"FASTLANE_XRAY_RELEASES_API":     "https://example.invalid/releases/latest",
		"FASTLANE_XRAY_INSTALL_MANIFEST": filepath.Join(workDir, "missing-manifest"),
	})
	if err != nil {
		t.Fatalf("external runtime check: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "FASTLANE_XRAY_UPDATE_STATUS=external") {
		t.Fatalf("expected external status, got %q", stdout)
	}
	data, err := os.ReadFile(xray)
	if err != nil || !strings.Contains(string(data), "external") {
		t.Fatalf("external Xray changed: %q, %v", data, err)
	}
}

func TestXrayUpdateHelperReturnsUnsupportedStatusForSoftFloatMips(t *testing.T) {
	t.Parallel()

	repoRoot := repoRoot(t)
	helperPath := filepath.Join(repoRoot, "openwrt", "root", "usr", "libexec", "fastlane-xray-update")
	helperSource, err := os.ReadFile(helperPath)
	if err != nil {
		t.Fatalf("read helper: %v", err)
	}

	workDir := t.TempDir()
	helperCopy := filepath.Join(workDir, "fastlane-xray-update")
	writeExecutable(t, helperCopy, string(helperSource))

	xrayStub := filepath.Join(workDir, "xray")
	writeExecutable(t, xrayStub, "#!/bin/sh\nset -eu\nprintf 'Xray 26.2.6 (Xray, Penetrates Everything.)\\n'\n")
	manifest := filepath.Join(workDir, "install-manifest.txt")
	writeFile(t, manifest, "runtime=xray\n", 0o600)

	wgetStub := filepath.Join(workDir, "wget")
	writeExecutable(t, wgetStub, "#!/bin/sh\nset -eu\nif [ \"$1\" = \"-qO-\" ]; then\n\t[ \"$2\" = \"https://example.invalid/releases/latest\" ] || { echo \"unexpected url: $2\" >&2; exit 1; }\n\tprintf '{\"tag_name\":\"v26.3.27\"}\\n'\n\texit 0\nfi\necho \"unexpected download\" >&2\nexit 1\n")

	stdout, stderr, err := runXrayUpdateHelper(t, helperCopy, map[string]string{
		"FASTLANE_XRAY_BINARY":           xrayStub,
		"FASTLANE_XRAY_WGET":             wgetStub,
		"FASTLANE_XRAY_RELEASES_API":     "https://example.invalid/releases/latest",
		"FASTLANE_XRAY_ARCH_OVERRIDE":    "mipsel_24kc",
		"FASTLANE_XRAY_INSTALL_MANIFEST": manifest,
	})
	if err != nil {
		t.Fatalf("expected helper to return unsupported status, got err: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	output := stdout + stderr
	if !strings.Contains(output, "FASTLANE_XRAY_UPDATE_STATUS=unsupported") {
		t.Fatalf("expected unsupported status, got:\n%s", output)
	}
	if !strings.Contains(output, "Official Xray releases do not publish a soft-float MIPS build.") {
		t.Fatalf("expected unsupported arch message, got:\n%s", output)
	}
}

func TestXrayUpdateHelperInstallsSupportedOfficialAsset(t *testing.T) {
	t.Parallel()

	repoRoot := repoRoot(t)
	helperPath := filepath.Join(repoRoot, "openwrt", "root", "usr", "libexec", "fastlane-xray-update")
	helperSource, err := os.ReadFile(helperPath)
	if err != nil {
		t.Fatalf("read helper: %v", err)
	}

	workDir := t.TempDir()
	helperCopy := filepath.Join(workDir, "fastlane-xray-update")
	writeExecutable(t, helperCopy, string(helperSource))

	xrayTarget := filepath.Join(workDir, "bin", "xray")
	writeExecutable(t, xrayTarget, "#!/bin/sh\nset -eu\nprintf 'Xray 26.2.6 (Xray, Penetrates Everything.)\\n'\n")
	manifest := filepath.Join(workDir, "install-manifest.txt")
	writeFile(t, manifest, "runtime=xray\n", 0o600)

	serviceLog := filepath.Join(workDir, "service.log")
	serviceStub := filepath.Join(workDir, "xray-service")
	writeExecutable(t, serviceStub, "#!/bin/sh\nset -eu\nprintf '%s\\n' \"${1:-}\" >> \"${FASTLANE_TEST_SERVICE_LOG:?}\"\n")

	wgetStub := filepath.Join(workDir, "wget")
	writeExecutable(t, wgetStub, "#!/bin/sh\nset -eu\nif [ \"$1\" = \"-qO-\" ]; then\n\t[ \"$2\" = \"https://example.invalid/releases/latest\" ] || { echo \"unexpected url: $2\" >&2; exit 1; }\n\tprintf '{\"tag_name\":\"v26.3.27\"}\\n'\n\texit 0\nfi\nout=\"$2\"\nurl=\"$3\"\ncase \"$url\" in\n\thttps://example.invalid/download/v26.3.27/Xray-linux-64.zip) printf 'fake zip' > \"$out\" ;;\n\thttps://example.invalid/download/v26.3.27/Xray-linux-64.zip.dgst) printf 'SHA2-256(xray.zip)= 9a53aa7be7f38b1807449c3607863ece9be4ff9305b68875684185185cb07a4b\\n' > \"$out\" ;;\n\t*) echo \"unexpected asset url: $url\" >&2; exit 1 ;;\nesac\n")

	unzipStub := filepath.Join(workDir, "unzip")
	writeExecutable(t, unzipStub, "#!/bin/sh\nset -eu\n[ \"$1\" = \"-p\" ] || { echo \"unexpected unzip arg: $1\" >&2; exit 1; }\ncat <<'EOS'\n#!/bin/sh\nset -eu\nprintf 'Xray 26.3.27 (Xray, Penetrates Everything.)\\n'\nEOS\n")

	stdout, stderr, err := runXrayUpdateHelper(t, helperCopy, map[string]string{
		"FASTLANE_XRAY_BINARY":           xrayTarget,
		"FASTLANE_XRAY_SERVICE":          serviceStub,
		"FASTLANE_XRAY_WGET":             wgetStub,
		"FASTLANE_XRAY_UNZIP":            unzipStub,
		"FASTLANE_XRAY_RELEASES_API":     "https://example.invalid/releases/latest",
		"FASTLANE_XRAY_RELEASE_BASE_URL": "https://example.invalid/download",
		"FASTLANE_XRAY_ARCH_OVERRIDE":    "x86_64",
		"FASTLANE_XRAY_WORKDIR":          filepath.Join(workDir, "tmp"),
		"FASTLANE_TEST_SERVICE_LOG":      serviceLog,
		"FASTLANE_XRAY_INSTALL_MANIFEST": manifest,
	})
	if err != nil {
		t.Fatalf("run xray update helper: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	if !strings.Contains(stdout, "FASTLANE_XRAY_UPDATE_STATUS=updated") {
		t.Fatalf("expected updated status, got stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Xray updated from 26.2.6 to 26.3.27.") {
		t.Fatalf("expected update message, got stdout:\n%s", stdout)
	}

	data, err := os.ReadFile(serviceLog)
	if err != nil {
		t.Fatalf("read service log: %v", err)
	}
	if !strings.Contains(string(data), "restart") {
		t.Fatalf("expected service restart, got %q", string(data))
	}

	output, err := exec.Command(xrayTarget, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("run installed xray: %v\n%s", err, string(output))
	}
	if !strings.Contains(string(output), "26.3.27") {
		t.Fatalf("expected installed xray version, got %q", string(output))
	}
}

func runXrayUpdateHelper(t *testing.T, helperPath string, env map[string]string) (string, string, error) {
	t.Helper()

	cmd := exec.Command("sh", helperPath)
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	output, err := cmd.CombinedOutput()
	return string(output), "", err
}
