package release_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGeoDataHelperUpdatesVerifiedPairAndRollsBackOnReloadFailure(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	helper := filepath.Join(root, "openwrt", "root", "usr", "libexec", "fastlane-geodata")
	assetDir := t.TempDir()
	binDir := t.TempDir()
	geoIP := "new-geoip"
	geoSite := "new-geosite category-ru"
	downloader := filepath.Join(binDir, "wget")
	downloaderScript := fmt.Sprintf(`#!/bin/sh
destination="$3"
url="$4"
case "$url" in
  */geoip.dat) printf '%%s' %q >"$destination" ;;
  */geosite.dat) printf '%%s' %q >"$destination" ;;
  */geoip.dat.sha256sum) printf '%%s  geoip.dat\n' %q >"$destination" ;;
  */geosite.dat.sha256sum) printf '%%s  geosite.dat\n' %q >"$destination" ;;
  *) exit 2 ;;
esac
`, geoIP, geoSite, fmt.Sprintf("%x", sha256.Sum256([]byte(geoIP))), fmt.Sprintf("%x", sha256.Sum256([]byte(geoSite))))
	writeFile(t, downloader, downloaderScript, 0o755)
	successService := filepath.Join(binDir, "xray-service-ok")
	writeFile(t, successService, "#!/bin/sh\ncase \"$1\" in\n  status) echo running; exit 0 ;;\n  running) exit 1 ;;\n  reload|restart) exit 0 ;;\n  *) exit 1 ;;\nesac\n", 0o755)

	runUpdate := func(service string) ([]byte, error) {
		cmd := exec.Command("sh", helper, "update")
		cmd.Env = append(os.Environ(),
			"FASTLANE_GEODATA_DIR="+assetDir,
			"FASTLANE_GEODATA_WORKDIR="+filepath.Join(t.TempDir(), "work"),
			"FASTLANE_GEODATA_WGET="+downloader,
			"FASTLANE_XRAY_BIN="+filepath.Join(binDir, "missing-xray"),
			"FASTLANE_XRAY_SERVICE="+service,
		)
		return cmd.CombinedOutput()
	}

	output, err := runUpdate(successService)
	if err != nil {
		t.Fatalf("successful geodata update: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), `"last_result":"ok"`) {
		t.Fatalf("expected successful status, got %s", output)
	}
	manifestPath := filepath.Join(assetDir, ".fastlane-geodata-manifest.sha256")
	manifest, readErr := os.ReadFile(manifestPath)
	if readErr != nil || string(manifest) != geoDataManifest(geoIP, geoSite) {
		t.Fatalf("unexpected installed manifest: %q, %v", manifest, readErr)
	}

	oldIP := "old-geoip"
	oldSite := "old-geosite category-ru"
	oldManifest := geoDataManifest(oldIP, oldSite)
	writeFile(t, filepath.Join(assetDir, "geoip.dat"), oldIP, 0o644)
	writeFile(t, filepath.Join(assetDir, "geosite.dat"), oldSite, 0o644)
	writeFile(t, manifestPath, oldManifest, 0o644)
	failingService := filepath.Join(binDir, "xray-service-fail")
	writeFile(t, failingService, "#!/bin/sh\n[ \"$1\" = running ] && exit 0\nexit 1\n", 0o755)
	output, err = runUpdate(failingService)
	if err == nil {
		t.Fatalf("expected reload failure, got %s", output)
	}
	gotIP, readErr := os.ReadFile(filepath.Join(assetDir, "geoip.dat"))
	if readErr != nil || string(gotIP) != oldIP {
		t.Fatalf("geoip rollback failed: %q, %v", gotIP, readErr)
	}
	gotSite, readErr := os.ReadFile(filepath.Join(assetDir, "geosite.dat"))
	if readErr != nil || string(gotSite) != oldSite {
		t.Fatalf("geosite rollback failed: %q, %v", gotSite, readErr)
	}
	gotManifest, readErr := os.ReadFile(manifestPath)
	if readErr != nil || string(gotManifest) != oldManifest {
		t.Fatalf("manifest rollback failed: %q, %v", gotManifest, readErr)
	}

	statusCmd := exec.Command("sh", helper, "status")
	statusCmd.Env = append(os.Environ(), "FASTLANE_GEODATA_DIR="+assetDir)
	statusOutput, statusErr := statusCmd.CombinedOutput()
	if statusErr != nil {
		t.Fatalf("status after rollback: %v\n%s", statusErr, statusOutput)
	}
	if !strings.Contains(string(statusOutput), `"ready":true`) || !strings.Contains(string(statusOutput), `"last_result":"error"`) {
		t.Fatalf("expected verified previous pair and failed update status, got %s", statusOutput)
	}
}

func TestGeoDataHelperInstallsWhileXrayIsStopped(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	helper := filepath.Join(root, "openwrt", "root", "usr", "libexec", "fastlane-geodata")
	assetDir := t.TempDir()
	binDir := t.TempDir()
	geoIP := "new-geoip"
	geoSite := "new-geosite category-ru"
	downloader := filepath.Join(binDir, "wget")
	downloaderScript := fmt.Sprintf(`#!/bin/sh
destination="$3"
url="$4"
case "$url" in
  */geoip.dat) printf '%%s' %q >"$destination" ;;
  */geosite.dat) printf '%%s' %q >"$destination" ;;
  */geoip.dat.sha256sum) printf '%%s  geoip.dat\n' %q >"$destination" ;;
  */geosite.dat.sha256sum) printf '%%s  geosite.dat\n' %q >"$destination" ;;
  *) exit 2 ;;
esac
`, geoIP, geoSite, fmt.Sprintf("%x", sha256.Sum256([]byte(geoIP))), fmt.Sprintf("%x", sha256.Sum256([]byte(geoSite))))
	writeFile(t, downloader, downloaderScript, 0o755)
	stoppedService := filepath.Join(binDir, "xray-service-stopped")
	unexpectedAction := filepath.Join(binDir, "unexpected-service-action")
	stoppedServiceScript := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n  status) echo 'not running'; exit 0 ;;\n  running) exit 0 ;;\n  reload|restart|start) : >%q; exit 0 ;;\n  *) exit 42 ;;\nesac\n", unexpectedAction)
	writeFile(t, stoppedService, stoppedServiceScript, 0o755)

	cmd := exec.Command("sh", helper, "update")
	cmd.Env = append(os.Environ(),
		"FASTLANE_GEODATA_DIR="+assetDir,
		"FASTLANE_GEODATA_WORKDIR="+filepath.Join(t.TempDir(), "work"),
		"FASTLANE_GEODATA_WGET="+downloader,
		"FASTLANE_XRAY_BIN="+filepath.Join(binDir, "missing-xray"),
		"FASTLANE_XRAY_SERVICE="+stoppedService,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install geodata with stopped Xray: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), `"ready":true`) {
		t.Fatalf("expected ready geodata, got %s", output)
	}
	manifest, readErr := os.ReadFile(filepath.Join(assetDir, ".fastlane-geodata-manifest.sha256"))
	if readErr != nil || string(manifest) != geoDataManifest(geoIP, geoSite) {
		t.Fatalf("expected installed checksum manifest: %q, %v", manifest, readErr)
	}
	if _, statErr := os.Stat(unexpectedAction); !os.IsNotExist(statErr) {
		t.Fatalf("stopped Xray must not be started or reloaded: %v", statErr)
	}
}

