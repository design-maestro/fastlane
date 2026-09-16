package luci_test

import (
	"strings"
	"testing"
)

func TestFastLaneVPNViewUsesAWGCLIContract(t *testing.T) {
	t.Parallel()
	source := readVPNViewSource(t)
	for _, want := range []string{
		"this.execJSON([ '--json', 'awg', 'status' ])",
		"fs.exec('/usr/libexec/fastlane-awg-import-prepare', [])",
		"fs.write(importPath, content)",
		"[ 'awg', 'import', '--file', importPath ]",
		"[ 'awg', 'connect' ]",
		"[ 'awg', 'check' ]",
		"[ 'awg', 'disconnect' ]",
		"[ 'awg', 'remove' ]",
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
		"You can import only one AmneziaWG profile at a time.",
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
		"id: 'amneziawg'",
		"source_type: 'file'",
		"kind: 'awg'",
		"display_name: _('AWG file')",
		"Experimental. It does not participate in automatic selection.",
		"selected && selected.id === 'amneziawg'",
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
