package app

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/design-maestro/fastlane/internal/domain"
)

func validateProbeURL(raw string) error {
	u, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Hostname() == "" || u.User != nil || strings.Contains(raw, "#") {
		return fmt.Errorf("invalid URL test URL: absolute HTTPS URL without credentials or fragment is required")
	}
	return nil
}

func probeURLs(settings domain.Settings) []string {
	defaults := domain.DefaultSettings()
	first, second := strings.TrimSpace(settings.URLTestURL), strings.TrimSpace(settings.URLTestFallbackURL)
	if first == "" {
		first = defaults.URLTestURL
	}
	if second == "" {
		second = defaults.URLTestFallbackURL
	}
	if first == second {
		return []string{first}
	}
	return []string{first, second}
}

func (s *Service) configuredProbeURLs() ([]string, error) {
	settings, err := s.store.LoadSettings()
	if err != nil {
		return nil, fmt.Errorf("load probe settings: %w", err)
	}
	return probeURLs(settings), nil
}
