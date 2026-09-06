package luci_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirewallViewUsesSimplifiedRoutingCopy(t *testing.T) {
	t.Parallel()

	source := readFirewallViewSource(t)

	for _, want := range []string{
		"Fast Lane - Routing",
		"Fast Lane status, the active connection, and the safe everyday routing actions.",
		"System DNS",
		"Recommended DNS preset",
		"Keep Direct",
		"Excluded Devices",
		"advanced Fast Lane mode",
		"current DNS profile is custom",
		"Advanced DNS settings are available in the CLI.",
		"CLI-only aliases",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("routing view missing marker %q", want)
		}
	}
}

func TestFirewallViewDefinesReadableContrastTheme(t *testing.T) {
	t.Parallel()

	source := readFirewallViewSource(t)

	for _, want := range []string{
		"--fastlane-routing-ink",
		"--fastlane-routing-panel-bg",
		"fastlane-routing-choice-selected",
		".fastlane-routing-panel .cbi-value-title",
		".fastlane-routing-inline > .cbi-button-action",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("routing view missing readability marker %q", want)
		}
	}
}

func TestFirewallViewUsesLightActionButtonsInsteadOfDarkNavy(t *testing.T) {
	t.Parallel()

	source := readFirewallViewSource(t)

	for _, want := range []string{
		".fastlane-routing-inline > .cbi-button-action { min-width:132px; min-height:52px; padding:0 18px; border:1px solid rgba(37, 99, 235, 0.18); border-radius:15px; background:linear-gradient(180deg, rgba(243, 248, 253, 0.98) 0%, rgba(232, 240, 248, 0.98) 100%); color:#17324b;",
		".fastlane-routing-actions .cbi-button { min-height:48px; padding:0 18px; border:1px solid rgba(37, 99, 235, 0.18); border-radius:15px; background:linear-gradient(180deg, rgba(243, 248, 253, 0.98) 0%, rgba(232, 240, 248, 0.98) 100%); color:#17324b;",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("routing view missing light action marker %q", want)
		}
	}

	for _, forbidden := range []string{
		"background:var(--fastlane-routing-surface-strong); color:#eef8ff;",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("routing view must not keep dark action marker %q", forbidden)
		}
	}
}

func TestFirewallViewUsesReadableLightInputsAndPlaceholders(t *testing.T) {
	t.Parallel()

	source := readFirewallViewSource(t)

	for _, want := range []string{
		".fastlane-theme-light .fastlane-routing-inline > .cbi-input-text, .fastlane-theme-light .fastlane-routing-inline > .cbi-input-select { border-color:rgba(125, 146, 170, 0.2); background:linear-gradient(180deg, rgba(251, 252, 254, 0.99) 0%, rgba(244, 248, 252, 0.99) 100%); color:#162638;",
		".fastlane-theme-light .fastlane-routing-inline .cbi-input-text::placeholder { color:#63768c; opacity:1; }",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("routing view missing light input marker %q", want)
		}
	}
}

func TestFirewallViewUsesStructuredKeepDirectSelectorShell(t *testing.T) {
	t.Parallel()

	source := readFirewallViewSource(t)

	for _, want := range []string{
		"fastlane-routing-selector-shell",
		"fastlane-routing-selector-head",
		"Direct selectors",
		"fastlane-routing-selector-meta",
		"fastlane-routing-item-value-code",
		"stay direct only while bypass mode is active",
		"Direct Domain or IPv4",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("routing view missing Keep Direct selector marker %q", want)
		}
	}
}

func TestFirewallViewRemovesDarkShellAroundRoutingPanels(t *testing.T) {
	t.Parallel()

	source := readFirewallViewSource(t)

	for _, want := range []string{
		"#fastlane-routing-root.fastlane-theme-dark::before, #fastlane-routing-root.fastlane-theme-dark::after { display:none; }",
		"#fastlane-routing-root .fastlane-routing-layout { display:grid; gap:14px; padding:0; border:0; background:transparent; box-shadow:none;",
		"#fastlane-routing-root .fastlane-routing-layout::before { display:none; }",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("routing view missing shell cleanup marker %q", want)
		}
	}
}

