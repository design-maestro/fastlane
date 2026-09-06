package luci_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubscriptionsViewKeepsSafeGeneratedXrayPreview(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, forbidden := range []string{
		"handleInspectPreview",
		"showInspectPreviewModal",
		"'inspect', 'xray-safe'",
		"Export JSON",
		"copyTextToClipboard",
		"handleSpeedTest",
		"'inspect', 'speed'",
		"Speed Test",
		"Sort by last availability",
		"fastlane.subscriptions.sort_by_last_availability",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("subscriptions view must not keep advanced action marker %q", forbidden)
		}
	}
}

func TestSubscriptionsViewShowsConditionalExpirationDateAndRemoveAction(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		"Expiration date",
		"if (trim(subscription.expires_at) !== '')",
		"handleRemoveSubscription",
		"handleCopySubscriptionID",
		"Subscription ID copied to clipboard.",
		"fastlane-meta-copy-button",
		"cbi-button-negative",
		"[ _('Remove') ]",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing marker %q", want)
		}
	}
}

func TestSubscriptionsViewShowsRemainingTrafficMeter(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		"Remaining traffic",
		"renderTrafficSummary",
		"fastlane-traffic-meter",
		"fastlane-traffic-meter-fill",
		"Unlimited",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing traffic marker %q", want)
		}
	}
}

func TestSubscriptionsViewShowsCompactStackColumnInNodeTable(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		"formatSecurityLabel",
		"renderNodeStackCell",
		"responsiveTableCell(_('Stack')",
		"E('th', { 'class': 'th' }, [ _('Stack') ])",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing compact stack marker %q", want)
		}
	}
}

func TestSubscriptionsViewUsesStaticNodeTableLayout(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		"overflow-x:visible",
		".fastlane-node-table { width:100%; min-width:0; table-layout:fixed; }",
		"fastlane-node-stack-vertical",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing static node table marker %q", want)
		}
	}
}

func TestSubscriptionsViewUsesDistinctVerticalStackChips(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		"fastlane-node-stack-chip-protocol",
		"fastlane-node-stack-chip-transport",
		"fastlane-node-stack-chip-security",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing vertical stack chip marker %q", want)
		}
	}
}

func TestSubscriptionsViewShowsPingControlsAndStates(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		"Check Ping",
		"Ping",
		"Recheck",
		"Last known",
		"Not checked",
		"fastlane.subscriptions.ping.latest",
		"'inspect', 'ping'",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing ping marker %q", want)
		}
	}
}

func TestSubscriptionsViewPlacesRecheckInActionStack(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		"fastlane-node-action-stack",
		"fastlane-node-actions-secondary",
		"handleRecheckPing",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing action stack marker %q", want)
		}
	}
}

func TestSubscriptionsViewStacksNodeActionsOnSmartphones(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		".fastlane-ping-actions, .fastlane-node-actions { flex-direction:column;",
		".fastlane-ping-actions .cbi-button, .fastlane-node-actions .cbi-button { width:100%; }",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing smartphone node action marker %q", want)
		}
	}
}

func TestSubscriptionsViewResetsNodeColumnWidthsOnSmartphones(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		".fastlane-node-table .fastlane-node-row > .td { width:100%; min-width:0;",
		".fastlane-node-table .fastlane-node-row > .td::before { content:attr(data-title);",
		"white-space:nowrap;",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing smartphone node width reset marker %q", want)
		}
	}
}

func TestSubscriptionsViewCentersNodeCardsOnSmartphones(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		".fastlane-node-table .fastlane-node-row { margin-bottom:12px; padding:12px 14px;",
		"text-align:center;",
		".fastlane-node-table .fastlane-node-row > .td::before { content:attr(data-title); display:block;",
		".fastlane-node-stack, .fastlane-node-stack-vertical { justify-items:center; }",
		".fastlane-ping-cell, .fastlane-node-action-stack { justify-items:center; }",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing smartphone centering marker %q", want)
		}
	}
}

