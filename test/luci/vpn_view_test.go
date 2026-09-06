package luci_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFastLaneVPNViewHasOneMergedServerTable(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"fastlaneShell.renderHeader('vpn')", "visibleRows: function()",
		"this.filter === 'all'", "Source", "fl-mode-switch", "handleConnect",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN view missing merged-list marker %q", want)
		}
	}
}

func TestFastLaneVPNViewKeepsFilteringVisualAndAutoGlobal(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"'health-check', '--subscription', 'all'", "rowAttrs.click", "rowAttrs.keydown",
		"Source", "subscriptionErrors", "Retry",
		"state.active_subscription_id", "aria-live", "formatLatency", "HTTPS",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN view missing truthful interaction marker %q", want)
		}
	}
	if strings.Contains(source, "args.push('--subscription', this.filter)") {
		t.Fatal("source filter must not narrow global auto selection")
	}
	if strings.Contains(source, "fl-info-card") && strings.Contains(source, "Клик «Подключить»") {
		t.Fatal("VPN view must not keep explanatory help cards below the server table")
	}
	if strings.Contains(source, "document.body.classList.add('fastlane-page-dark')") {
		t.Fatal("VPN view must not leave a global page theme class behind")
	}
}

func TestFastLaneVPNDoesNotDuplicateRoutingSummary(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, forbidden := range []string{"renderRouting", "class: 'fl-route", "LAN не перехватывается", "Обновить базы", "Установить базы"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("VPN view must leave routing status and actions to the Routes tab: %q", forbidden)
		}
	}
}

func TestFastLaneVPNViewReadsPartialBackgroundGETResults(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"mergeBackgroundPings", "progress.results", "background.done", "background.total",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN view missing router-side GET progress marker %q", want)
		}
	}
}

func TestFastLaneVPNViewUsesHTTPSGETForAllLatencyMeasurements(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"refresh', '--all", "refresh', '--subscription", "inspect', 'url-test",
		"fastlane.vpn.get.results.v1", "Ping (GET)", "Add servers",
		"Update subscriptions", "Check ping (GET)", "handleRefreshSubscriptions",
		"Remove subscription", "handleRemove', selected.id",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN view missing action marker %q", want)
		}
	}
	for _, forbidden := range []string{"inspect', 'ping", "TCP-пинг", "|| health[node.id]"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("VPN view must not expose TCP latency marker %q", forbidden)
		}
	}
}

func TestFastLaneVPNDelegatesBulkGETToTheRouter(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"'inspect', 'health-check'", "'inspect', 'health-check-status'", "startPolling: function()",
		"poll.add(this.pollFn, vpnPollInterval)", "visibilitychange", "You can close the page",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN view missing asynchronous router-side GET marker %q", want)
		}
	}
	for _, forbidden := range []string{"function runNext()", "Promise.all(workers)", "Math.min(5, jobs.length)"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("VPN view must not run bulk GET work in the browser: %q", forbidden)
		}
	}
}

func TestFastLaneVPNDoesNotRenderFailedGETAsZeroMilliseconds(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"function positiveLatency(value)",
		"number > 0",
		"value.consecutive_failures",
		"value.last_failure_reason",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("failed or missing GET latency guard missing %q", want)
		}
	}
	if strings.Contains(source, "latency = durationMilliseconds(value.average_latency)") {
		t.Fatal("the latest GET result must not be revived from historical average latency")
	}
}

func TestFastLaneVPNMergesPersistedHealthAfterReload(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"mergePersistedPings", "state.health", "persistedObservation",
		"last_checked_at", "fresherObservation", "durationMilliseconds",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN view missing persisted health merge marker %q", want)
		}
	}
}

func TestFastLaneVPNViewCanHideAndRestoreNodes(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"auto.excluded-nodes", "handleHidden", "Hidden", "Hide", "Restore",
		"setLocalHiddenNodeKeys", "var previous = this.hiddenNodeKeys()",
		"Server hidden and excluded from automatic selection.",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN view missing hidden-node marker %q", want)
		}
	}
}

func TestFastLaneVPNServerActionsUseVisibleThreeDotControl(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		".fl-more summary:before{content:\"\";width:4px;height:4px",
		"box-shadow:0 -7px 0 currentColor,0 7px 0 currentColor",
		"justify-content:flex-start!important",
		"text-align:left!important",
		"class: 'fl-actions-cell'",
		"'aria-label': _('Server actions')",
		"handleServerMenuToggle",
		"this.activeMenuKey === actionKey ? 'open' : null",
		"handleDocumentClick",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN view missing visible server action control marker %q", want)
		}
	}
}

func TestFastLaneVPNSourcesScrollWithoutMovingAddAction(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		".fl-sourcebar{display:flex;align-items:stretch;min-height:90px;margin-bottom:16px;border:1px solid var(--fl-line);border-radius:8px;background:#050d10;overflow:hidden}",
		".fl-tabs{display:flex;flex:1 1 auto;min-width:0;overflow-x:auto",
		".fl-source-actions{position:relative;z-index:2;display:flex;flex:0 0 auto",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN source strip missing independent-scroll marker %q", want)
		}
	}
}