func TestFirewallViewUsesGreenSelectedChoiceState(t *testing.T) {
	t.Parallel()

	source := readFirewallViewSource(t)

	for _, want := range []string{
		"fastlane-routing-choice-indicator",
		"fastlane-routing-choice-control",
		"rgba(34, 197, 94, 0.52)",
		"rgba(220, 252, 231, 0.99)",
		"content:\"\\\\2713\"",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("routing view missing green choice marker %q", want)
		}
	}
}

func TestFirewallViewUsesPremiumDarkThemeChoicesAndSelectors(t *testing.T) {
	t.Parallel()

	source := readFirewallViewSource(t)

	for _, want := range []string{
		"#fastlane-routing-root.fastlane-theme-dark { --fastlane-routing-ink:#eef4ff; --fastlane-routing-ink-muted:#a8b8ce; --fastlane-routing-ink-soft:#8ea0b8;",
		".fastlane-theme-dark .fastlane-routing-choice { border-color:rgba(145, 175, 220, 0.16); background:linear-gradient(180deg, rgba(11, 18, 30, 0.94) 0%, rgba(8, 14, 24, 0.98) 100%);",
		".fastlane-theme-dark .fastlane-routing-choice-selected { border-color:rgba(34, 197, 94, 0.42); background:linear-gradient(180deg, rgba(13, 35, 28, 0.96) 0%, rgba(10, 24, 21, 1) 100%);",
		".fastlane-theme-dark .fastlane-routing-inline > .cbi-input-text, .fastlane-theme-dark .fastlane-routing-inline > .cbi-input-select { border-color:rgba(145, 175, 220, 0.16); background:rgba(6, 12, 22, 0.72); color:#eef4ff;",
		".fastlane-theme-dark .fastlane-routing-selector-shell { display:grid; gap:14px; padding:16px 18px; border:1px solid rgba(145, 175, 220, 0.14); border-radius:18px; background:rgba(8, 15, 26, 0.5);",
		".fastlane-theme-dark .fastlane-routing-item { background:linear-gradient(180deg, rgba(11, 18, 30, 0.94) 0%, rgba(8, 14, 24, 0.98) 100%); border-color:rgba(145, 175, 220, 0.14);",
		".fastlane-theme-dark .fastlane-routing-summary-shell { background:rgba(8, 15, 26, 0.58); border-color:rgba(145, 175, 220, 0.16);",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("routing view missing dark-theme marker %q", want)
		}
	}
}

func TestFirewallViewReRendersAfterRoutingChoiceChange(t *testing.T) {
	t.Parallel()

	source := readFirewallViewSource(t)

	modeStart := strings.Index(source, "handleModeChange: function(ev) {")
	dnsStart := strings.Index(source, "handleDNSChoiceChange: function(ev) {")
	selectorStart := strings.Index(source, "handleSelectorInputChange: function(ev) {")
	if modeStart < 0 || dnsStart < 0 || selectorStart < 0 {
		t.Fatal("routing view missing expected change handlers")
	}

	modeBlock := source[modeStart:dnsStart]
	dnsBlock := source[dnsStart:selectorStart]

	if !strings.Contains(modeBlock, "this.renderIntoRoot();") {
		t.Fatal("handleModeChange must re-render the routing cards")
	}

	if !strings.Contains(dnsBlock, "this.renderIntoRoot();") {
		t.Fatal("handleDNSChoiceChange must re-render the dns cards")
	}
}

func TestFirewallViewKeepsIntroOnThemeColors(t *testing.T) {
	t.Parallel()

	source := readFirewallViewSource(t)

	if strings.Contains(source, "#fastlane-routing-root { --fastlane-routing-ink:#10263f; --fastlane-routing-ink-muted:#44566b; --fastlane-routing-ink-soft:#62758a; --fastlane-routing-panel-bg:linear-gradient(160deg, rgba(243, 248, 255, 0.98) 0%, rgba(230, 239, 249, 0.98) 56%, rgba(220, 232, 245, 0.98) 100%); --fastlane-routing-surface-bg:linear-gradient(180deg, rgba(255, 255, 255, 0.97) 0%, rgba(246, 250, 254, 0.97) 100%); --fastlane-routing-surface-strong:linear-gradient(180deg, #17324d 0%, #10243a 100%); color:var(--fastlane-routing-ink); }") {
		t.Fatal("routing root must not override intro text color")
	}
}

