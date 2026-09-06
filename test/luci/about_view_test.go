package luci_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAboutViewUsesLatestInstallScriptUpgradeFlow(t *testing.T) {
	t.Parallel()

	source := readAboutViewSource(t)

	for _, want := range []string{
		"Fast Lane - About",
		"Update to new version",
		"/usr/libexec/fastlane-self-update",
		"Existing /etc/fastlane state is preserved by the installer.",
		"function extractSelfUpdateStatus(output)",
		"status !== 'up-to-date'",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("about view missing marker %q", want)
		}
	}

	for _, forbidden := range []string{
		"this.execHelper(fastlaneBinary, [ '--upgrade' ])",
		"this.execText([ '--upgrade' ])",
		"fs.exec('/bin/sh'",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("about view must not keep legacy upgrade flow marker %q", forbidden)
		}
	}
}

func TestAboutUpdateHelperRunsExactInstallCommand(t *testing.T) {
	t.Parallel()

	source := readSelfUpdateHelperSource(t)

	for _, want := range []string{
		"#!/bin/sh",
		"set -eu",
		"\"${FASTLANE_BINARY}\" --upgrade",
		"FASTLANE_SELF_UPDATE_STATUS=up-to-date",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("self-update helper missing marker %q", want)
		}
	}
}

func TestXrayUpdateHelperUsesOfficialUpstreamSource(t *testing.T) {
	t.Parallel()

	source := readXrayUpdateHelperSource(t)

	for _, want := range []string{
		"#!/bin/sh",
		"set -eu",
		"https://api.github.com/repos/XTLS/Xray-core/releases/latest",
		"https://github.com/XTLS/Xray-core/releases/download",
		"Official Xray releases do not publish a soft-float MIPS build.",
		"Xray-linux-64.zip",
		"Xray-linux-arm64-v8a.zip",
		"FASTLANE_XRAY_UPDATE_STATUS=",
		"exit_with_status",
		"Xray is up to date",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("xray update helper missing marker %q", want)
		}
	}
}

func TestFastLaneACLAllowsUpdateHelpers(t *testing.T) {
	t.Parallel()

	source := readACLSource(t)

	if !strings.Contains(source, "\"/usr/libexec/fastlane-self-update\": [ \"exec\" ]") {
		t.Fatal("acl must allow fastlane self-update helper")
	}
	if !strings.Contains(source, "\"/usr/libexec/fastlane-xray-update\": [ \"exec\" ]") {
		t.Fatal("acl must allow xray update helper")
	}

	for _, forbidden := range []string{
		"\"/bin/sh *\": [ \"exec\" ]",
		"\"/bin/sh\": [ \"exec\" ]",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("acl must not allow shell execution marker %q", forbidden)
		}
	}
}

func TestAboutViewFormatsBuildDateAndSimplifiesWhatsNew(t *testing.T) {
	t.Parallel()

	source := readAboutViewSource(t)

	for _, want := range []string{
		"function formatBuildDate(value)",
		"fastlaneUI.renderSummaryCard(_('Build Date'), formattedBuildDate)",
		"var versionText = 'Fast Lane ' + version + '\\nCommit: ' + commit + '\\nBuilt: ' + formattedBuildDate;",
		"Only Selected Devices Mode",
		"Optimized subscriptions and single servers by introducing the Server List",
		"About intentionally keeps destructive maintenance actions out of LuCI.",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("about view missing marker %q", want)
		}
	}

	for _, forbidden := range []string{
		"Subscription expiration date is now shown",
		"Update Fast Lane from LuCI",
		"Bypass mode and target bundles",
		"Update Zapret",
		"Update Xray",
		"Remove Fast Lane",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("about view must not keep old what's new marker %q", forbidden)
		}
	}
}

func TestAboutViewUsesFastLaneButtonsInsteadOfLegacyThemeClasses(t *testing.T) {
	t.Parallel()

	source := readAboutViewSource(t)

	for _, want := range []string{
		"'class': 'cbi-button cbi-button-action'",
		"'class': 'cbi-button cbi-button-apply'",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("about view missing Fast Lane button marker %q", want)
		}
	}

	for _, forbidden := range []string{
		"'class': 'btn cbi-button'",
		"'class': 'btn cbi-button cbi-button-action important'",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("about view must not keep legacy theme class marker %q", forbidden)
		}
	}
}

func readAboutViewSource(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	path := filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "about.js")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}

func readSelfUpdateHelperSource(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	path := filepath.Join(root, "openwrt", "root", "usr", "libexec", "fastlane-self-update")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}

func readXrayUpdateHelperSource(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	path := filepath.Join(root, "openwrt", "root", "usr", "libexec", "fastlane-xray-update")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}

func readACLSource(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	path := filepath.Join(root, "luci-app-fastlane", "root", "usr", "share", "rpcd", "acl.d", "luci-app-fastlane.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}
