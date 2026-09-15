package openwrt_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"debug/elf"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// Run this test with its explicit opt-in:
//
// FASTLANE_RUN_OPENWRT_ARCHIVE_UPGRADE=1 \
// FASTLANE_OPENWRT_UPGRADE_ASSET_DIR=/absolute/path/to/candidate-assets \
// FASTLANE_OPENWRT_UPGRADE_VERSION=0.1.25 \
//
//	go test -v -count=1 -timeout=40m ./test/integration/openwrt \
//	  -run '^TestOpenWrtPublishedArchiveUpgrade$'
//
// Generate real fastlane_<version>_x86_64.tar.gz and xray_<version>_x86_64.tar.gz
// with .sha256 sidecars before running. The stable Xray is deliberately reused
// by the upgrade, even though the candidate runtime asset is supplied/verified.
// Version defaults to 0.1.25; 0.1.25 test suffixes are allowed explicitly.
// FASTLANE_OPENWRT_FASTLANE_BIN can point at the matching prebuilt candidate to
// avoid the harness's unused binary build. No build/publish step here
// writes shared dist/. The installer is rendered from this checkout into TempDir.
// FASTLANE_OPENWRT_UPGRADE_INSTALLER optionally supplies an already rendered
// installer, which must match the current template/version/supported arches.
// Optional FASTLANE_OPENWRT_UPGRADE_STABLE_DIR supplies the five official files
// below offline, but cannot override their pins. Only a fresh harness VM is used.
//
// Scope: archive installer rollback, NOT a downgrade bypass or package-manager
// transaction. A failed candidate is rolled back while 0.1.24 is still installed;
// after a successful 0.1.25 upgrade the official old installer must reject it.
// HTTPS egress and authenticated management HTTP are separate assertions: the
// product has no native management TLS. Management is opt-in via environment,
// so its helper enables it only AFTER checking autonomous service boot. This
// does not claim that management-listener configuration survives reboot.
// OpenWrt/application assets are pinned; opkg/Geo downloads retain the real
// installers' dependency behavior and require network access.
const archiveUpgradeStableBase = "https://github.com/design-maestro/fastlane/releases/download/v0.1.24/"

// GitHub official release asset digests, cross-checked with archive sidecars:
// https://api.github.com/repos/design-maestro/fastlane/releases/tags/v0.1.24
var archiveUpgradeStablePins = map[string]string{
	"install.sh":                           "dcfee5bffef345ee82e03ee1fe0f27ad3761814aef9c63a921f23cecb6117934",
	"fastlane_0.1.24_x86_64.tar.gz":        "666086b4a09ec82551bfc73065985e7fca96ce502f223754e8c0c8cb8c792f3a",
	"fastlane_0.1.24_x86_64.tar.gz.sha256": "84e82e1b4f3a06efd3d1be3406e0c9229c26c6a7ce1d9ea4806c5ca7289828fe",
	"xray_0.1.24_x86_64.tar.gz":            "ef9ba2484538116e81c959e2a9c79031ec4664ed73c5c92407ca89ec28eb038c",
	"xray_0.1.24_x86_64.tar.gz.sha256":     "68c1fc0c96959b4b88ba2271e3790c923e6c5ae6ddf263639d38d5ee558dc0f6",
}

