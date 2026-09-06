package luci_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFastLaneSettingsExposeOperationalControls(t *testing.T) {
	t.Parallel()
	source := readSettingsViewSource(t)
	for _, want := range []string{
		"Fast Lane settings", "refresh_interval", "health_check_interval",
		"url_test_url", "url_test_timeout", "switch_cooldown",
		"latency_threshold", "strict_egress_check", "min-height:44px",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("settings view missing marker %q", want)
		}
	}
}

func TestFastLaneSettingsAreDarkOnly(t *testing.T) {
	t.Parallel()
	source := readSettingsViewSource(t)
	for _, forbidden := range []string{"fastlaneUI.setThemePreference", "value: 'light'", "Светлая"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("dark-only MVP must not expose %q", forbidden)
		}
	}
}

func TestFastLaneSettingsDoesNotDuplicateRoutingControls(t *testing.T) {
	t.Parallel()
	source := readSettingsViewSource(t)
	for _, forbidden := range []string{"russia_direct", "fastlane-geodata", "Маршрутизация России"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("settings view must leave routing control to the Routes tab: %q", forbidden)
		}
	}
}

func TestFastLaneSettingsCanOverrideTheLuCILanguage(t *testing.T) {
	t.Parallel()
	source := readSettingsViewSource(t)
	for _, want := range []string{
		"require uci",
		"uci.load('luci')",
		"uci.get('luci', 'main', 'lang')",
		"uci.set('luci', 'main', 'lang', language)",
		"uci.save().then(function() { return uci.apply(); })",
		"value: 'auto'",
		"value: 'en'",
		"value: 'ru'",
		"Interface language",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("settings view missing language control marker %q", want)
		}
	}
}

func TestFastLaneSettingsHasOneExplicitSaveAction(t *testing.T) {
	t.Parallel()
	source := readSettingsViewSource(t)
	if strings.Count(source, "[ _('Save') ]") != 1 {
		t.Fatalf("settings view must render exactly one Save button")
	}
	for _, want := range []string{"handleSaveSettings", "handleSave: null"} {
		if !strings.Contains(source, want) {
			t.Fatalf("settings view missing single-save marker %q", want)
		}
	}
	if !strings.Contains(source, "'settings', 'patch', JSON.stringify(patch)") {
		t.Fatal("settings save must use one atomic backend patch")
	}
	if strings.Contains(source, "'settings', 'set', pair[1]") {
		t.Fatal("settings save must not issue a partial sequence of setting writes")
	}
	for _, want := range []string{"this.setSaving(true)", "this.setSaving(false)", "this.syncSettingsControls()", "querySelectorAll('input, button')"} {
		if !strings.Contains(source, want) {
			t.Fatalf("settings save must prevent edits from being lost while the atomic request runs; missing %q", want)
		}
	}
}

func TestFastLaneSettingsAlignsStrictCheckWithItsHeading(t *testing.T) {
	t.Parallel()
	source := readSettingsViewSource(t)
	for _, want := range []string{
		"class: 'fls-field fls-field-toggle'",
		".fls-field-toggle{align-items:center}",
		".fls-field-toggle>.fls-toggle{min-height:22px;padding-top:0}",
		"display:inline-flex",
		"white-space:nowrap",
		"position:static!important;top:auto!important;left:auto!important;width:22px!important;height:22px!important",
		".fls-toggle span{display:block;line-height:22px}",
		"data-toggle-label",
		"label.textContent = this.draft[key] ? _('On') : _('Off')",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("settings view missing strict-check alignment marker %q", want)
		}
	}
}

func TestFastLaneSettingsKeepsDurationUnitsFixedAndOnlyDigitsEditable(t *testing.T) {
	t.Parallel()
	source := readSettingsViewSource(t)
	for _, want := range []string{
		"function durationParts(value, units)",
		"function durationValue(parts, units)",
		"function normalizeDuration(value)",
		"handleDurationInput: function(key, units, unit, ev)",
		"String(input.value).replace(/\\D/g, '')",
		"class: 'fls-duration-unit', 'aria-hidden': 'true'",
		"inputmode: 'numeric', pattern: '[0-9]*'",
		"normalizeDuration(this.draft[key])",
		"[ 'h', 'm', 's' ]",
		"[ 'm', 's' ]",
		"[ 'ms' ]",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("settings view missing fixed-duration marker %q", want)
		}
	}
}

func TestFastLaneSettingsReadCommandsAreAllowed(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	path := filepath.Join(root, "luci-app-fastlane", "root", "usr", "share", "rpcd", "acl.d", "luci-app-fastlane.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	source := string(data)
	for _, want := range []string{
		"\"/usr/bin/fastlane --json settings get\": [ \"exec\" ]",
		"\"/usr/libexec/fastlane-geodata status\": [ \"exec\" ]",
		"\"uci\": [ \"luci\" ]",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("settings ACL missing marker %q", want)
		}
	}
	if strings.Count(source, `"uci": [ "luci" ]`) != 2 {
		t.Fatal("settings ACL must grant LuCI language read and write access")
	}
}

func TestFastLaneSettingsExposeSafeAppRemovalAndFriendlyWrtPackageManager(t *testing.T) {
	t.Parallel()
	source := readSettingsViewSource(t)
	for _, want := range []string{
		"Application management",
		"FriendlyWrt package manager",
		"admin/system/package-manager",
		"Remove Fast Lane",
		"window.confirm(_('Remove Fast Lane from the router? Settings and subscriptions will also be removed.'))",
		"fs.exec(uninstaller, [ '--confirm' ])",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("settings view missing install-management marker %q", want)
		}
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	aclPath := filepath.Join(root, "luci-app-fastlane", "root", "usr", "share", "rpcd", "acl.d", "luci-app-fastlane.json")
	acl, err := os.ReadFile(aclPath)
	if err != nil {
		t.Fatalf("read settings ACL: %v", err)
	}
	if !strings.Contains(string(acl), `"/usr/libexec/fastlane-uninstall --confirm": [ "exec" ]`) {
		t.Fatal("settings ACL must allow only the confirmed uninstaller command")
	}
}

func readSettingsViewSource(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	path := filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "settings-20260905-updates-v6.js")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