func TestGeoDataHelperStartDetachesAndReportsLifecycle(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	helper := filepath.Join(root, "openwrt", "root", "usr", "libexec", "fastlane-geodata")
	assetDir := t.TempDir()
	binDir := t.TempDir()
	workDir := filepath.Join(t.TempDir(), "work")
	lockDir := filepath.Join(t.TempDir(), "geodata.lock")
	startedFile := filepath.Join(t.TempDir(), "download-started")
	releaseFile := filepath.Join(t.TempDir(), "download-release")
	geoIP := "async-new-geoip"
	geoSite := "async-new-geosite category-ru"
	downloader := filepath.Join(binDir, "wget")
	downloaderScript := fmt.Sprintf(`#!/bin/sh
destination="$3"
url="$4"
case "$url" in
  */geoip.dat)
    : >%q
    while [ ! -e %q ]; do sleep 0.05; done
    printf '%%s' %q >"$destination"
    ;;
  */geosite.dat) printf '%%s' %q >"$destination" ;;
  */geoip.dat.sha256sum) printf '%%s  geoip.dat\n' %q >"$destination" ;;
  */geosite.dat.sha256sum) printf '%%s  geosite.dat\n' %q >"$destination" ;;
  *) exit 2 ;;
esac
`, startedFile, releaseFile, geoIP, geoSite, fmt.Sprintf("%x", sha256.Sum256([]byte(geoIP))), fmt.Sprintf("%x", sha256.Sum256([]byte(geoSite))))
	writeFile(t, downloader, downloaderScript, 0o755)

	command := func(action string) *exec.Cmd {
		cmd := exec.Command("sh", helper, action)
		cmd.Env = append(os.Environ(),
			"FASTLANE_GEODATA_DIR="+assetDir,
			"FASTLANE_GEODATA_WORKDIR="+workDir,
			"FASTLANE_GEODATA_LOCK_DIR="+lockDir,
			"FASTLANE_GEODATA_WGET="+downloader,
			"FASTLANE_XRAY_BIN="+filepath.Join(binDir, "missing-xray"),
			"FASTLANE_XRAY_SERVICE="+filepath.Join(binDir, "missing-service"),
		)
		return cmd
	}

	t.Cleanup(func() { _ = os.WriteFile(releaseFile, []byte("release"), 0o644) })
	startCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	startCmd := exec.CommandContext(startCtx, "sh", helper, "start")
	startCmd.WaitDelay = 250 * time.Millisecond
	startCmd.Env = command("start").Env
	startOutput, err := startCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("detached geodata start must return before download completes: %v\n%s", err, startOutput)
	}
	if startCtx.Err() != nil {
		t.Fatalf("detached geodata start hit RPC-sized timeout: %v", startCtx.Err())
	}
	if !strings.Contains(string(startOutput), `"updating":true`) || !strings.Contains(string(startOutput), `"last_result":"updating"`) {
		t.Fatalf("start must immediately report updating, got %s", startOutput)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(startedFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("detached worker never reached downloader")
		}
		time.Sleep(20 * time.Millisecond)
	}
	statusOutput, statusErr := command("status").CombinedOutput()
	if statusErr != nil || !strings.Contains(string(statusOutput), `"updating":true`) {
		t.Fatalf("status must expose live detached update: %v\n%s", statusErr, statusOutput)
	}

	if err := os.WriteFile(releaseFile, []byte("release"), 0o644); err != nil {
		t.Fatalf("release detached update: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		statusOutput, statusErr = command("status").CombinedOutput()
		_, lockErr := os.Stat(lockDir)
		if statusErr == nil && strings.Contains(string(statusOutput), `"updating":false`) &&
			strings.Contains(string(statusOutput), `"last_result":"ok"`) && strings.Contains(string(statusOutput), `"ready":true`) &&
			os.IsNotExist(lockErr) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("detached update did not finish and clean its lock: status=%v lock=%v\n%s", statusErr, lockErr, statusOutput)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(lockDir); !os.IsNotExist(err) {
		t.Fatalf("detached update lock was not released: %v", err)
	}
	workDirs, globErr := filepath.Glob(workDir + ".*")
	if globErr != nil || len(workDirs) != 0 {
		t.Fatalf("detached update workdir was not cleaned: %v, %v", workDirs, globErr)
	}
}

func TestGeoDataHelperStatusRejectsOrphanedUpdatingState(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	helper := filepath.Join(root, "openwrt", "root", "usr", "libexec", "fastlane-geodata")
	assetDir := t.TempDir()
	lockDir := filepath.Join(t.TempDir(), "geodata.lock")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("create orphaned lock: %v", err)
	}
	writeFile(t, filepath.Join(lockDir, "pid"), "2147483647\n", 0o644)
	writeFile(t, filepath.Join(assetDir, ".fastlane-geodata-status"), "updating|2026-09-04T14:04:04Z|GeoIP/GeoSite update in progress\n", 0o644)

	cmd := exec.Command("sh", helper, "status")
	cmd.Env = append(os.Environ(),
		"FASTLANE_GEODATA_DIR="+assetDir,
		"FASTLANE_GEODATA_LOCK_DIR="+lockDir,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("status with orphaned worker: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), `"updating":false`) || !strings.Contains(string(output), `"last_result":"error"`) ||
		!strings.Contains(string(output), "stopped unexpectedly") {
		t.Fatalf("orphaned worker must become a terminal error, got %s", output)
	}
}

func TestGeoDataHelperRejectsConcurrentUpdateAndReleasesLock(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	helper := filepath.Join(root, "openwrt", "root", "usr", "libexec", "fastlane-geodata")
	assetDir := t.TempDir()
	binDir := t.TempDir()
	workDir := filepath.Join(t.TempDir(), "work")
	lockDir := filepath.Join(t.TempDir(), "geodata.lock")
	startedFile := filepath.Join(t.TempDir(), "download-started")
	releaseFile := filepath.Join(t.TempDir(), "download-release")
	geoIP := "locked-new-geoip"
	geoSite := "locked-new-geosite category-ru"
	downloader := filepath.Join(binDir, "wget")
	downloaderScript := fmt.Sprintf(`#!/bin/sh
destination="$3"
url="$4"
case "$url" in
  */geoip.dat)
    : >%q
    while [ ! -e %q ]; do sleep 0.05; done
    printf '%%s' %q >"$destination"
    ;;
  */geosite.dat) printf '%%s' %q >"$destination" ;;
  */geoip.dat.sha256sum) printf '%%s  geoip.dat\n' %q >"$destination" ;;
  */geosite.dat.sha256sum) printf '%%s  geosite.dat\n' %q >"$destination" ;;
  *) exit 2 ;;
esac
`, startedFile, releaseFile, geoIP, geoSite, fmt.Sprintf("%x", sha256.Sum256([]byte(geoIP))), fmt.Sprintf("%x", sha256.Sum256([]byte(geoSite))))
	writeFile(t, downloader, downloaderScript, 0o755)

	command := func(ctx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "sh", helper, "update")
		cmd.Env = append(os.Environ(),
			"FASTLANE_GEODATA_DIR="+assetDir,
			"FASTLANE_GEODATA_WORKDIR="+workDir,
			"FASTLANE_GEODATA_LOCK_DIR="+lockDir,
			"FASTLANE_GEODATA_WGET="+downloader,
			"FASTLANE_XRAY_BIN="+filepath.Join(binDir, "missing-xray"),
			"FASTLANE_XRAY_SERVICE="+filepath.Join(binDir, "missing-service"),
		)
		return cmd
	}

	first := command(context.Background())
	var firstOutput bytes.Buffer
	first.Stdout = &firstOutput
	first.Stderr = &firstOutput
	if err := first.Start(); err != nil {
		t.Fatalf("start first geodata update: %v", err)
	}
	firstDone := make(chan error, 1)
	go func() { firstDone <- first.Wait() }()
	firstWaited := false
	t.Cleanup(func() {
		_ = os.WriteFile(releaseFile, []byte("release"), 0o644)
		if firstWaited {
			return
		}
		select {
		case <-firstDone:
		default:
			_ = first.Process.Kill()
			<-firstDone
		}
	})

	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(startedFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("first update never reached downloader; output: %s", firstOutput.String())
		}
		time.Sleep(20 * time.Millisecond)
	}

	busyCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	busyOutput, busyErr := command(busyCtx).CombinedOutput()
	if busyErr == nil {
		t.Fatalf("concurrent update unexpectedly succeeded: %s", busyOutput)
	}
	if busyCtx.Err() != nil {
		t.Fatalf("concurrent update blocked instead of failing fast: %v", busyCtx.Err())
	}
	if !strings.Contains(string(busyOutput), "уже выполняется") {
		t.Fatalf("expected busy lock error, got %s", busyOutput)
	}

	if err := os.WriteFile(releaseFile, []byte("release"), 0o644); err != nil {
		t.Fatalf("release first update: %v", err)
	}
	firstErr := <-firstDone
	firstWaited = true
	if firstErr != nil {
		t.Fatalf("first update failed: %v\n%s", firstErr, firstOutput.String())
	}
	if _, err := os.Stat(lockDir); !os.IsNotExist(err) {
		t.Fatalf("lock was not released after update: %v", err)
	}

	afterOutput, afterErr := command(context.Background()).CombinedOutput()
	if afterErr != nil {
		t.Fatalf("update after lock release failed: %v\n%s", afterErr, afterOutput)
	}
}

