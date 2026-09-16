package luci_test

import (
	"strings"
	"testing"
)

func TestFastLaneVPNViewUsesAWGCLIContract(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"this.execJSON([ '--json', 'awg', 'list' ])",
		"fs.exec('/usr/libexec/fastlane-awg-import-prepare', [])",
		"fs.write(importPath, content)",
		"[ 'awg', 'import', '--file', importPath ]",
		"[ 'awg', 'connect', '--id', profileID ]",
		"[ 'awg', 'check', '--id', profileID ]",
		"[ 'awg', 'disconnect' ]",
		"[ 'awg', 'remove', '--id', profileID ]",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN view missing AmneziaWG CLI contract marker %q", want)
		}
	}
}

func TestFastLaneVPNViewImportsAWGConfThroughCommonFilePicker(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"accept: '.yaml,.yml,.json,.txt,.conf,application/x-yaml,text/yaml,text/plain'",
		"Choose configuration files",
		"/\\.conf$/i.test(file.name || '')",
		"self.importAWGFileContent(file.name, content)",
		"selected.forEach(function(file, index)",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN view missing common AWG file import marker %q", want)
		}
	}
}

func TestFastLaneVPNViewRendersAllAWGStatesAndActions(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"state === 'invalid'", "state === 'error'", "state === 'preparing'",
		"state === 'connected'", "state === 'probe_failed'", "return 'direct'",
		"No profile imported", "Import profile", "Replace profile",
		"handleAWGConnect", "handleAWGCheck", "handleAWGDisconnect", "handleAWGRemove",
		"awgImportError(err)", "lifecycle hooks are forbidden",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN view missing AmneziaWG state or action marker %q", want)
		}
	}
}

func TestFastLaneVPNViewOnlyRendersSafeAWGProfileFields(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"profile.name", "profile.protocol", "profile.endpoint", "profile.interface_name", "profile.address",
		"sourceInput.value = ''", "Private keys and raw profile contents are never shown on this page.",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN view missing safe AmneziaWG presentation marker %q", want)
		}
	}
	for _, forbidden := range []string{
		"profile.private_key", "profile.privateKey", "profile.raw", "status.raw_profile",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("VPN view must not render sensitive AmneziaWG field %q", forbidden)
		}
	}
}

func TestFastLaneVPNViewKeepsAWGInCommonServerList(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"id: 'server-list'",
		"source_type: 'raw'",
		"kind: 'awg'",
		"display_name: 'Server List'",
		"Participates in shared GET checks and automatic selection.",
		"this.isManuallyHidden(sub.id, node.id)",
		"E('section', { class: 'fl-server-panel'",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("VPN view missing common-list AmneziaWG marker %q", want)
		}
	}
	if strings.Contains(source, "this.renderAWGCard()") {
		t.Fatal("VPN view still renders the redundant standalone AmneziaWG card")
	}
}
