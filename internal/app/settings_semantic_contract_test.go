package app

import (
	"reflect"
	"strings"
	"testing"

	"github.com/design-maestro/fastlane/internal/domain"
)

func TestSettingSemanticValidationRejectsInvalidValuesWithoutWrites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{name: "zero refresh interval", key: "refresh-interval", value: "0s", want: "greater than zero"},
		{name: "negative refresh interval", key: "refresh-interval", value: "-1s", want: "greater than zero"},
		{name: "negative health interval", key: "health-check-interval", value: "-1ms", want: "zero or greater"},
		{name: "sub-second refresh interval", key: "refresh-interval", value: "999ms", want: "at least 1s"},
		{name: "sub-second health interval", key: "health-check-interval", value: "1ns", want: "at least 1s"},
		{name: "zero URL test timeout", key: "url-test-timeout", value: "0s", want: "greater than zero"},
		{name: "negative URL test timeout", key: "url-test-timeout", value: "-1ns", want: "greater than zero"},
		{name: "negative switch cooldown", key: "switch-cooldown", value: "-1s", want: "zero or greater"},
		{name: "negative latency threshold", key: "latency-threshold", value: "-1ms", want: "zero or greater"},
		{name: "invalid strict check boolean", key: "strict-egress-check", value: "yes", want: "true or false"},
		{name: "empty country routing boolean", key: "country-routing.enabled", value: "", want: "true or false"},
		{name: "invalid country code", key: "country-routing.country", value: "ZZ", want: "unsupported country code"},
		{name: "numeric auto mode boolean", key: "auto-mode", value: "1", want: "true or false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			initial := domain.DefaultSettings()
			store := &semanticContractStore{
				settings: initial,
				state:    domain.DefaultRuntimeState(),
			}
			service := NewService(Dependencies{Store: store})

			if _, err := service.SetSetting(tt.key, tt.value); err == nil {
				t.Fatalf("SetSetting(%q, %q) succeeded; want validation error", tt.key, tt.value)
			} else if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("SetSetting(%q, %q) error = %q; want %q", tt.key, tt.value, err, tt.want)
			}

			if store.saveSettingsCalls != 0 || store.saveSubscriptionsCalls != 0 || store.saveStateCalls != 0 {
				t.Fatalf("invalid value caused writes: settings=%d subscriptions=%d state=%d", store.saveSettingsCalls, store.saveSubscriptionsCalls, store.saveStateCalls)
			}
			if !reflect.DeepEqual(store.settings, initial) {
				t.Fatalf("invalid value changed persisted settings: got %+v want %+v", store.settings, initial)
			}
		})
	}
}

func TestSettingSemanticValidationAcceptsBoundaryValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "positive refresh interval", key: "refresh-interval", value: "1s"},
		{name: "zero health interval disables checks", key: "health-check-interval", value: "0s"},
		{name: "positive URL test timeout", key: "url-test-timeout", value: "1ns"},
		{name: "zero switch cooldown", key: "switch-cooldown", value: "0s"},
		{name: "zero latency threshold", key: "latency-threshold", value: "0s"},
		{name: "trimmed case insensitive true", key: "strict-egress-check", value: " TRUE "},
		{name: "country selection", key: "country-routing.country", value: "ca"},
		{name: "false country routing", key: "country-routing.enabled", value: "false"},
		{name: "false auto mode", key: "auto-mode", value: "FALSE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := &semanticContractStore{
				settings: domain.DefaultSettings(),
				state:    domain.DefaultRuntimeState(),
			}
			service := NewService(Dependencies{Store: store})

			if _, err := service.SetSetting(tt.key, tt.value); err != nil {
				t.Fatalf("SetSetting(%q, %q): %v", tt.key, tt.value, err)
			}
			if store.saveSettingsCalls != 1 {
				t.Fatalf("settings writes = %d; want 1", store.saveSettingsCalls)
			}
		})
	}
}

type semanticContractStore struct {
	subscriptions          []domain.Subscription
	settings               domain.Settings
	state                  domain.RuntimeState
	saveSubscriptionsCalls int
	saveSettingsCalls      int
	saveStateCalls         int
}

func (s *semanticContractStore) LoadSubscriptions() ([]domain.Subscription, error) {
	return s.subscriptions, nil
}

func (s *semanticContractStore) SaveSubscriptions(subscriptions []domain.Subscription) error {
	s.saveSubscriptionsCalls++
	s.subscriptions = subscriptions
	return nil
}

func (s *semanticContractStore) LoadSettings() (domain.Settings, error) {
	return s.settings, nil
}

func (s *semanticContractStore) SaveSettings(settings domain.Settings) error {
	s.saveSettingsCalls++
	s.settings = settings
	return nil
}

func (s *semanticContractStore) LoadState() (domain.RuntimeState, error) {
	return s.state, nil
}

func (s *semanticContractStore) SaveState(state domain.RuntimeState) error {
	s.saveStateCalls++
	s.state = state
	return nil
}
