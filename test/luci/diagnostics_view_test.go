package luci_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticsViewUsesFastLaneOperationalLayout(t *testing.T) {
	t.Parallel()
	source := readDiagnosticsViewSource(t)
	for _, want := range []string{
		"Current Fast Lane and connection status.",
		"fld-overview", "VPN service", "Subscription", "DNS", "Mode",
		"Technical details", "fastlane-diagnostics-advanced",
		"Internet traffic is direct.",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("diagnostics view missing marker %q", want)
		}
	}
}

func TestDiagnosticsViewHidesLegacyImplementationDetails(t *testing.T) {
	t.Parallel()
	source := readDiagnosticsViewSource(t)
	for _, forbidden := range []string{
		"Zapret and backend details", "ZAPRET SERVICE", "Low-level runtime notes",
		"Fast Lane - Diagnostics", "background:linear-gradient",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("diagnostics view must not contain legacy marker %q", forbidden)
		}
	}
}

func TestDiagnosticsViewUsesToastForTechnicalErrors(t *testing.T) {
	t.Parallel()
	source := readDiagnosticsViewSource(t)
	for _, want := range []string{
		"fastlaneShell.showToast(", "The last connection failed.",
		"diagnosticsResult.error.message", "this.problemNotified = true",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("diagnostics view missing error marker %q", want)
		}
	}
	if strings.Contains(source, ") : null") {
		t.Fatal("diagnostics view must not render null as visible text")
	}
}

func TestDiagnosticsViewDoesNotRenderZeroTimestamps(t *testing.T) {
	t.Parallel()
	source := readDiagnosticsViewSource(t)
	if !strings.Contains(source, "0001-01-01T00:00:00Z") {
		t.Fatal("diagnostics view must guard Go zero timestamps")
	}
}

func TestDiagnosticsViewUsesActiveNodeDisplayName(t *testing.T) {
	t.Parallel()
	source := readDiagnosticsViewSource(t)
	start := strings.Index(source, "function activeNodeDisplayName(node, fallbackID)")
	if start < 0 {
		t.Fatal("diagnostics view must define the active-node display-name fallback")
	}
	end := strings.Index(source[start:], "\n}\n")
	if end < 0 {
		t.Fatal("active-node display-name fallback has no function boundary")
	}
	body := source[start : start+end]
	previous := -1
	for _, marker := range []string{
		"node && node.name",
		"node && node.remark",
		"node && node.address",
		"node && node.id",
		"\t\tfallbackID\n\t];",
	} {
		position := strings.Index(body, marker)
		if position <= previous {
			t.Fatalf("active-node display fallback missing or out of order at %q", marker)
		}
		previous = position
	}
	if !strings.Contains(source, "activeNodeDisplayName(status.active_node, state.active_node_id)") ||
		!strings.Contains(source, "_('Server:') + ' ' + activeNode") {
		t.Fatal("diagnostics row must render status.active_node through the display-name fallback")
	}
	if strings.Contains(source, "_('Server:') + ' ' + state.active_node_id") {
		t.Fatal("diagnostics row must not expose the raw active node id when display data is available")
	}
}

func readDiagnosticsViewSource(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	path := filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "diagnostics-20260904-v3.js")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