func TestFirewallViewRemovesAdvancedRoutingControls(t *testing.T) {
	t.Parallel()

	source := readFirewallViewSource(t)

	for _, forbidden := range []string{
		"fastlane-firewall-mode",
		"Transparent Port",
		"Block QUIC",
		"Disable IPv6",
		"fastlane-firewall-help",
		"firewall explain",
		"Targets",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("routing view must not contain %q", forbidden)
		}
	}
}

func TestFirewallViewPersistsOnlyOffAndBypassModes(t *testing.T) {
	t.Parallel()

	source := readFirewallViewSource(t)

	for _, want := range []string{
		"'firewall', 'set', 'bypass'",
		"'firewall', 'draft', 'bypass'",
		"'firewall', 'set', 'hosts'",
		"'firewall', 'draft', 'hosts'",
		"'firewall', 'disable'",
		"'dns', 'set', 'mode', 'system'",
		"'dns', 'set', 'default'",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("routing view must contain %q", want)
		}
	}
}

func TestFastLaneLuCIMenuKeepsPrimaryProductSections(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	path := filepath.Join(root, "luci-app-fastlane", "root", "usr", "share", "luci", "menu.d", "luci-app-fastlane.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var payload map[string]struct {
		Order  int    `json:"order"`
		Title  string `json:"title"`
		Action struct {
			Type string `json:"type"`
			Path string `json:"path"`
		} `json:"action"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal menu json: %v", err)
	}

	if len(payload) != 5 {
		t.Fatalf("expected root + 4 Fast Lane entries, got %d", len(payload))
	}

	rootEntry, ok := payload["admin/services/fastlane"]
	if !ok {
		t.Fatal("missing Fast Lane root menu entry")
	}
	if rootEntry.Title != "Fast Lane" {
		t.Fatalf("unexpected root title %q", rootEntry.Title)
	}
	if rootEntry.Action.Path != "admin/services/fastlane/vpn" {
		t.Fatalf("root Fast Lane alias path mismatch: %q", rootEntry.Action.Path)
	}

	vpnEntry, ok := payload["admin/services/fastlane/vpn"]
	if !ok {
		t.Fatal("missing VPN menu entry")
	}
	if vpnEntry.Title != "VPN" || vpnEntry.Order != 10 {
		t.Fatalf("unexpected VPN menu entry: %+v", vpnEntry)
	}
	routingEntry, ok := payload["admin/services/fastlane/routing"]
	if !ok {
		t.Fatal("missing routing menu entry")
	}
	if routingEntry.Title != "Routing" || routingEntry.Order != 20 {
		t.Fatalf("unexpected routing menu entry: %+v", routingEntry)
	}

	diagnosticsEntry, ok := payload["admin/services/fastlane/diagnostics"]
	if !ok {
		t.Fatal("missing diagnostics menu entry")
	}
	if diagnosticsEntry.Title != "Diagnostics" {
		t.Fatalf("unexpected diagnostics title %q", diagnosticsEntry.Title)
	}
	if diagnosticsEntry.Order != 30 {
		t.Fatalf("unexpected diagnostics order %d", diagnosticsEntry.Order)
	}

	settingsEntry, ok := payload["admin/services/fastlane/settings"]
	if !ok {
		t.Fatal("missing settings menu entry")
	}
	if settingsEntry.Title != "Settings" {
		t.Fatalf("unexpected settings title %q", settingsEntry.Title)
	}
	if settingsEntry.Order != 40 {
		t.Fatalf("unexpected settings order %d", settingsEntry.Order)
	}

	for _, forbidden := range []string{
		"admin/services/fastlane/overview",
		"admin/services/fastlane/logs",
		"admin/services/fastlane/subscriptions",
		"admin/services/fastlane/firewall",
		"admin/services/fastlane/zapret",
		"admin/services/fastlane/about",
	} {
		if _, exists := payload[forbidden]; exists {
			t.Fatalf("menu must not keep removed entry %q", forbidden)
		}
	}
}

func readFirewallViewSource(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	path := filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "firewall.js")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}
