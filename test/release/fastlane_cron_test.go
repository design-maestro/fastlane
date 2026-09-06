package release_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFastLaneCronHelperEnsuresHourlyXrayRetention(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	serviceLogPath := filepath.Join(t.TempDir(), "services.log")
	scriptPath := filepath.Join(repoRoot(t), "openwrt", "root", "usr", "libexec", "fastlane-cron")

	writeFile(t, filepath.Join(installRoot, "etc", "crontabs", "root"), "15 4 * * * echo keep\n", 0o644)
	writeServiceStub(t, filepath.Join(installRoot, "etc", "init.d", "cron"))

	runFastLaneCronHelper(t, scriptPath, installRoot, serviceLogPath, "ensure-xray-log-retention")
	runFastLaneCronHelper(t, scriptPath, installRoot, serviceLogPath, "ensure-xray-log-retention")

	contents, err := os.ReadFile(filepath.Join(installRoot, "etc", "crontabs", "root"))
	if err != nil {
		t.Fatalf("read crontab: %v", err)
	}

	text := string(contents)
	if !strings.Contains(text, "15 4 * * * echo keep") {
		t.Fatalf("expected existing cron entry to be preserved, got %q", text)
	}
	if strings.Count(text, "# fastlane:xray-log-retention:start") != 1 {
		t.Fatalf("expected one start marker, got %q", text)
	}
	if strings.Count(text, "0 * * * * [ -f /var/log/xray.log ] && : > /var/log/xray.log") != 1 {
		t.Fatalf("expected one hourly retention job, got %q", text)
	}
	if strings.Count(text, "17 3 * * * [ -d /etc/fastlane ] && find /etc/fastlane -maxdepth 1 -type f -name '*.corrupt-*' -mtime +7 -exec rm -f {} \\;") != 1 {
		t.Fatalf("expected one daily corrupt-backup cleanup job, got %q", text)
	}

	serviceLog, err := os.ReadFile(serviceLogPath)
	if err != nil {
		t.Fatalf("read service log: %v", err)
	}
	if strings.Count(string(serviceLog), "cron:restart") != 1 {
		t.Fatalf("expected cron restart exactly once, got %q", string(serviceLog))
	}
}

func TestFastLaneCronHelperRemovesManagedBlockOnly(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	serviceLogPath := filepath.Join(t.TempDir(), "services.log")
	scriptPath := filepath.Join(repoRoot(t), "openwrt", "root", "usr", "libexec", "fastlane-cron")

	writeFile(t, filepath.Join(installRoot, "etc", "crontabs", "root"), strings.Join([]string{
		"15 4 * * * echo keep",
		"# fastlane:xray-log-retention:start",
		"0 * * * * [ -f /var/log/xray.log ] && : > /var/log/xray.log",
		"17 3 * * * [ -d /etc/fastlane ] && find /etc/fastlane -maxdepth 1 -type f -name '*.corrupt-*' -mtime +7 -exec rm -f {} \\;",
		"# fastlane:xray-log-retention:end",
		"",
	}, "\n"), 0o644)
	writeServiceStub(t, filepath.Join(installRoot, "etc", "init.d", "cron"))

	runFastLaneCronHelper(t, scriptPath, installRoot, serviceLogPath, "remove-xray-log-retention")

	contents, err := os.ReadFile(filepath.Join(installRoot, "etc", "crontabs", "root"))
	if err != nil {
		t.Fatalf("read crontab: %v", err)
	}

	text := string(contents)
	if strings.Contains(text, "fastlane:xray-log-retention") {
		t.Fatalf("expected managed block to be removed, got %q", text)
	}
	if !strings.Contains(text, "15 4 * * * echo keep") {
		t.Fatalf("expected unrelated cron entry to remain, got %q", text)
	}

	serviceLog, err := os.ReadFile(serviceLogPath)
	if err != nil {
		t.Fatalf("read service log: %v", err)
	}
	if strings.Count(string(serviceLog), "cron:restart") != 1 {
		t.Fatalf("expected cron restart exactly once, got %q", string(serviceLog))
	}
}

func TestFastLaneCronHelperUpdatesManagedBlockWhenJobsChange(t *testing.T) {
	t.Parallel()

	installRoot := t.TempDir()
	serviceLogPath := filepath.Join(t.TempDir(), "services.log")
	scriptPath := filepath.Join(repoRoot(t), "openwrt", "root", "usr", "libexec", "fastlane-cron")

	writeFile(t, filepath.Join(installRoot, "etc", "crontabs", "root"), strings.Join([]string{
		"15 4 * * * echo keep",
		"# fastlane:xray-log-retention:start",
		"0 * * * * [ -f /var/log/xray.log ] && : > /var/log/xray.log",
		"# fastlane:xray-log-retention:end",
		"",
	}, "\n"), 0o644)
	writeServiceStub(t, filepath.Join(installRoot, "etc", "init.d", "cron"))

	runFastLaneCronHelper(t, scriptPath, installRoot, serviceLogPath, "ensure-xray-log-retention")

	contents, err := os.ReadFile(filepath.Join(installRoot, "etc", "crontabs", "root"))
	if err != nil {
		t.Fatalf("read crontab: %v", err)
	}

	text := string(contents)
	if !strings.Contains(text, "17 3 * * * [ -d /etc/fastlane ] && find /etc/fastlane -maxdepth 1 -type f -name '*.corrupt-*' -mtime +7 -exec rm -f {} \\;") {
		t.Fatalf("expected managed block to be updated with cleanup job, got %q", text)
	}
}

func runFastLaneCronHelper(t *testing.T, scriptPath, installRoot, serviceLogPath, action string) {
	t.Helper()

	cmd := exec.Command("sh", scriptPath, action)
	cmd.Env = append(os.Environ(),
		"FASTLANE_INSTALL_ROOT="+installRoot,
		"FASTLANE_TEST_SERVICE_LOG="+serviceLogPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run fastlane cron helper: %v\n%s", err, output)
	}
}