func TestGeoDataHelperRecoversDeadOwnerLockWithoutStealingUnknownLock(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	helper := filepath.Join(root, "openwrt", "root", "usr", "libexec", "fastlane-geodata")
	assetDir := t.TempDir()
	binDir := t.TempDir()
	workDir := filepath.Join(t.TempDir(), "work")
	lockDir := filepath.Join(t.TempDir(), "geodata.lock")
	geoIP := "recovered-lock-geoip"
	geoSite := "recovered-lock-geosite category-ru"
	downloader := filepath.Join(binDir, "wget")
	downloaderScript := fmt.Sprintf(`#!/bin/sh
destination="$3"
url="$4"
case "$url" in
  */geoip.dat) printf '%%s' %q >"$destination" ;;
  */geosite.dat) printf '%%s' %q >"$destination" ;;
  */geoip.dat.sha256sum) printf '%%s  geoip.dat\n' %q >"$destination" ;;
  */geosite.dat.sha256sum) printf '%%s  geosite.dat\n' %q >"$destination" ;;
  *) exit 2 ;;
esac
`, geoIP, geoSite, fmt.Sprintf("%x", sha256.Sum256([]byte(geoIP))), fmt.Sprintf("%x", sha256.Sum256([]byte(geoSite))))
	writeFile(t, downloader, downloaderScript, 0o755)

	command := func() *exec.Cmd {
		cmd := exec.Command("sh", helper, "update")
		cmd.Env = append(os.Environ(),
			"FASTLANE_GEODATA_DIR="+assetDir,
			"FASTLANE_GEODATA_WORKDIR="+workDir,
			"FASTLANE_GEODATA_LOCK_DIR="+lockDir,
			"FASTLANE_GEODATA_WGET="+downloader,
			"FASTLANE_XRAY_BIN="+filepath.Join(binDir, "missing-xray"),
			"FASTLANE_XRAY_SERVICE="+filepath.Join(binDir, "missing-service"),
		)
		return cmd
	}

	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("create stale lock: %v", err)
	}
	writeFile(t, filepath.Join(lockDir, "pid"), "2147483647\n", 0o644)
	staleWorkDir := workDir + ".2147483647"
	if err := os.MkdirAll(staleWorkDir, 0o755); err != nil {
		t.Fatalf("create stale workdir: %v", err)
	}
	writeFile(t, filepath.Join(staleWorkDir, "partial-download"), "partial", 0o644)
	output, err := command().CombinedOutput()
	if err != nil {
		t.Fatalf("recover dead-owner lock: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), `"ready":true`) {
		t.Fatalf("expected successful update after stale lock recovery, got %s", output)
	}
	if _, err := os.Stat(lockDir); !os.IsNotExist(err) {
		t.Fatalf("recovered lock was not released: %v", err)
	}
	if _, err := os.Stat(staleWorkDir); !os.IsNotExist(err) {
		t.Fatalf("stale worker directory was not removed: %v", err)
	}

	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("create unknown lock: %v", err)
	}
	writeFile(t, filepath.Join(lockDir, "pid"), "not-a-pid\n", 0o644)
	output, err = command().CombinedOutput()
	if err == nil {
		t.Fatalf("unknown lock owner must not be stolen: %s", output)
	}
	if !strings.Contains(string(output), "уже выполняется") {
		t.Fatalf("expected busy lock error, got %s", output)
	}
	if _, err := os.Stat(lockDir); err != nil {
		t.Fatalf("unknown lock must remain for manual inspection: %v", err)
	}
}

