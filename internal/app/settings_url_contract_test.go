package app

import (
	"strings"
	"testing"

	"github.com/design-maestro/fastlane/internal/domain"
)

func TestURLTestURLAcceptsOnlyAbsoluteHTTPS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "absolute HTTPS", value: "https://example.com/generate_204", want: "https://example.com/generate_204"},
		{name: "absolute HTTPS is trimmed", value: "  https://example.com/generate_204?source=fastlane  ", want: "https://example.com/generate_204?source=fastlane"},
		{name: "uppercase HTTPS scheme", value: "HTTPS://example.com/generate_204", want: "HTTPS://example.com/generate_204"},
		{name: "empty", value: "", wantErr: true},
		{name: "relative path", value: "/generate_204", wantErr: true},
		{name: "scheme relative", value: "//example.com/generate_204", wantErr: true},
		{name: "host without scheme", value: "example.com/generate_204", wantErr: true},
		{name: "HTTP", value: "http://example.com/generate_204", wantErr: true},
		{name: "HTTPS without host", value: "https:///generate_204", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			initial := domain.DefaultSettings()
			store := &urlContractStore{
				settings: initial,
				state:    domain.DefaultRuntimeState(),
			}
			service := NewService(Dependencies{Store: store})

			settings, err := service.SetSetting("url-test-url", tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("SetSetting(url-test-url, %q) succeeded; want validation error", tt.value)
				}
				if !strings.Contains(err.Error(), "invalid URL test URL") {
					t.Fatalf("SetSetting(url-test-url, %q) error = %q; want URL validation error", tt.value, err)
				}
				if store.settings.URLTestURL != initial.URLTestURL {
					t.Fatalf("invalid URL changed persisted setting to %q; want %q", store.settings.URLTestURL, initial.URLTestURL)
				}
				return
			}

			if err != nil {
				t.Fatalf("SetSetting(url-test-url, %q): %v", tt.value, err)
			}
			if settings.URLTestURL != tt.want {
				t.Fatalf("URLTestURL = %q; want %q", settings.URLTestURL, tt.want)
			}
			if store.settings.URLTestURL != tt.want {
				t.Fatalf("persisted URLTestURL = %q; want %q", store.settings.URLTestURL, tt.want)
			}
		})
	}
}

type urlContractStore struct {
	subscriptions []domain.Subscription
	settings      domain.Settings
	state         domain.RuntimeState
}

func (s *urlContractStore) LoadSubscriptions() ([]domain.Subscription, error) {
	return s.subscriptions, nil
}

func (s *urlContractStore) SaveSubscriptions(subscriptions []domain.Subscription) error {
	s.subscriptions = subscriptions
	return nil
}

func (s *urlContractStore) LoadSettings() (domain.Settings, error) {
	return s.settings, nil
}

func (s *urlContractStore) SaveSettings(settings domain.Settings) error {
	s.settings = settings
	return nil
}

func (s *urlContractStore) LoadState() (domain.RuntimeState, error) {
	return s.state, nil
}

func (s *urlContractStore) SaveState(state domain.RuntimeState) error {
	s.state = state
	return nil
}
