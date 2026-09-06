package store

import (
	"os"

	"github.com/design-maestro/fastlane/internal/domain"
)

// SaveSettings persists user settings.
func (s *FileStore) SaveSettings(settings domain.Settings) error {
	settings.SchemaVersion = domain.DefaultSettings().SchemaVersion
	return AtomicWriteJSON(s.paths.SettingsPath, settings)
}

// LoadSettings loads persisted settings or returns defaults.
func (s *FileStore) LoadSettings() (domain.Settings, error) {
	settings, _, err := s.readSettings()
	return settings, err
}

// readSettings is deliberately read-only. Corrupt-file recovery is performed
// separately by RecoverCorruptFiles while holding the store write lock.
func (s *FileStore) readSettings() (domain.Settings, bool, error) {
	defaults := domain.DefaultSettings()
	data, err := os.ReadFile(s.paths.SettingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaults, false, nil
		}
		return domain.Settings{}, false, err
	}

	settings, err := decodeSettings(data, s.paths.SettingsPath)
	if err != nil {
		return domain.Settings{}, true, err
	}

	return settings, true, nil
}
