package luci_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFastLaneUISharedThemeSupportsDarkPremiumShell(t *testing.T) {
	t.Parallel()

	source := readFastLaneUISource(t)

	for _, want := range []string{
		"fastlane-theme-dark",
		"fastlane-page-shell",
		"fastlane-page-hero",
		"fastlane-page-hero-actions",
		"fastlane-surface",
		"fastlane-data-table",
		"fastlane-button-primary",
		"fastlane-section-heading",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("shared Fast Lane UI missing theme marker %q", want)
		}
	}
}

func TestFastLaneUISharedThemeSupportsLightModeAndPersistence(t *testing.T) {
	t.Parallel()

	source := readFastLaneUISource(t)

	for _, want := range []string{
		"fastlane-theme-light",
		"fastlane.ui.theme.preference",
		"currentTheme: function()",
		"setThemePreference: function(value)",
		"withThemeClass: function(className)",
		"window.localStorage",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("shared Fast Lane UI missing light theme marker %q", want)
		}
	}
}

func TestFastLaneUISharedThemeUsesReadableLightPalette(t *testing.T) {
	t.Parallel()

	source := readFastLaneUISource(t)

	for _, want := range []string{
		"--fastlane-bg:#f3f6fb",
		"--fastlane-surface:#f8fbfd",
		"--fastlane-text-primary:#162638",
		"--fastlane-text-secondary:#41566d",
		"--fastlane-text-muted:#6a7c91",
		".fastlane-page-shell.fastlane-theme-light .cbi-section-descr, .fastlane-page-shell.fastlane-theme-light .cbi-value-description { color:var(--fastlane-text-secondary);",
		".fastlane-page-shell.fastlane-theme-light pre { border-color:rgba(125, 146, 170, 0.16); background:linear-gradient(180deg, rgba(250, 252, 254, 0.98) 0%, rgba(243, 247, 251, 0.98) 100%);",
		".fastlane-page-shell.fastlane-theme-light code { background:rgba(37, 99, 235, 0.08); color:#1e3a8a; }",
		".fastlane-page-shell .cbi-page-actions { display:flex; flex-wrap:wrap; gap:10px; background:transparent !important; border:none !important; padding:0 !important; box-shadow:none !important; margin-top:12px !important; }",
		".fastlane-page-shell.fastlane-theme-light .cbi-button-apply, .fastlane-page-shell.fastlane-theme-light .btn.cbi-button-apply, .fastlane-theme-light .fastlane-button-primary { border-color:rgba(37, 99, 235, 0.34); background:linear-gradient(180deg, #2563eb 0%, #1d4ed8 100%);",
		".fastlane-page-shell.fastlane-theme-light .cbi-button-action, .fastlane-page-shell.fastlane-theme-light .btn.cbi-button-action, .fastlane-theme-light .fastlane-button-secondary { border-color:rgba(37, 99, 235, 0.18); background:linear-gradient(180deg, rgba(243, 248, 253, 0.98) 0%, rgba(232, 240, 248, 0.98) 100%); color:#1d4ed8;",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("shared Fast Lane UI missing readable light marker %q", want)
		}
	}
}

func readFastLaneUISource(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	path := filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "fastlane", "ui.js")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}
