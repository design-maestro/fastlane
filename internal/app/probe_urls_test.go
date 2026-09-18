package app

import (
	"reflect"
	"strings"
	"testing"

	"github.com/design-maestro/fastlane/internal/domain"
)

func TestProbeURLSettingsAtomicAndDoNotChangeConnection(t *testing.T) {
	store := &urlContractStore{settings: domain.DefaultSettings(), state: domain.DefaultRuntimeState()}
	service := NewService(Dependencies{Store: store})
	state := store.state
	settings, err := service.PatchSettings(map[string]string{"url-test-url": "https://first.example/204", "url-test-fallback-url": "https://second.example/204"})
	if err != nil {
		t.Fatal(err)
	}
	urls, err := service.configuredProbeURLs()
	if err != nil || !reflect.DeepEqual(urls, []string{"https://first.example/204", "https://second.example/204"}) {
		t.Fatalf("urls=%v error=%v", urls, err)
	}
	if !reflect.DeepEqual(state, store.state) {
		t.Fatal("saving addresses changed runtime state")
	}
	for _, bad := range []string{"", "http://example.com", "https://user:SECRET@example.com", "https://example.com/#fragment", "https://%"} {
		_, err := service.PatchSettings(map[string]string{"url-test-url": "https://changed.example/204", "url-test-fallback-url": bad})
		if err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatalf("unsafe validation error: %v", err)
		}
		if !reflect.DeepEqual(settings, store.settings) {
			t.Fatal("invalid patch changed settings")
		}
	}
	if _, err := service.SetSetting("url-test-fallback-url", "https://third.example/204"); err != nil {
		t.Fatal(err)
	}
	if store.settings.URLTestFallbackURL != "https://third.example/204" {
		t.Fatal("second URL was not persisted")
	}
}

func TestProbeURLsDefaultsAndDeduplication(t *testing.T) {
	if len(probeURLs(domain.Settings{})) != 2 {
		t.Fatal("missing defaults")
	}
	if len(probeURLs(domain.Settings{URLTestURL: "https://same.example/204", URLTestFallbackURL: "https://same.example/204"})) != 1 {
		t.Fatal("duplicate checks")
	}
}
