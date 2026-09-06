package app

import (
	"reflect"
	"testing"

	"github.com/design-maestro/fastlane/internal/domain"
)

func TestPatchSettingsAppliesOperationalFieldsTogether(t *testing.T) {
	t.Parallel()
	store := &memoryStore{
		settings: domain.DefaultSettings(),
		state:    domain.DefaultRuntimeState(),
		subs:     []domain.Subscription{{ID: "sub-1", RefreshInterval: domain.NewDuration(15)}},
	}
	service := NewService(Dependencies{Store: store})

	settings, err := service.PatchSettings(map[string]string{
		"refresh-interval":      "2h3m4s",
		"health-check-interval": "45s",
		"url-test-url":          "https://example.com/generate_204",
		"url-test-timeout":      "12s",
		"switch-cooldown":       "2m5s",
		"latency-threshold":     "70ms",
		"strict-egress-check":   "false",
	})
	if err != nil {
		t.Fatalf("patch settings: %v", err)
	}
	if settings.RefreshInterval.Duration().String() != "2h3m4s" {
		t.Fatalf("refresh interval was not applied to settings: %s", settings.RefreshInterval)
	}
	if store.subs[0].RefreshInterval.Duration().String() != "15ns" {
		t.Fatalf("legacy per-subscription interval should remain untouched: %s", store.subs[0].RefreshInterval)
	}
	if settings.HealthCheckInterval.Duration().String() != "45s" || settings.URLTestTimeout.Duration().String() != "12s" || settings.SwitchCooldown.Duration().String() != "2m5s" || settings.LatencyThreshold.Duration().String() != "70ms" {
		t.Fatalf("duration patch was not applied: %+v", settings)
	}
	if settings.URLTestURL != "https://example.com/generate_204" || settings.StrictEgressCheck {
		t.Fatalf("URL/boolean patch was not applied: %+v", settings)
	}
}

func TestPatchSettingsRejectsWholePatchBeforeWriting(t *testing.T) {
	t.Parallel()
	store := &memoryStore{
		settings: domain.DefaultSettings(),
		state:    domain.DefaultRuntimeState(),
		subs:     []domain.Subscription{{ID: "sub-1", RefreshInterval: domain.NewDuration(15)}},
	}
	originalSettings := store.settings
	originalSubscriptions := append([]domain.Subscription(nil), store.subs...)
	service := NewService(Dependencies{Store: store})

	_, err := service.PatchSettings(map[string]string{
		"refresh-interval": "2h",
		"url-test-url":     "http://not-https.example/",
	})
	if err == nil {
		t.Fatal("expected invalid URL to reject the patch")
	}
	if !reflect.DeepEqual(store.settings, originalSettings) || !reflect.DeepEqual(store.subs, originalSubscriptions) {
		t.Fatalf("invalid patch changed persisted data: settings=%+v subscriptions=%+v", store.settings, store.subs)
	}
}
