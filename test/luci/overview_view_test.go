package luci_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOverviewViewShowsActivePingCard(t *testing.T) {
	t.Parallel()

	source := readOverviewViewSource(t)

	for _, want := range []string{
		"Active Ping",
		"Not checked",
		"Last known",
		"fastlane.overview.ping.latest",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("overview view missing ping marker %q", want)
		}
	}
}

func TestOverviewViewUsesFlagshipShellMarkers(t *testing.T) {
	t.Parallel()

	source := readOverviewViewSource(t)

	for _, want := range []string{
		"fastlane-page-shell fastlane-page-shell-overview",
		"fastlane-page-hero",
		"fastlane-page-hero-actions",
		"fastlane-surface",
		"fastlane-data-table",
		"fastlane-section-heading",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("overview view missing flagship shell marker %q", want)
		}
	}
}

func readOverviewViewSource(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	path := filepath.Join(root, "luci-app-fastlane", "htdocs", "luci-static", "resources", "view", "fastlane", "overview.js")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}