func TestOpenWrtPublishedArchiveUpgrade(t *testing.T) {
	if os.Getenv("FASTLANE_RUN_OPENWRT_ARCHIVE_UPGRADE") != "1" {
		t.Skip("explicit opt-in required; run this archive test alone")
	}
	// Each harness owns a fresh image and free forwarded ports. The explicit
	// opt-in prevents broad integration commands from starting this costly VM.
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
	defer cancel()
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	stable, candidate := t.TempDir(), t.TempDir()
	candidateVersion := archiveUpgradeCandidateVersion(t)
	input := os.Getenv("FASTLANE_OPENWRT_UPGRADE_ASSET_DIR")
	if input == "" || !filepath.IsAbs(input) {
		t.Fatal("FASTLANE_OPENWRT_UPGRADE_ASSET_DIR must name an explicit absolute candidate asset directory")
	}
	var installedPaths []string
	for _, name := range []string{"fastlane_" + candidateVersion + "_x86_64.tar.gz", "xray_" + candidateVersion + "_x86_64.tar.gz"} {
		for _, suffix := range []string{"", ".sha256"} {
			if err := copyFile(filepath.Join(input, name+suffix), filepath.Join(candidate, name+suffix)); err != nil {
				t.Fatal(err)
			}
		}
		if err := archiveUpgradeVerifySidecar(filepath.Join(candidate, name)); err != nil {
			t.Fatal(err)
		}
		expectedVersion := ""
		if strings.HasPrefix(name, "fastlane_") {
			expectedVersion = candidateVersion
		}
		files, err := archiveUpgradeInspect(filepath.Join(candidate, name), expectedVersion)
		if err != nil {
			t.Fatal(err)
		}
		installedPaths = append(installedPaths, files...)
		digest, err := archiveUpgradeDigest(filepath.Join(candidate, name))
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("candidate %s sha256=%s", name, digest)
	}
	archiveUpgradePrepareInstaller(t, ctx, root, filepath.Join(candidate, "install.sh"), candidateVersion)
	for name, digest := range archiveUpgradeStablePins {
		dest := filepath.Join(stable, name)
		if source := os.Getenv("FASTLANE_OPENWRT_UPGRADE_STABLE_DIR"); source != "" {
			err = copyFile(filepath.Join(source, name), dest)
		} else {
			err = downloadFile(archiveUpgradeStableBase+name, dest)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := archiveUpgradeVerifyDigest(dest, digest); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"fastlane_0.1.24_x86_64.tar.gz", "xray_0.1.24_x86_64.tar.gz"} {
		if err := archiveUpgradeVerifySidecar(filepath.Join(stable, name)); err != nil {
			t.Fatal(err)
		}
		files, err := archiveUpgradeInspect(filepath.Join(stable, name), "")
		if err != nil {
			t.Fatal(err)
		}
		installedPaths = append(installedPaths, files...)
	}
	h, err := newOpenWRTHarness(t)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	// Verify the official compressed image and regenerate the working copy;
	// never trust a previously decompressed mutable cache as our baseline.
	imageGZ := filepath.Join(h.cacheDir, filepath.Base(openWrtImageURL))
	if err := archiveUpgradeVerifyDigest(imageGZ, "5b854a7a2909a0b21887ba9be773b0964eb28fef81d6b8399d32a0f270a5d8ff"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(h.qemuImagePath); err != nil {
		t.Fatal(err)
	}
	if err := gunzipFile(imageGZ, h.qemuImagePath); err != nil {
		t.Fatal(err)
	}
	if err := h.Start(ctx); err != nil {
		t.Fatal(err)
	}
	guest := func(command string) {
		t.Helper()
		if err := h.sshCommand(ctx, command); err != nil {
			t.Fatal(err)
		}
	}
	guest("umask 077; mkdir -p /tmp/archive-upgrade/stable /tmp/archive-upgrade/candidate /tmp/archive-upgrade/fault")
	for _, set := range []struct{ local, remote string }{{stable, "stable"}, {candidate, "candidate"}} {
		entries, err := os.ReadDir(set.local)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if err := h.scpFile(ctx, filepath.Join(set.local, entry.Name()), "/tmp/archive-upgrade/"+set.remote+"/"+entry.Name()); err != nil {
				t.Fatal(err)
			}
		}
	}
	// No InstallFastLane/InstallXray calls: every installed application byte
	// must come from the official/candidate archive and its actual installer.
	guest("sh /tmp/archive-upgrade/stable/install.sh --arch x86_64 --asset-dir /tmp/archive-upgrade/stable >/tmp/archive-upgrade/stable.log 2>&1")
	// Use settings supported by both the published stable binary and the
	// candidate. Newer-only fields would test CLI compatibility rather than
	// preservation across an archive upgrade.
	guest("fastlane settings set url-test-timeout 17s && fastlane settings set health-check-interval 0s")
	archiveUpgradeAssertState(t, ctx, h, "0.1.24")
	guest("sha256sum /usr/bin/xray >/tmp/archive-upgrade/xray.sha256")
	installedPaths = append(installedPaths, "etc/crontabs/root", "etc/fastlane/install-manifest.txt")
	sort.Strings(installedPaths)
	uniquePaths := installedPaths[:0]
	for _, installedPath := range installedPaths {
		if len(uniquePaths) == 0 || uniquePaths[len(uniquePaths)-1] != installedPath {
			uniquePaths = append(uniquePaths, installedPath)
		}
	}
	installedPaths = uniquePaths
	pathsFile := filepath.Join(t.TempDir(), "paths")
	if err := os.WriteFile(pathsFile, []byte(strings.Join(installedPaths, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.scpFile(ctx, pathsFile, "/tmp/archive-upgrade/paths"); err != nil {
		t.Fatal(err)
	}
	snapshot := `while IFS= read -r relative; do
  file="/$relative"
  printf '%s ' "$relative"
	if [ -L "$file" ]; then readlink "$file"
	elif [ -f "$file" ]; then LC_ALL=C ls -ld "$file" | awk '{print $1}'; sha256sum "$file"
	else printf 'absent\n'; fi
done < /tmp/archive-upgrade/paths`
	beforeSnapshot, err := h.sshOutput(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	// Fail once at the late manifest publication, after real binary/helper and
	// release-data replacements. Preserve the archived bytes/digests unchanged.
	fault := `#!/bin/sh
set -eu
previous=''; last=''
for arg do previous="$last"; last="$arg"; done
case "$previous" in
  *.fastlane-new.*)
    if [ "$last" = /etc/fastlane/install-manifest.txt ] && [ ! -e /tmp/archive-upgrade/fault-fired ]; then
      : >/tmp/archive-upgrade/fault-fired
      exit 73
    fi ;;
esac
exec /bin/mv "$@"
`
	faultFile := filepath.Join(t.TempDir(), "mv")
	if err := os.WriteFile(faultFile, []byte(fault), 0700); err != nil {
		t.Fatal(err)
	}
	if err := h.scpFile(ctx, faultFile, "/tmp/archive-upgrade/fault/mv"); err != nil {
		t.Fatal(err)
	}
	failureCommand := "chmod 0700 /tmp/archive-upgrade/fault/mv; PATH=/tmp/archive-upgrade/fault:$PATH sh /tmp/archive-upgrade/candidate/install.sh --arch x86_64 --asset-dir /tmp/archive-upgrade/candidate >/tmp/archive-upgrade/failure.log 2>&1; failure_status=$?; test \"$failure_status\" -ne 0 && test -f /tmp/archive-upgrade/fault-fired && grep -q 'previous files and service state restored' /tmp/archive-upgrade/failure.log && ! grep -q 'installed successfully' /tmp/archive-upgrade/failure.log"
	if err := h.sshCommand(ctx, failureCommand); err != nil {
		trace, _ := h.sshOutput(ctx, "tail -80 /tmp/archive-upgrade/failure.log")
		t.Fatalf("injected failure did not roll back cleanly: %v\n%s", err, trace)
	}
	afterSnapshot, err := h.sshOutput(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeSnapshot, afterSnapshot) {
		beforeLines := strings.Split(string(beforeSnapshot), "\n")
		afterLines := strings.Split(string(afterSnapshot), "\n")
		for index := 0; index < len(beforeLines) || index < len(afterLines); index++ {
			var beforeLine, afterLine string
			if index < len(beforeLines) {
				beforeLine = beforeLines[index]
			}
			if index < len(afterLines) {
				afterLine = afterLines[index]
			}
			if beforeLine != afterLine {
				t.Fatalf("rollback snapshot differs at line %d: before=%q after=%q", index+1, beforeLine, afterLine)
			}
		}
		t.Fatal("rollback snapshot differs")
	}
	guest("test -z \"$(find /tmp -maxdepth 1 -name 'fastlane-install.*' -print)\"; test -z \"$(find /etc /usr /www -name '*.fastlane-new.*' -o -name '*.fastlane-rollback.*')\"")
	archiveUpgradeAssertState(t, ctx, h, "0.1.24")
	t.Log("real candidate failure restored 0.1.24 archive files/modes/existence and service; shared setting preserved")
	// The official generic image leaves less free rootfs than one additional
	// atomic Fast Lane binary. Keep the already-running Xray inode in tmpfs only
	// during the successful update, then restore the exact pinned bytes before
	// reboot. This changes disposable fixture capacity, not installer behavior.
	guest("cp -p /usr/bin/xray /tmp/archive-upgrade/xray-runtime; rm -f /usr/bin/xray; ln -s /tmp/archive-upgrade/xray-runtime /usr/bin/xray")
	guest("sh /tmp/archive-upgrade/candidate/install.sh --arch x86_64 --asset-dir /tmp/archive-upgrade/candidate >/tmp/archive-upgrade/upgrade.log 2>&1")
	guest("rm -f /usr/bin/xray; cp -p /tmp/archive-upgrade/xray-runtime /usr/bin/xray; rm -f /tmp/archive-upgrade/xray-runtime")
	archiveUpgradeAssertState(t, ctx, h, candidateVersion)
	// Keep the official old installer on persistent guest storage solely for
	// the post-reboot rejection check; /tmp assets intentionally disappear.
	guest("mkdir -p /root/archive-upgrade-fixture; cp /tmp/archive-upgrade/stable/install.sh /root/archive-upgrade-fixture/old-install.sh; cp /tmp/archive-upgrade/xray.sha256 /root/archive-upgrade-fixture/xray.sha256; chmod 0700 /root/archive-upgrade-fixture")
	if err := h.RebootAndWait(ctx); err != nil {
		t.Fatal(err)
	}
	archiveUpgradeAssertState(t, ctx, h, candidateVersion)
	guest("sha256sum -c /root/archive-upgrade-fixture/xray.sha256 >/dev/null")
	t.Log("0.1.25 version, shared settings and enabled/running service verified after reboot")
	// Re-enabling an optional API is explicit, and cannot mask failed boot:
	// running/enabled and CLI assertions have already succeeded above.
	if err := h.AssertManagementHTTP(ctx); err != nil {
		t.Fatal(err)
	}
	response, err := h.managementRequest(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/v1/settings-panel", h.managementPort), "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var state struct {
		Settings struct {
			URLTestTimeout string `json:"url_test_timeout"`
		} `json:"settings"`
		Update struct {
			CurrentVersion string `json:"current_version"`
		} `json:"update"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || state.Update.CurrentVersion != candidateVersion || state.Settings.URLTestTimeout != "17s" {
		t.Fatal("management HTTP did not expose the installed version and preserved shared setting")
	}
	guest("if sh /root/archive-upgrade-fixture/old-install.sh --arch x86_64 >/tmp/archive-downgrade.log 2>&1; then exit 1; fi; grep -Fq " + shellQuote("refusing downgrade from "+candidateVersion+" to 0.1.24") + " /tmp/archive-downgrade.log")
	archiveUpgradeAssertState(t, ctx, h, candidateVersion)
	t.Log("downgrade rejected without modifying 0.1.25; automatic failure recovery is not manual downgrade support")
}

func archiveUpgradeAssertState(t *testing.T, ctx context.Context, h *openWRTHarness, version string) {
	t.Helper()
	out, err := h.sshOutput(ctx, "fastlane --json version")
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(out, &info); err != nil || strings.TrimPrefix(info.Version, "v") != version {
		t.Fatalf("installed version is not %s: %v", version, err)
	}
	if err := h.sshCommand(ctx, "fastlane settings get | grep -qx 'url-test-timeout=17s'"); err != nil {
		t.Fatal("shared settings marker not preserved: ", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := h.sshCommand(ctx, "/etc/init.d/fastlane enabled && /etc/init.d/fastlane running"); err == nil {
			return
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			t.Fatal("installed service did not autonomously become enabled/running")
		}
		time.Sleep(time.Second)
	}
}

func archiveUpgradeCandidateVersion(t *testing.T) string {
	t.Helper()
	version := os.Getenv("FASTLANE_OPENWRT_UPGRADE_VERSION")
	if version == "" {
		version = "0.1.25"
	}
	if !regexp.MustCompile(`^0\.1\.25(?:-[A-Za-z0-9][A-Za-z0-9.-]*)?$`).MatchString(version) {
		t.Fatal("expected explicit 0.1.25 or 0.1.25-<test suffix> candidate version")
	}
	return version
}

func archiveUpgradePrepareInstaller(t *testing.T, ctx context.Context, root, output, version string) {
	t.Helper()
	render := exec.CommandContext(ctx, "sh", filepath.Join(root, "scripts/render-install.sh"), version, output, "mipsel_24kc", "x86_64", "aarch64_cortex-a53")
	render.Dir = root
	if out, err := render.CombinedOutput(); err != nil {
		t.Fatalf("render candidate installer: %v: %s", err, out)
	}
	if supplied := os.Getenv("FASTLANE_OPENWRT_UPGRADE_INSTALLER"); supplied != "" {
		if !filepath.IsAbs(supplied) {
			t.Fatal("candidate installer must be an absolute explicit path")
		}
		expected, err := archiveUpgradeDigest(output)
		if err != nil {
			t.Fatal(err)
		}
		if err := archiveUpgradeVerifyDigest(supplied, expected); err != nil {
			t.Fatal("supplied installer differs from the current locally rendered candidate: ", err)
		}
		if err := copyFile(supplied, output); err != nil {
			t.Fatal(err)
		}
	}
	digest, err := archiveUpgradeDigest(output)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("candidate installer sha256=%s", digest)
}

// Safe real-artifact preflight: no network, no harness/VM, no package builds.
// Run with the same explicit input paths before spending a QEMU run.
func TestArchiveUpgradeProvidedAssets(t *testing.T) {
	input, stable := os.Getenv("FASTLANE_OPENWRT_UPGRADE_ASSET_DIR"), os.Getenv("FASTLANE_OPENWRT_UPGRADE_STABLE_DIR")
	if input == "" || stable == "" {
		t.Skip("set explicit candidate and stable directories for offline real-artifact preflight")
	}
	version := archiveUpgradeCandidateVersion(t)
	for _, runtime := range []string{"fastlane", "xray"} {
		file := filepath.Join(input, runtime+"_"+version+"_x86_64.tar.gz")
		if err := archiveUpgradeVerifySidecar(file); err != nil {
			t.Fatal(err)
		}
		expectedVersion := ""
		if runtime == "fastlane" {
			expectedVersion = version
		}
		if _, err := archiveUpgradeInspect(file, expectedVersion); err != nil {
			t.Fatal(err)
		}
	}
	for name, digest := range archiveUpgradeStablePins {
		if err := archiveUpgradeVerifyDigest(filepath.Join(stable, name), digest); err != nil {
			t.Fatal(err)
		}
	}
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	archiveUpgradePrepareInstaller(t, ctx, root, filepath.Join(t.TempDir(), "install.sh"), version)
}

func archiveUpgradeDigest(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func archiveUpgradeVerifyDigest(file, expected string) error {
	actual, err := archiveUpgradeDigest(file)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("SHA-256 mismatch for %s", filepath.Base(file))
	}
	return nil
}

func archiveUpgradeVerifySidecar(file string) error {
	data, err := os.ReadFile(file + ".sha256")
	if err != nil {
		return err
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 || len(fields[0]) != 64 || strings.TrimPrefix(fields[1], "*") != filepath.Base(file) {
		return fmt.Errorf("invalid checksum sidecar for %s", filepath.Base(file))
	}
	return archiveUpgradeVerifyDigest(file, fields[0])
}

func archiveUpgradeInspect(file, candidateVersion string) ([]string, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	var files []string
	foundBinary := false
	expectedBinary := "usr/bin/xray"
	if candidateVersion != "" || strings.HasPrefix(filepath.Base(file), "fastlane_") {
		expectedBinary = "usr/bin/fastlane"
	}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		name := strings.TrimPrefix(header.Name, "./")
		name = strings.TrimSuffix(name, "/")
		if name == "." || name == "" {
			continue
		}
		if path.Clean(name) != name || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") || name == ".." || strings.ContainsAny(name, "\n\r\t") {
			return nil, fmt.Errorf("unsafe archive path")
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("archive has non-regular entry: %s", name)
		}
		files = append(files, name)
		if target, ok := strings.CutPrefix(name, "usr/libexec/fastlane-release-data/"); ok {
			files = append(files, target)
		}
		if name == "usr/bin/fastlane" || name == "usr/bin/xray" {
			if header.Size <= 0 || header.Size > 128<<20 {
				return nil, fmt.Errorf("invalid runtime binary size")
			}
			data, err := io.ReadAll(reader)
			if err != nil {
				return nil, err
			}
			binaryFile, err := elf.NewFile(bytes.NewReader(data))
			if err != nil {
				return nil, fmt.Errorf("runtime is not a real ELF binary: %w", err)
			}
			if binaryFile.Machine != elf.EM_X86_64 || binaryFile.Class != elf.ELFCLASS64 {
				return nil, fmt.Errorf("runtime is not Linux amd64")
			}
			if name == expectedBinary {
				foundBinary = true
			}
			if candidateVersion != "" && name == "usr/bin/fastlane" {
				info, err := buildinfo.Read(bytes.NewReader(data))
				if err != nil {
					return nil, err
				}
				version, linux, amd64, hasLinkFlags := false, false, false, false
				for _, setting := range info.Settings {
					switch setting.Key {
					case "-ldflags":
						hasLinkFlags = true
						for _, field := range strings.Fields(setting.Value) {
							if field == "github.com/design-maestro/fastlane/internal/buildinfo.Version="+candidateVersion {
								version = true
							}
						}
					case "GOOS":
						linux = setting.Value == "linux"
					case "GOARCH":
						amd64 = setting.Value == "amd64"
					}
				}
				// Go's -trimpath builds may omit -ldflags from build metadata.
				// Do not reject real release binaries or infer their version from
				// a filename: the actual --json version is asserted in the guest.
				if (hasLinkFlags && !version) || !linux || !amd64 {
					return nil, fmt.Errorf("candidate binary must be built for linux/amd64 with exact VERSION=%s", candidateVersion)
				}
			}
		}
	}
	if !foundBinary {
		return nil, fmt.Errorf("archive lacks a runtime binary")
	}
	return files, nil
}

// Grow only the harness's disposable MBR/ext4 root image, never a host device
// or cached image. The official image is too small for full Geo/runtime assets.
func archiveUpgradeGrowImage(ctx context.Context, image, workDir string) error {
	if filepath.Dir(image) != workDir {
		return fmt.Errorf("refusing to resize outside harness TempDir")
	}
	f, err := os.OpenFile(image, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	mbr := make([]byte, 512)
	if _, err := f.ReadAt(mbr, 0); err != nil {
		return err
	}
	if mbr[510] != 0x55 || mbr[511] != 0xaa || mbr[466] != 0x83 {
		return fmt.Errorf("expected official MBR Linux root partition 2")
	}
	for _, offset := range []int{478, 494} {
		if binary.LittleEndian.Uint32(mbr[offset+12:offset+16]) != 0 {
			return fmt.Errorf("refusing to overwrite additional partition")
		}
	}
	start := int64(binary.LittleEndian.Uint32(mbr[470:474])) * 512
	oldSize := int64(binary.LittleEndian.Uint32(mbr[474:478])) * 512
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if start < 512 || oldSize <= 0 || start+oldSize > info.Size() {
		return fmt.Errorf("invalid root partition extent")
	}
	rootFS := filepath.Join(workDir, "archive-upgrade-root.ext4")
	partition, err := os.OpenFile(rootFS, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer partition.Close()
	if _, err := io.CopyN(partition, io.NewSectionReader(f, start, oldSize), oldSize); err != nil {
		return err
	}
	const newSize = int64(768 << 20)
	if err := partition.Truncate(newSize); err != nil {
		return err
	}
	if err := partition.Sync(); err != nil {
		return err
	}
	check := exec.CommandContext(ctx, "e2fsck", "-f", "-p", rootFS)
	if out, err := check.CombinedOutput(); err != nil {
		if code, ok := err.(*exec.ExitError); !ok || code.ExitCode() != 1 {
			return fmt.Errorf("check disposable ext4: %v: %s", err, out)
		}
	}
	if out, err := exec.CommandContext(ctx, "resize2fs", rootFS).CombinedOutput(); err != nil {
		return fmt.Errorf("grow disposable ext4: %v: %s", err, out)
	}
	if err := f.Truncate(start + newSize); err != nil {
		return err
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.CopyN(f, io.NewSectionReader(partition, 0, newSize), newSize); err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(mbr[474:478], uint32(newSize/512))
	if _, err := f.WriteAt(mbr, 0); err != nil {
		return err
	}
	return f.Sync()
}

func TestArchiveUpgradeChecksumPreflight(t *testing.T) {
	file := filepath.Join(t.TempDir(), "fixture.tar.gz")
	if err := os.WriteFile(file, []byte("synthetic asset"), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := archiveUpgradeDigest(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, sidecar string
		valid         bool
	}{
		{"valid", digest + "  fixture.tar.gz\n", true},
		{"wrong-file", digest + "  another.tar.gz\n", false},
		{"wrong-digest", strings.Repeat("0", 64) + "  fixture.tar.gz\n", false},
		{"multiple-records", digest + "  fixture.tar.gz\n" + digest + "  fixture.tar.gz\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(file+".sha256", []byte(tc.sidecar), 0600); err != nil {
				t.Fatal(err)
			}
			if err := archiveUpgradeVerifySidecar(file); (err == nil) != tc.valid {
				t.Fatalf("valid=%t, error=%v", tc.valid, err)
			}
		})
	}
}

func TestArchiveUpgradeRejectsStubAndUnsafeArchive(t *testing.T) {
	for _, name := range []string{"usr/bin/fastlane", "../usr/bin/fastlane", "/usr/bin/fastlane"} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			gz := gzip.NewWriter(&buf)
			tw := tar.NewWriter(gz)
			data := []byte("#!/bin/sh\nexit 0\n")
			if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(data))}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(data); err != nil {
				t.Fatal(err)
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(t.TempDir(), "fixture.tar.gz")
			if err := os.WriteFile(file, buf.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := archiveUpgradeInspect(file, "0.1.25"); err == nil {
				t.Fatal("unsafe/stub candidate accepted")
			}
		})
	}
}

func TestArchiveUpgradeImageGuard(t *testing.T) {
	work := t.TempDir()
	image := filepath.Join(work, "invalid.img")
	before := bytes.Repeat([]byte{0}, 512)
	if err := os.WriteFile(image, before, 0600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{t.TempDir(), work} {
		if err := archiveUpgradeGrowImage(context.Background(), image, dir); err == nil {
			t.Fatal("unsafe or invalid image accepted for resizing")
		}
	}
	after, err := os.ReadFile(image)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("image changed before validating its location and partition table")
	}
}
