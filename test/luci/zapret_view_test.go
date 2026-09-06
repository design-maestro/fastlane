package luci_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestZapretViewIncludesCompactFallbackAndSelectorEditor(t *testing.T) {
	t.Parallel()

	source := readZapretViewSource(t)

	for _, want := range []string{
		"Fast Lane - Zapret",
		"Automatic fallback",
		"Failback Success Threshold",
		"Zapret Test",
		"Start ZapretTest",
		"Return to Previous Route",
		"Choose what Zapret should cover",
		"Create custom presets from domains and IPv4 selectors. Zapret only covers the presets listed below.",
		"Saved presets are added to Zapret immediately. If no presets are listed below, fallback stays disabled.",
		"Active presets in Zapret: %d. Expanded selectors: %d.",
		"switch this router into Zapret even while proxy nodes stay healthy",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("zapret view missing marker %q", want)
		}
	}

	for _, forbidden := range []string{
		"fastlane-zapret-resource-grid",
		"Built-in presets are readonly",
		"Preset aliases",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("zapret view must not contain %q", forbidden)
		}
	}
}

func TestZapretViewUsesDedicatedCommands(t *testing.T) {
	t.Parallel()

	source := readZapretViewSource(t)

	for _, want := range []string{
		"'--json', 'zapret', 'get'",
		"'--json', 'zapret', 'status'",
		"'--json', 'services', 'list'",
		"'zapret', 'set', 'enabled'",
		"'zapret', 'set', 'selectors'",
		"'zapret', 'set', 'failback-success-threshold'",
		"'services', 'set'",
		"'services', 'delete'",
		"'zapret', 'test', 'start'",
		"'zapret', 'test', 'stop'",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("zapret view must contain %q", want)
		}
	}

	for _, forbidden := range []string{
		"'--json', 'zapret', 'resources'",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("zapret view must not contain %q", forbidden)
		}
	}
}

func TestZapretViewUsesGreenSelectedChoiceState(t *testing.T) {
	t.Parallel()

	source := readZapretViewSource(t)

	for _, want := range []string{
		"fastlane-zapret-choice-indicator",
		"fastlane-zapret-choice-control",
		"rgba(34, 197, 94, 0.52)",
		"rgba(220, 252, 231, 0.99)",
		"content:\"\\\\2713\"",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("zapret view missing green choice marker %q", want)
		}
	}
}

func TestZapretViewUsesCompactSelectorEditorInsteadOfPresetBlocks(t *testing.T) {
	t.Parallel()

	source := readZapretViewSource(t)

	for _, want := range []string{
		"fastlane-zapret-list",
		"fastlane-zapret-item",
		"fastlane-zapret-badge-preset",
		"fastlane-zapret-item-value",
		"Preset name",
		"Preset domains and IPv4",
		"Change",
		"Delete",
		"Save preset",
		"Saved presets are added to Zapret immediately",
		"zapret-",
		"YouTube",
		"googlevideo.com",
		"readPresetDraftName",
		"readPresetDraftSelectors",
		"autocorrect': 'off",
		"setLocalZapretSelectors",
		"selectedZapretPresetNames",
		"'refresh': false",
		"'--json', 'services', 'get'",
		"Zapret fallback needs at least one allowed preset.",
		"inputmode': 'text",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("zapret view missing compact selector marker %q", want)
		}
	}

	for _, forbidden := range []string{
		"Guided resources",
		"fastlane-zapret-resource-grid",
		"fastlane-zapret-resource",
		"fastlane-zapret-resource-selected",
		"fastlane-zapret-alias-select",
		"Direct selectors",
		"Add selector",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("zapret view must not keep preset/resource marker %q", forbidden)
		}
	}
}

func TestZapretViewRemovesDarkShellAroundLightPanels(t *testing.T) {
	t.Parallel()

	source := readZapretViewSource(t)

	for _, want := range []string{
		"#fastlane-zapret-root.fastlane-theme-dark::before, #fastlane-zapret-root.fastlane-theme-dark::after { display:none; }",
		"#fastlane-zapret-root .fastlane-zapret-layout { display:grid; gap:14px; padding:0; border:0; background:transparent; box-shadow:none;",
		"#fastlane-zapret-root .fastlane-zapret-layout::before { display:none; }",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("zapret view missing shell cleanup marker %q", want)
		}
	}
}