func TestFastLaneVPNStacksFixedSubscriptionActions(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		".fl-source-actions{flex-direction:column}",
		".fl-source-actions .fl-button+.fl-button{border-top:1px solid var(--fl-line)}",
		"E('button', { class: 'fl-button fl-source-add'",
		"selected ? E('button', { class: 'fl-button fl-button-danger'",
		"if (this.showHidden) return null;",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN source actions missing stacked-group marker %q", want)
		}
	}
	addIndex := strings.Index(source, "E('button', { class: 'fl-button fl-source-add'")
	removeIndex := strings.Index(source, "selected ? E('button', { class: 'fl-button fl-button-danger'")
	if addIndex < 0 || removeIndex < 0 || addIndex > removeIndex {
		t.Fatal("add subscription action must render above the conditional remove action")
	}
}

func TestFastLaneVPNOnlyShowsCombinedPoolForMultipleSubscriptions(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"if (availableSubscriptions.length > 1)",
		"available.length === 1 && this.filter === 'all'",
		"selectedSubscription: function()",
		"var selected = this.selectedSubscription()",
		"this.poolSubscriptions().length > 1 && this.filter === 'all'",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN source tabs missing single-subscription behavior marker %q", want)
		}
	}
}

func TestFastLaneVPNTableOverridesFriendlyWrtStripeColors(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		".fastlane-root .fl-table tbody tr:nth-of-type(2n)",
		"background:#071115!important",
		"border-top:0!important",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN table missing theme isolation marker %q", want)
		}
	}
}

func TestFastLaneVPNUsesThreeEqualMobileColumnsWithoutSource(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"class: 'fl-table ' + (all ? 'fl-table-all' : 'fl-table-single')",
		".fl-table-single tbody tr{grid-template-columns:repeat(3,minmax(0,1fr))}",
		".fl-table-single .fl-meta-status{grid-column:3}",
		".fastlane-root .fl-table td{border-bottom:0!important}",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN view missing equal mobile metadata columns marker %q", want)
		}
	}
}

func TestFastLaneVPNMovesCountryEmojiIntoFlagMarker(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"function flagEmoji(value)",
		"function flagEmojiFromCode(code)",
		"replace(/(?:\\uD83C[\\uDDE6-\\uDDFF]){2}/g, '')",
		"fl-server-flag-emoji",
		"fl-server-flag-glyph",
		".fastlane-root .fl-server-flag-glyph{transform:translateY(2px)}",
		"emoji = flagEmoji(nodeRawName(row.node)) || flagEmojiFromCode(nodeFlag(row.node))",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN view missing country emoji normalization marker %q", want)
		}
	}
	if strings.Contains(source, "asset('flag-' + flag + '.png')") {
		t.Fatal("VPN view still mixes raster country flags with emoji markers")
	}
}

func TestFastLaneVPNProtectsLineIconsFromLuCITheme(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"document.createElementNS(namespace, 'svg')",
		"document.createElementNS(namespace, 'path')",
		"path.setAttribute('stroke', 'currentColor')",
		"plus: 'M12 5v14M5 12h14'",
		"trash: 'M4 7h16",
		"[ icon('plus'), _('Add servers') ]",
		"selected.source_type === 'file' ? _('Remove file') : _('Remove subscription')",
		".fastlane-root .fl-icon path{fill:none!important;stroke:currentColor!important}",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN view missing real SVG icon marker %q", want)
		}
	}
}

func TestFastLaneVPNUsesBrandedResponsiveSubscriptionModal(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"body:has(.fastlane-modal) #modal_overlay",
		"width:min(600px,calc(100vw - 32px))!important",
		"class: 'fl-add-field'",
		"class: 'fl-modal-button fl-modal-primary'",
		"class: 'fl-modal-status', role: 'status', 'aria-live': 'polite'",
		"window.requestAnimationFrame(function() { source.focus(); })",
		"submitButton.textContent = _('Adding…')",
		"@media(max-width:760px){body:has(.fastlane-modal) #modal_overlay",
		"body:not(.modal-overlay-active) #modal_overlay:has(.fastlane-modal){display:none!important}",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN view missing branded subscription modal marker %q", want)
		}
	}
	for _, forbidden := range []string{
		"class: 'btn cbi-button-positive'",
		"class: 'fl-modal-error'",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("VPN view still contains legacy modal marker %q", forbidden)
		}
	}
}

func TestFastLaneVPNShowsProviderSubscriptionExpiry(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"function subscriptionExpiryPresentation(value)",
		"subscriptionExpiryPresentation(sub.expires_at)",
		"Expires tomorrow",
		"Expires in",
		"Expired",
		"fl-tab-expiry-soon",
		"fl-tab-expiry-expired",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN view missing subscription expiry marker %q", want)
		}
	}
}

func TestFastLaneVPNBackgroundLoaderHasTextSpacing(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	if !strings.Contains(source, ".fl-busy{display:flex;align-items:center;gap:10px") {
		t.Fatal("background progress loader must keep a stable gap from its text")
	}
}

func readVPNViewSource(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	path := filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "vpn-20260905-latency-v18.js")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