func TestSubscriptionsViewCentersHeroCardsAndSummaryBlocksOnSmartphones(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		".fastlane-subscriptions-hero, .fastlane-subscription-card, .fastlane-provider-group-header, .fastlane-auto-exclusions, .fastlane-node-details summary { text-align:center; }",
		".fastlane-overview-grid { justify-items:center; }",
		".fastlane-overview-grid .fastlane-card { width:100%; text-align:center; }",
		".fastlane-overview-grid .fastlane-card-accent, .fastlane-overview-grid .fastlane-card-label, .fastlane-overview-grid .fastlane-card-value { text-align:center; justify-self:center; margin-left:auto; margin-right:auto; }",
		".fastlane-page-hero-meta, .fastlane-subscription-controls { justify-items:center; }",
		".fastlane-page-hero-meta-item, .fastlane-page-hero-meta-label, .fastlane-page-hero-meta-value, .fastlane-action-status-group { text-align:center; justify-self:center; }",
		".fastlane-subscription-badges, .fastlane-node-status-badges, .fastlane-auto-exclusions-list, .fastlane-subscription-actions, .fastlane-ping-actions, .fastlane-node-actions { justify-content:center; }",
		".fastlane-traffic-meter, .fastlane-node-action-stack, .fastlane-node-heading-actions-label { margin-left:auto; margin-right:auto; }",
		".fastlane-node-heading-actions, .fastlane-node-cell-actions { text-align:center; padding-right:0; }",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing smartphone symmetry marker %q", want)
		}
	}
}

func TestSubscriptionsViewCentersSubscriptionMetaTableOnMobile(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		".fastlane-meta-table .tr { padding:10px 0; border-top:1px solid rgba(145, 175, 220, 0.1); text-align:center; }",
		".fastlane-subscription-card .fastlane-meta-table, .fastlane-subscription-card .fastlane-meta-table .tr, .fastlane-subscription-card .fastlane-meta-table .td, .fastlane-subscription-card .fastlane-meta-table .td.left { text-align:center !important; }",
		".fastlane-meta-table .td.fastlane-meta-label, .fastlane-subscription-card .fastlane-meta-table .td.fastlane-meta-label.left { width:100%; padding-bottom:4px; text-align:center !important; }",
		".fastlane-meta-table .td.fastlane-meta-value, .fastlane-subscription-card .fastlane-meta-table .td.fastlane-meta-value.left { padding-top:0; text-align:center !important; }",
		".fastlane-meta-copy-shell { display:flex; flex-direction:column; align-items:center; justify-content:center; gap:10px; width:100%; }",
		".fastlane-meta-copy-value { width:auto; max-width:100%; text-align:center !important; margin:0 auto; }",
		".fastlane-meta-copy-button { align-self:center; margin:0 auto; }",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing centered mobile meta-table marker %q", want)
		}
	}
}

func TestSubscriptionsViewKeepsCopyButtonCloseToIDOnDesktop(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		".fastlane-meta-copy-shell { display:inline-flex; align-items:center; justify-content:flex-start; gap:8px; min-width:0; width:auto; max-width:100%; }",
		".fastlane-meta-copy-value { min-width:0; flex:0 1 auto; overflow-wrap:anywhere; word-break:break-word; }",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing desktop copy alignment marker %q", want)
		}
	}
}

func TestSubscriptionsViewUsesSofterLightAccentsAndReadablePingState(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		".fastlane-theme-light .fastlane-ping-primary-live { color:#0f766e; }",
		".fastlane-theme-light .fastlane-ping-primary-down { color:#b91c1c; }",
		".fastlane-theme-light .fastlane-ping-primary-seed { color:#475569; }",
		".fastlane-theme-light .fastlane-ping-status-group { color:#1d4ed8; }",
		".fastlane-theme-light .fastlane-add-kicker { background:rgba(37, 99, 235, 0.08); color:#1d4ed8; }",
		".fastlane-theme-light .fastlane-add-field-shell { border-color:rgba(125, 146, 170, 0.18); background:linear-gradient(180deg, rgba(250, 252, 254, 0.98) 0%, rgba(243, 247, 251, 0.98) 100%);",
		".fastlane-theme-light .fastlane-subscription-badges .label.notice, .fastlane-theme-light .fastlane-node-active-badge .label.notice { border-color:rgba(22, 163, 74, 0.22); background:rgba(22, 163, 74, 0.1); color:#166534; }",
		".fastlane-theme-light .fastlane-provider-group-header { padding:12px 14px; border:1px solid rgba(125, 146, 170, 0.14); border-radius:16px; background:linear-gradient(180deg, rgba(250, 252, 254, 0.96) 0%, rgba(243, 247, 251, 0.96) 100%);",
		".fastlane-theme-light .fastlane-provider-group-title { color:#162638; }",
		".fastlane-theme-light .fastlane-provider-group-meta { color:#52667c; }",
		".fastlane-theme-light .fastlane-node-table { background:rgba(249, 251, 253, 0.92); border-color:rgba(125, 146, 170, 0.18); }",
		".fastlane-theme-light .fastlane-node-table .th { background:rgba(125, 146, 170, 0.08); color:#5c7085; }",
		".fastlane-theme-light .fastlane-subscription-actions .cbi-button-action, .fastlane-theme-light .fastlane-node-actions .cbi-button-action { border-color:rgba(37, 99, 235, 0.18); background:linear-gradient(180deg, rgba(243, 248, 253, 0.98) 0%, rgba(232, 240, 248, 0.98) 100%); color:#17324b;",
		".fastlane-theme-light .fastlane-subscription-actions .cbi-button-apply { border-color:rgba(37, 99, 235, 0.34); background:linear-gradient(180deg, #2563eb 0%, #1d4ed8 100%); color:#f8fbff;",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing soft light marker %q", want)
		}
	}
}

