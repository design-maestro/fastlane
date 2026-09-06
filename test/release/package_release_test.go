package release_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageReleaseBuildsAllPublishedArchitectures(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "release.log")

	scriptSource, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts", "package-release.sh"))
	if err != nil {
		t.Fatalf("read package-release.sh: %v", err)
	}

	writeExecutable(t, filepath.Join(repoDir, "scripts", "package-release.sh"), string(scriptSource))
	writeExecutable(t, filepath.Join(repoDir, "scripts", "build-openwrt.sh"), `#!/bin/sh
set -eu
printf 'build-openwrt GOARCH=%s GOMIPS=%s OUTPUT_DIR=%s\n' "${GOARCH:-}" "${GOMIPS:-}" "${OUTPUT_DIR:-}" >> "${RELEASE_TEST_LOG:?}"
mkdir -p "${OUTPUT_DIR:?}"
: > "${OUTPUT_DIR}/fastlane"
`)
	writeExecutable(t, filepath.Join(repoDir, "scripts", "package-openwrt.sh"), `#!/bin/sh
set -eu
printf 'package-openwrt VERSION=%s ARCH=%s BINARY_DIR=%s\n' "${VERSION:-}" "${ARCH:-}" "${BINARY_PATH%/*}" >> "${RELEASE_TEST_LOG:?}"
mkdir -p dist
: > "dist/fastlane_${VERSION}_${ARCH}.ipk"
: > "dist/fastlane_${VERSION}_${ARCH}.tar.gz"
: > "dist/fastlane_${VERSION}_${ARCH}.ipk.sha256"
: > "dist/fastlane_${VERSION}_${ARCH}.tar.gz.sha256"
`)
	writeExecutable(t, filepath.Join(repoDir, "scripts", "build-xray.sh"), `#!/bin/sh
set -eu
printf 'build-xray GOARCH=%s GOMIPS=%s OUTPUT_DIR=%s\n' "${GOARCH:-}" "${GOMIPS:-}" "${OUTPUT_DIR:-}" >> "${RELEASE_TEST_LOG:?}"
mkdir -p "${OUTPUT_DIR:?}"
: > "${OUTPUT_DIR}/xray"
`)
	writeExecutable(t, filepath.Join(repoDir, "scripts", "package-xray.sh"), `#!/bin/sh
set -eu
printf 'package-xray VERSION=%s ARCH=%s BINARY_DIR=%s\n' "${VERSION:-}" "${ARCH:-}" "${BINARY_PATH%/*}" >> "${RELEASE_TEST_LOG:?}"
mkdir -p dist
: > "dist/xray_${VERSION}_${ARCH}.tar.gz"
: > "dist/xray_${VERSION}_${ARCH}.tar.gz.sha256"
`)
	writeExecutable(t, filepath.Join(repoDir, "scripts", "render-install.sh"), `#!/bin/sh
set -eu
printf 'render-install VERSION=%s ARCHES=%s|%s|%s\n' "$1" "$3" "$4" "$5" >> "${RELEASE_TEST_LOG:?}"
mkdir -p "$(dirname "$2")"
printf '#!/bin/sh\n' > "$2"
`)
	writeExecutable(t, filepath.Join(repoDir, "scripts", "uninstall.sh"), "#!/bin/sh\nexit 0\n")

	cmd := exec.Command("sh", filepath.Join(repoDir, "scripts", "package-release.sh"))
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"VERSION=1.2.3",
		"RELEASE_TEST_LOG="+logPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run package-release.sh: %v\n%s", err, output)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read release log: %v", err)
	}
	logOutput := string(logBytes)

	for _, want := range []string{
		"build-openwrt GOARCH=mipsle GOMIPS=softfloat OUTPUT_DIR=" + filepath.Join(repoDir, "bin", "openwrt", "mipsel_24kc"),
		"build-openwrt GOARCH=amd64 GOMIPS= OUTPUT_DIR=" + filepath.Join(repoDir, "bin", "openwrt", "x86_64"),
		"build-openwrt GOARCH=arm64 GOMIPS= OUTPUT_DIR=" + filepath.Join(repoDir, "bin", "openwrt", "aarch64_cortex-a53"),
		"package-openwrt VERSION=1.2.3 ARCH=mipsel_24kc BINARY_DIR=" + filepath.Join(repoDir, "bin", "openwrt", "mipsel_24kc"),
		"package-openwrt VERSION=1.2.3 ARCH=x86_64 BINARY_DIR=" + filepath.Join(repoDir, "bin", "openwrt", "x86_64"),
		"package-openwrt VERSION=1.2.3 ARCH=aarch64_cortex-a53 BINARY_DIR=" + filepath.Join(repoDir, "bin", "openwrt", "aarch64_cortex-a53"),
		"build-xray GOARCH=mipsle GOMIPS=softfloat OUTPUT_DIR=" + filepath.Join(repoDir, "bin", "xray", "mipsel_24kc"),
		"build-xray GOARCH=amd64 GOMIPS= OUTPUT_DIR=" + filepath.Join(repoDir, "bin", "xray", "x86_64"),
		"build-xray GOARCH=arm64 GOMIPS= OUTPUT_DIR=" + filepath.Join(repoDir, "bin", "xray", "aarch64_cortex-a53"),
		"package-xray VERSION=1.2.3 ARCH=mipsel_24kc BINARY_DIR=" + filepath.Join(repoDir, "bin", "xray", "mipsel_24kc"),
		"package-xray VERSION=1.2.3 ARCH=x86_64 BINARY_DIR=" + filepath.Join(repoDir, "bin", "xray", "x86_64"),
		"package-xray VERSION=1.2.3 ARCH=aarch64_cortex-a53 BINARY_DIR=" + filepath.Join(repoDir, "bin", "xray", "aarch64_cortex-a53"),
		"render-install VERSION=1.2.3 ARCHES=mipsel_24kc|x86_64|aarch64_cortex-a53",
	} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("expected release log to contain %q, got:\n%s", want, logOutput)
		}
	}

	for _, artifact := range []string{
		filepath.Join(repoDir, "dist", "fastlane_1.2.3_aarch64_cortex-a53.ipk"),
		filepath.Join(repoDir, "dist", "fastlane_1.2.3_aarch64_cortex-a53.ipk.sha256"),
		filepath.Join(repoDir, "dist", "fastlane_1.2.3_aarch64_cortex-a53.tar.gz"),
		filepath.Join(repoDir, "dist", "fastlane_1.2.3_aarch64_cortex-a53.tar.gz.sha256"),
		filepath.Join(repoDir, "dist", "xray_1.2.3_aarch64_cortex-a53.tar.gz"),
		filepath.Join(repoDir, "dist", "xray_1.2.3_aarch64_cortex-a53.tar.gz.sha256"),
		filepath.Join(repoDir, "dist", "install.sh"),
		filepath.Join(repoDir, "dist", "uninstall.sh"),
	} {
		if _, err := os.Stat(artifact); err != nil {
			t.Fatalf("expected artifact %s: %v", artifact, err)
		}
	}
}