func TestGeoDataHelperRollsBackWhenReloadReturnsSuccessButXrayStops(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	helper := filepath.Join(root, "openwrt", "root", "usr", "libexec", "fastlane-geodata")
	assetDir := t.TempDir()
	binDir := t.TempDir()
	oldIP := "verified-old-ip"
	oldSite := "verified-old-site category-ru"
	newIP := "new-ip-that-stops-xray"
	newSite := "new-site-that-stops-xray category-ru"
	manifestPath := filepath.Join(assetDir, ".fastlane-geodata-manifest.sha256")
	writeFile(t, filepath.Join(assetDir, "geoip.dat"), oldIP, 0o644)
	writeFile(t, filepath.Join(assetDir, "geosite.dat"), oldSite, 0o644)
	writeFile(t, manifestPath, geoDataManifest(oldIP, oldSite), 0o644)

	downloader := filepath.Join(binDir, "wget")
	downloaderScript := fmt.Sprintf(`#!/bin/sh
destination="$3"
url="$4"
case "$url" in
  */geoip.dat) printf '%%s' %q >"$destination" ;;
  */geosite.dat) printf '%%s' %q >"$destination" ;;
  */geoip.dat.sha256sum) printf '%%s  geoip.dat\n' %q >"$destination" ;;
  */geosite.dat.sha256sum) printf '%%s  geosite.dat\n' %q >"$destination" ;;
  *) exit 2 ;;
esac
`, newIP, newSite, fmt.Sprintf("%x", sha256.Sum256([]byte(newIP))), fmt.Sprintf("%x", sha256.Sum256([]byte(newSite))))
	writeFile(t, downloader, downloaderScript, 0o755)

	stateFile := filepath.Join(binDir, "xray-state")
	serviceLog := filepath.Join(binDir, "xray-service.log")
	writeFile(t, stateFile, "running\n", 0o644)
	service := filepath.Join(binDir, "xray-service")
	serviceScript := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$1" >>%q
case "$1" in
  status)
    state="$(cat %q)"
    [ "$state" = running ] && echo running || echo stopped
    exit 0
    ;;
  running) [ "$(cat %q)" = running ] ;;
  reload)
    printf 'stopped\n' >%q
    exit 0
    ;;
  restart|start)
    if [ "$(cat %q/geoip.dat)" = %q ]; then
      printf 'running\n' >%q
    else
      printf 'stopped\n' >%q
    fi
    exit 0
    ;;
  *) exit 1 ;;
