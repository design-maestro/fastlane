package luci_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFastLaneRoutingViewHasCountryNeutralPreset(t *testing.T) {
	t.Parallel()
	source := readRoutingViewSource(t)
	for _, want := range []string{
		"fastlaneShell.renderHeader('routing')",
		"Local-country traffic directly",
		"settings', 'patch'",
		"country_direct: enable",
		"direct_country: this.countryCode",
		"countries.options(countryCode)",
		"if (enable && !this.geodata.ready)",
		"fs.exec(geodataHelper, [ 'start' ])",
		"fs.exec(geodataHelper, [ 'status' ])",
		"waitForGeoUpdate",
		"Databases will be installed automatically when routing is first enabled",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("routing view missing one-step preset marker %q", want)
		}
	}
	if strings.Contains(source, "fs.exec(geodataHelper, [ 'update' ])") {
		t.Fatal("routing view must not hold LuCI RPC open for the long geodata update")
	}
}

func TestFastLaneRoutingCountrySelectMatchesPrimaryInputs(t *testing.T) {
	t.Parallel()
	source := readRoutingViewSource(t)
	for _, want := range []string{
		".flr-select{display:block;width:100%;height:52px!important;min-height:52px!important",
		"padding:0 42px 0 16px!important",
		"border-radius:11px!important",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("routing country select missing stable field sizing %q", want)
		}
	}
}

func TestFastLaneRoutingHasOneVisibleOnOffState(t *testing.T) {
	t.Parallel()
	source := readRoutingViewSource(t)
	if strings.Contains(source, "flr-state") {
		t.Fatal("routing header must not duplicate the country-routing toggle state")
	}
}

func TestFastLaneRoutingReusesSharedDialogFields(t *testing.T) {
	t.Parallel()
	source := readRoutingViewSource(t)
	for _, want := range []string{
		"class: 'flr-rule-form fl-dialog-form'",
		"class: 'flr-form-field fl-dialog-field'",
		"class: 'flr-form-label fl-dialog-label'",
		"class: 'flr-form-help fl-dialog-help'",
		"class: 'right fl-dialog-actions'",
		"modal.classList.add('fl-dialog')",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("routing modal missing shared dialog marker %q", want)
		}
	}
}

func TestFastLaneRoutingViewReadsHAPPLinksWithoutPartialApply(t *testing.T) {
	t.Parallel()
	source := readRoutingViewSource(t)
	for _, want := range []string{
		"happ://routing/onadd/",
		"function decodeHappLink(raw)",
		"Partial application is disabled",
		"DirectSites", "ProxySites", "BlockSites",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("routing view missing safe HAPP import marker %q", want)
		}
	}
}

func TestFastLaneRoutingViewManagesNamedDirectExclusions(t *testing.T) {
	t.Parallel()
	source := readRoutingViewSource(t)
	for _, want := range []string{
		"services', 'list'",
		"services', 'set'",
		"services', 'delete'",
		"firewall', 'set', 'bypass'",
		"--exclude-host",
		"renderBypassRules",
		"Direct access exclusions",
		"Domains",
		"IP addresses and networks",
		"handleRuleToggle",
		"handleRuleDelete",
		"fastlane-routing-modal",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("routing view missing direct-exclusion control %q", want)
		}
	}
}

func readRoutingViewSource(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	path := filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "routing-20260906-v4.js")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