func TestSubscriptionsViewSortsNodesByActiveThenPingLatency(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		"nodePingSortMeta: function(subscriptionId, nodeId, status)",
		"compareNodeTableEntries: function(left, right)",
		"sortedEntries = nodes.map(L.bind(function(node, index)",
		"sortedEntries.sort(L.bind(this.compareNodeTableEntries, this));",
		"'ping_sort_bucket': pingSort.bucket",
		"'ping_latency_ms': pingSort.latency_ms",
		"'original_index': index",
		"return left.original_index - right.original_index;",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing node sorting marker %q", want)
		}
	}
}

func TestSubscriptionsViewSupportsAutoExcludedNodes(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		"autoExcludedNodeKey: function(subscriptionId, nodeId)",
		"isNodeAutoExcluded: function(status, subscriptionId, nodeId)",
		"handleToggleAutoExcluded: function(subscriptionId, nodeId, shouldExclude, ev)",
		"'settings', 'set', 'auto.excluded-nodes'",
		"Auto exclusions",
		"Auto mode skips these nodes when selecting the best route.",
		"Exclude",
		"Allow in Auto",
		"Auto excluded",
		"fastlane-auto-exclusions",
		"fastlane-node-auto-badge",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing auto exclusion marker %q", want)
		}
	}
}

func TestSubscriptionsViewKeepsOverviewSummaryAndCoreActions(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		"Fast Lane - Subscriptions",
		"Fast Lane status, the active connection, and the basic subscription actions you need every day.",
		"Refresh Active",
		"Disconnect",
		"Active Provider",
		"Active Profile",
		"Active Node",
		"handleDisconnect",
		"handleRefreshActive",
		"handleConnectAuto",
		"handleConnectNode",
		"handleAdd",
		"handleRefreshSubscription",
		"handleRemoveSubscription",
		"handleRemoveAll",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing summary/core action marker %q", want)
		}
	}
}

func TestSubscriptionsViewUsesStyledAddSubscriptionPanel(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		"fastlane-add-panel",
		"fastlane-add-panel-head",
		"fastlane-add-kicker",
		"fastlane-add-field-shell",
		"fastlane-add-format-list",
		"fastlane-add-format-badge",
		"Accepted input",
		"http(s) URL",
		"VLESS / VMess / Trojan / SS",
		"base64 payload",
		"Xray / 3x-ui JSON",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing add panel marker %q", want)
		}
	}
}

func TestSubscriptionsViewUsesFlagshipDarkShellMarkers(t *testing.T) {
	t.Parallel()

	source := readSubscriptionsViewSource(t)

	for _, want := range []string{
		"fastlane-page-shell fastlane-page-shell-subscriptions",
		"fastlane-page-hero",
		"fastlane-page-hero-actions",
		"fastlane-surface",
		"fastlane-data-table",
		"fastlane-section-heading",
		"fastlane-provider-group-header",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("subscriptions view missing flagship dark shell marker %q", want)
		}
	}
}

func readSubscriptionsViewSource(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	path := filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "subscriptions.js")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}