esac
`, serviceLog, stateFile, stateFile, stateFile, assetDir, oldIP, stateFile, stateFile)
	writeFile(t, service, serviceScript, 0o755)
	lockDir := filepath.Join(t.TempDir(), "geodata.lock")

	cmd := exec.Command("sh", helper, "update")
	cmd.Env = append(os.Environ(),
		"FASTLANE_GEODATA_DIR="+assetDir,
		"FASTLANE_GEODATA_WORKDIR="+filepath.Join(t.TempDir(), "work"),
		"FASTLANE_GEODATA_LOCK_DIR="+lockDir,
		"FASTLANE_GEODATA_WGET="+downloader,
		"FASTLANE_XRAY_BIN="+filepath.Join(binDir, "missing-xray"),
		"FASTLANE_XRAY_SERVICE="+service,
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected post-reload running check to fail: %s", output)
	}
	if !strings.Contains(string(output), "Xray не запущен после reload") {
		t.Fatalf("expected post-reload error, got %s", output)
	}

	gotIP, readErr := os.ReadFile(filepath.Join(assetDir, "geoip.dat"))
	if readErr != nil || string(gotIP) != oldIP {
		t.Fatalf("geoip rollback failed: %q, %v", gotIP, readErr)
	}
	gotSite, readErr := os.ReadFile(filepath.Join(assetDir, "geosite.dat"))
	if readErr != nil || string(gotSite) != oldSite {
		t.Fatalf("geosite rollback failed: %q, %v", gotSite, readErr)
	}
	gotManifest, readErr := os.ReadFile(manifestPath)
	if readErr != nil || string(gotManifest) != geoDataManifest(oldIP, oldSite) {
		t.Fatalf("manifest rollback failed: %q, %v", gotManifest, readErr)
	}
	state, readErr := os.ReadFile(stateFile)
	if readErr != nil || strings.TrimSpace(string(state)) != "running" {
		t.Fatalf("Xray service was not recovered: %q, %v", state, readErr)
	}
	logOutput, readErr := os.ReadFile(serviceLog)
	if readErr != nil || !strings.Contains(string(logOutput), "reload\n") || !strings.Contains(string(logOutput), "restart\n") {
		t.Fatalf("expected reload followed by recovery restart, got %q, %v", logOutput, readErr)
	}
	if _, err := os.Stat(lockDir); !os.IsNotExist(err) {
		t.Fatalf("lock was not released after rollback: %v", err)
	}
}

func TestGeoDataHelperStatusRequiresVerifiedManifest(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	helper := filepath.Join(root, "openwrt", "root", "usr", "libexec", "fastlane-geodata")
	assetDir := t.TempDir()
	status := func() string {
		t.Helper()
		cmd := exec.Command("sh", helper, "status")
		cmd.Env = append(os.Environ(), "FASTLANE_GEODATA_DIR="+assetDir)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("geodata status: %v\n%s", err, output)
		}
		return string(output)
	}

	if output := status(); !strings.Contains(output, `"ready":false`) {
		t.Fatalf("expected missing assets status, got %s", output)
	}

	geoIP := "arbitrary-non-empty-ip"
	geoSite := "arbitrary-non-empty-site"
	writeFile(t, filepath.Join(assetDir, "geoip.dat"), geoIP, 0o644)
	writeFile(t, filepath.Join(assetDir, "geosite.dat"), geoSite, 0o644)
	if output := status(); !strings.Contains(output, `"ready":false`) {
		t.Fatalf("unverified non-empty assets must not be ready, got %s", output)
	}

	manifestPath := filepath.Join(assetDir, ".fastlane-geodata-manifest.sha256")
	writeFile(t, manifestPath, geoDataManifest(geoIP, geoSite)+strings.Repeat("0", 64)+"  extra.dat\n", 0o644)
	if output := status(); !strings.Contains(output, `"ready":false`) {
		t.Fatalf("manifest with unexpected entries must not be ready, got %s", output)
	}

	writeFile(t, manifestPath, geoDataManifest(geoIP, geoSite), 0o644)
	if output := status(); !strings.Contains(output, `"ready":true`) {
		t.Fatalf("expected checksum-verified assets status, got %s", output)
	}

	writeFile(t, filepath.Join(assetDir, "geosite.dat"), geoSite+"-tampered", 0o644)
	if output := status(); !strings.Contains(output, `"ready":false`) {
		t.Fatalf("tampered asset must not be ready, got %s", output)
	}
}

func TestGeoDataHelperRejectsBadChecksumWithoutReplacingVerifiedPair(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	helper := filepath.Join(root, "openwrt", "root", "usr", "libexec", "fastlane-geodata")
	assetDir := t.TempDir()
	binDir := t.TempDir()
	oldIP := "verified-old-ip"
	oldSite := "verified-old-site category-ru"
	manifestPath := filepath.Join(assetDir, ".fastlane-geodata-manifest.sha256")
	writeFile(t, filepath.Join(assetDir, "geoip.dat"), oldIP, 0o644)
	writeFile(t, filepath.Join(assetDir, "geosite.dat"), oldSite, 0o644)
	writeFile(t, manifestPath, geoDataManifest(oldIP, oldSite), 0o644)

	newIP := "untrusted-new-ip"
	newSite := "untrusted-new-site category-ru"
	downloader := filepath.Join(binDir, "wget")
	downloaderScript := fmt.Sprintf(`#!/bin/sh
