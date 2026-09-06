package release_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelfUpdateHelperSkipsInstallWhenAlreadyLatest(t *testing.T) {
	t.Parallel()

	repoRoot := repoRoot(t)
	helperPath := filepath.Join(repoRoot, "openwrt", "root", "usr", "libexec", "fastlane-self-update")
	helperSource, err := os.ReadFile(helperPath)
	if err != nil {
		t.Fatalf("read helper: %v", err)
	}

	workDir := t.TempDir()
	helperCopy := filepath.Join(workDir, "fastlane-self-update")
	writeExecutable(t, helperCopy, string(helperSource))

	statePath := filepath.Join(workDir, "fastlane-version-state")
	writeFile(t, statePath, "0.1.6 959bc2f\n", 0o644)

	fastlaneStub := filepath.Join(workDir, "fastlane")
	writeExecutable(t, fastlaneStub, "#!/bin/sh\nset -eu\nset -- $(cat \"${FASTLANE_TEST_STATE:?}\")\nif [ \"${1:-}\" = \"--upgrade\" ]; then\n\techo \"unexpected upgrade\" >&2\n\texit 1\nfi\nprintf '{\"version\":\"%s\",\"commit\":\"%s\",\"build_date\":\"2026-04-06T08:08:29Z\"}\\n' \"$1\" \"$2\"\n")

	wgetStub := filepath.Join(workDir, "wget")
	writeExecutable(t, wgetStub, "#!/bin/sh\nset -eu\nif [ \"$1\" = \"-qO-\" ]; then\n\turl=\"$2\"\n\tcase \"$url\" in\n\t\thttps://example.invalid/releases/latest)\n\t\t\tprintf '{\"tag_name\":\"v0.1.6\"}\\n'\n\t\t\t;;\n\t\t*)\n\t\t\techo \"unexpected url: $url\" >&2\n\t\t\texit 1\n\t\t\t;;\n\tesac\n\texit 0\nfi\necho \"unexpected wget invocation\" >&2\nexit 1\n")

	stdout, stderr, err := runSelfUpdateHelper(t, helperCopy, map[string]string{
		"FASTLANE_BINARY":       fastlaneStub,
		"FASTLANE_WGET":         wgetStub,
		"FASTLANE_TEST_STATE":   statePath,
		"FASTLANE_RELEASES_API": "https://example.invalid/releases/latest",
	})
	if err != nil {
		t.Fatalf("run self-update helper: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	if !strings.Contains(stdout, "FASTLANE_SELF_UPDATE_STATUS=up-to-date") {
		t.Fatalf("expected up-to-date status, got stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Fast Lane is already up to date (0.1.6, 959bc2f).") {
		t.Fatalf("expected up-to-date message, got stdout:\n%s", stdout)
	}
}

func TestSelfUpdateHelperRunsCLIUpgradeWhenVersionDiffers(t *testing.T) {
	t.Parallel()

	repoRoot := repoRoot(t)
	helperPath := filepath.Join(repoRoot, "openwrt", "root", "usr", "libexec", "fastlane-self-update")
	helperSource, err := os.ReadFile(helperPath)
	if err != nil {
		t.Fatalf("read helper: %v", err)
	}

	workDir := t.TempDir()
	helperCopy := filepath.Join(workDir, "fastlane-self-update")
	writeExecutable(t, helperCopy, string(helperSource))

	statePath := filepath.Join(workDir, "fastlane-version-state")
	writeFile(t, statePath, "0.1.6 463158b\n", 0o644)
	installLog := filepath.Join(workDir, "install.log")

	fastlaneStub := filepath.Join(workDir, "fastlane")
	writeExecutable(t, fastlaneStub, "#!/bin/sh\nset -eu\nif [ \"${1:-}\" = \"--upgrade\" ]; then\n\tprintf 'cli upgrade invoked\\n' >> \"${FASTLANE_TEST_INSTALL_LOG:?}\"\n\tprintf '0.1.7 abcd123\\n' > \"${FASTLANE_TEST_STATE:?}\"\n\texit 0\nfi\nset -- $(cat \"${FASTLANE_TEST_STATE:?}\")\nprintf '{\"version\":\"%s\",\"commit\":\"%s\",\"build_date\":\"2026-04-06T08:08:29Z\"}\\n' \"$1\" \"$2\"\n")

	wgetStub := filepath.Join(workDir, "wget")
	writeExecutable(t, wgetStub, "#!/bin/sh\nset -eu\nif [ \"$1\" = \"-qO-\" ]; then\n\turl=\"$2\"\n\tcase \"$url\" in\n\t\thttps://example.invalid/releases/latest)\n\t\t\tprintf '{\"tag_name\":\"v0.1.7\"}\\n'\n\t\t\t;;\n\t\t*)\n\t\t\techo \"unexpected url: $url\" >&2\n\t\t\texit 1\n\t\t\t;;\n\tesac\n\texit 0\nfi\necho \"unexpected wget invocation\" >&2\nexit 1\n")

	stdout, stderr, err := runSelfUpdateHelper(t, helperCopy, map[string]string{
		"FASTLANE_BINARY":           fastlaneStub,
		"FASTLANE_WGET":             wgetStub,
		"FASTLANE_TEST_STATE":       statePath,
		"FASTLANE_TEST_INSTALL_LOG": installLog,
		"FASTLANE_RELEASES_API":     "https://example.invalid/releases/latest",
	})
	if err != nil {
		t.Fatalf("run self-update helper: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	if !strings.Contains(stdout, "FASTLANE_SELF_UPDATE_STATUS=updated") {
		t.Fatalf("expected updated status, got stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Fast Lane updated from 0.1.6 (463158b) to 0.1.7 (abcd123).") {
		t.Fatalf("expected update message, got stdout:\n%s", stdout)
	}
	if data, err := os.ReadFile(installLog); err != nil {
		t.Fatalf("read install log: %v", err)
	} else if !strings.Contains(string(data), "cli upgrade invoked") {
		t.Fatalf("expected cli upgrade to run, got %q", string(data))
	}
	stateData, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if strings.TrimSpace(string(stateData)) != "0.1.7 abcd123" {
		t.Fatalf("expected updated state, got %q", string(stateData))
	}
}

func TestSelfUpdateHelperRejectsDowngrade(t *testing.T) {
	t.Parallel()
	repoRoot := repoRoot(t)
	helperPath := filepath.Join(repoRoot, "openwrt", "root", "usr", "libexec", "fastlane-self-update")
	workDir := t.TempDir()
	helperCopy := filepath.Join(workDir, "fastlane-self-update")
	source, err := os.ReadFile(helperPath)
	if err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, helperCopy, string(source))
	fastlaneStub := filepath.Join(workDir, "fastlane")
	writeExecutable(t, fastlaneStub, "#!/bin/sh\nset -eu\n[ \"${1:-}\" != \"--upgrade\" ] || exit 99\nprintf '{\"version\":\"1.3.0\",\"commit\":\"newer\"}\\n'\n")
	wgetStub := filepath.Join(workDir, "wget")
	writeExecutable(t, wgetStub, "#!/bin/sh\nprintf '{\"tag_name\":\"v1.2.3\"}\\n'\n")
	stdout, stderr, err := runSelfUpdateHelper(t, helperCopy, map[string]string{
		"FASTLANE_BINARY": fastlaneStub, "FASTLANE_WGET": wgetStub,
		"FASTLANE_RELEASES_API": "https://example.invalid/releases/latest",
	})
	if err != nil {
		t.Fatalf("downgrade guard: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "FASTLANE_SELF_UPDATE_STATUS=downgrade-blocked") || !strings.Contains(stdout, "Refusing to downgrade") {
		t.Fatalf("expected downgrade-blocked status, got %q", stdout)
	}
}

func runSelfUpdateHelper(t *testing.T, helperPath string, env map[string]string) (string, string, error) {
	t.Helper()

	cmd := exec.Command("sh", helperPath)
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	output, err := cmd.CombinedOutput()
	return string(output), "", err
}