func TestReleaseWorkflowPublishesInstallerDependenciesAndDirectIPKs(t *testing.T) {
	t.Parallel()

	workflow, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	source := string(workflow)
	for _, pattern := range []string{"dist/*.tar.gz", "dist/*.ipk", "dist/*.sha256", "dist/install.sh", "dist/uninstall.sh"} {
		if !strings.Contains(source, pattern) {
			t.Fatalf("release workflow must publish %q", pattern)
		}
	}
}

func TestReleaseInstallDocsTargetFastLaneRepository(t *testing.T) {
	t.Parallel()

	read := func(path ...string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(append([]string{repoRoot(t)}, path...)...))
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Join(path...), err)
		}
		return string(data)
	}

	readme := read("README.md")
	template := read("scripts", "install.sh.tmpl")
	if strings.Contains(readme, "github.com/legacy-owner/fastlane/releases") {
		t.Fatal("README install commands still target the upstream Fast Lane releases")
	}
	if !strings.Contains(readme, "github.com/design-maestro/fastlane/releases") {
		t.Fatal("README install commands must target the Fast Lane release repository")
	}
	for _, marker := range []string{
		`FASTLANE_REPO_OWNER="${FASTLANE_REPO_OWNER:-design-maestro}"`,
		`FASTLANE_REPO_NAME="${FASTLANE_REPO_NAME:-fastlane}"`,
	} {
		if !strings.Contains(template, marker) {
			t.Fatalf("installer repository default missing %q", marker)
		}
	}
}