destination="$3"
url="$4"
case "$url" in
  */geoip.dat) printf '%%s' %q >"$destination" ;;
  */geosite.dat) printf '%%s' %q >"$destination" ;;
  */geoip.dat.sha256sum) printf '%%064d  geoip.dat\n' 0 >"$destination" ;;
  */geosite.dat.sha256sum) printf '%%s  geosite.dat\n' %q >"$destination" ;;
  *) exit 2 ;;
esac
`, newIP, newSite, fmt.Sprintf("%x", sha256.Sum256([]byte(newSite))))
	writeFile(t, downloader, downloaderScript, 0o755)

	cmd := exec.Command("sh", helper, "update")
	cmd.Env = append(os.Environ(),
		"FASTLANE_GEODATA_DIR="+assetDir,
		"FASTLANE_GEODATA_WORKDIR="+filepath.Join(t.TempDir(), "work"),
		"FASTLANE_GEODATA_WGET="+downloader,
		"FASTLANE_XRAY_BIN="+filepath.Join(binDir, "missing-xray"),
		"FASTLANE_XRAY_SERVICE="+filepath.Join(binDir, "missing-service"),
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected checksum failure, got %s", output)
	}
	if !strings.Contains(string(output), "Контрольная сумма geoip.dat не совпала") {
		t.Fatalf("expected checksum error, got %s", output)
	}

	gotIP, readErr := os.ReadFile(filepath.Join(assetDir, "geoip.dat"))
	if readErr != nil || string(gotIP) != oldIP {
		t.Fatalf("verified geoip changed after rejected update: %q, %v", gotIP, readErr)
	}
	gotSite, readErr := os.ReadFile(filepath.Join(assetDir, "geosite.dat"))
	if readErr != nil || string(gotSite) != oldSite {
		t.Fatalf("verified geosite changed after rejected update: %q, %v", gotSite, readErr)
	}
	gotManifest, readErr := os.ReadFile(manifestPath)
	if readErr != nil || string(gotManifest) != geoDataManifest(oldIP, oldSite) {
		t.Fatalf("verified manifest changed after rejected update: %q, %v", gotManifest, readErr)
	}
}

func geoDataManifest(geoIP, geoSite string) string {
	return fmt.Sprintf("%x  geoip.dat\n%x  geosite.dat\n", sha256.Sum256([]byte(geoIP)), sha256.Sum256([]byte(geoSite)))
}