func TestZapretViewUsesReadableLightPanelCopy(t *testing.T) {
	t.Parallel()

	source := readZapretViewSource(t)

	for _, want := range []string{
		"--fastlane-zapret-ink-muted:#3e5368",
		"--fastlane-zapret-ink-soft:#5c7085",
		".fastlane-zapret-panel .cbi-section-descr { color:var(--fastlane-zapret-ink-muted); font-size:15px; font-weight:500;",
		".fastlane-zapret-summary-shell { padding:14px 16px; border:1px solid rgba(125, 145, 168, 0.22); border-radius:14px; background:rgba(255, 255, 255, 0.72); box-shadow:none; }",
		".fastlane-zapret-summary-shell h3 { margin-top:0; margin-bottom:8px; color:var(--fastlane-zapret-ink); font-size:18px; letter-spacing:-0.02em; }",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("zapret view missing readable copy marker %q", want)
		}
	}
}

func TestZapretViewUsesLightActionButtonsInsteadOfDarkNavy(t *testing.T) {
	t.Parallel()

	source := readZapretViewSource(t)

	for _, want := range []string{
		"--fastlane-zapret-ink:#102234",
		"--fastlane-zapret-ink-muted:#3e5368",
		"--fastlane-zapret-ink-soft:#5c7085",
		".fastlane-zapret-inline > .cbi-button-action, .fastlane-zapret-actions .cbi-button { min-height:52px; padding:0 18px; border:1px solid rgba(37, 99, 235, 0.18); border-radius:15px; background:linear-gradient(180deg, rgba(243, 248, 253, 0.98) 0%, rgba(232, 240, 248, 0.98) 100%); color:#17324b;",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("zapret view missing light action marker %q", want)
		}
	}
}

func TestZapretViewUsesReadableLightInputsAndPlaceholders(t *testing.T) {
	t.Parallel()

	source := readZapretViewSource(t)

	for _, want := range []string{
		".fastlane-theme-light .fastlane-zapret-inline > .cbi-input-text { border-color:rgba(125, 146, 170, 0.18); background:#fcfdff; color:#162638; box-shadow:none; }",
		".fastlane-theme-light .fastlane-zapret-inline > .cbi-input-textarea { border-color:rgba(125, 146, 170, 0.18); background:#fcfdff; color:#162638; box-shadow:none; }",
		".fastlane-theme-light .fastlane-zapret-inline > .cbi-input-textarea::placeholder { color:#63768c; opacity:1; }",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("zapret view missing light input marker %q", want)
		}
	}
}

func TestZapretViewUsesPremiumDarkThemeChoicesAndEditors(t *testing.T) {
	t.Parallel()

	source := readZapretViewSource(t)

	for _, want := range []string{
		"#fastlane-zapret-root.fastlane-theme-dark { --fastlane-zapret-ink:#eef4ff; --fastlane-zapret-ink-muted:#a8b8ce; --fastlane-zapret-ink-soft:#8ea0b8;",
		".fastlane-theme-dark .fastlane-zapret-choice { border-color:rgba(145, 175, 220, 0.16); background:linear-gradient(180deg, rgba(11, 18, 30, 0.94) 0%, rgba(8, 14, 24, 0.98) 100%);",
		".fastlane-theme-dark .fastlane-zapret-choice-selected { border-color:rgba(34, 197, 94, 0.42); background:linear-gradient(180deg, rgba(13, 35, 28, 0.96) 0%, rgba(10, 24, 21, 1) 100%);",
		".fastlane-theme-dark .fastlane-zapret-inline > .cbi-input-textarea { border-color:rgba(145, 175, 220, 0.16); background:rgba(6, 12, 22, 0.88); color:#eef4ff; box-shadow:none; }",
		".fastlane-theme-dark .fastlane-zapret-item { background:linear-gradient(180deg, rgba(11, 18, 30, 0.94) 0%, rgba(8, 14, 24, 0.98) 100%); border-color:rgba(145, 175, 220, 0.14); box-shadow:none; }",
		".fastlane-theme-dark .fastlane-zapret-empty { background:rgba(8, 15, 26, 0.5); border-color:rgba(145, 175, 220, 0.24); color:#a8b8ce; }",
		".fastlane-theme-dark .fastlane-zapret-summary-shell { background:rgba(8, 15, 26, 0.58); border-color:rgba(145, 175, 220, 0.16); box-shadow:none; }",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("zapret view missing dark-theme marker %q", want)
		}
	}
}

func readZapretViewSource(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	path := filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "zapret.js")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}
