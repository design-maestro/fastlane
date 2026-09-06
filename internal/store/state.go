package store

import (
	"os"

	"github.com/design-maestro/fastlane/internal/domain"
)

// SaveState persists runtime state.
func (s *FileStore) SaveState(state domain.RuntimeState) error {
	state.SchemaVersion = domain.DefaultRuntimeState().SchemaVersion
	return AtomicWriteJSON(s.paths.StatePath, state)
}

// LoadState loads persisted runtime state or returns the default state.
func (s *FileStore) LoadState() (domain.RuntimeState, error) {
	state, _, err := s.readState()
	return state, err
}

// readState is deliberately read-only. Corrupt-file recovery is performed
// separately by RecoverCorruptFiles while holding the store write lock.
func (s *FileStore) readState() (domain.RuntimeState, bool, error) {
	defaults := domain.DefaultRuntimeState()
	data, err := os.ReadFile(s.paths.StatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaults, false, nil
		}
		return domain.RuntimeState{}, false, err
	}

	state, err := decodeState(data, s.paths.StatePath)
	if err != nil {
		return domain.RuntimeState{}, true, err
	}

	return state, true, nil
}
