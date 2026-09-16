package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/design-maestro/fastlane/internal/amneziawg"
)

// SaveAWGProfile persists an already validated profile and its secret-free
// metadata. It returns false when the stable ID already exists, making repeated
// imports idempotent.
func (s *FileStore) SaveAWGProfile(metadata amneziawg.ProfileMetadata, raw []byte) (bool, error) {
	metadata.ID = strings.TrimSpace(metadata.ID)
	metadata.Name = strings.TrimSpace(metadata.Name)
	if metadata.ID == "" || strings.ContainsAny(metadata.ID, `/\\`) {
		return false, fmt.Errorf("invalid AmneziaWG profile ID")
	}
	if len(raw) == 0 {
		return false, fmt.Errorf("AmneziaWG profile is empty")
	}

	profiles, err := s.ListAWGProfiles()
	if err != nil {
		return false, err
	}
	for _, existing := range profiles {
		if existing.ID == metadata.ID {
			return false, nil
		}
	}

	if err := os.MkdirAll(s.paths.AWGProfilesDir, PrivateDirPerm); err != nil {
		return false, fmt.Errorf("create AmneziaWG profile directory: %w", err)
	}
	profilePath := s.awgProfilePath(metadata.ID)
	if err := atomicWriteFile(profilePath, append(append([]byte(nil), raw...), '\n'), SecretFilePerm); err != nil {
		return false, err
	}

	profiles = append(profiles, metadata)
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ID < profiles[j].ID })
	if err := AtomicWriteJSON(s.paths.AWGProfilesPath, profiles); err != nil {
		_ = os.Remove(profilePath)
		return false, fmt.Errorf("save AmneziaWG profile index: %w", err)
	}
	return true, nil
}

func (s *FileStore) ListAWGProfiles() ([]amneziawg.ProfileMetadata, error) {
	data, err := os.ReadFile(s.paths.AWGProfilesPath)
	if errors.Is(err, os.ErrNotExist) {
		return []amneziawg.ProfileMetadata{}, nil
	}
	if err != nil {
		return nil, err
	}
	var profiles []amneziawg.ProfileMetadata
	if err := json.Unmarshal(data, &profiles); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", s.paths.AWGProfilesPath, err)
	}
	if profiles == nil {
		return []amneziawg.ProfileMetadata{}, nil
	}
	return append([]amneziawg.ProfileMetadata(nil), profiles...), nil
}

func (s *FileStore) LoadAWGProfile(id string) ([]byte, error) {
	data, err := os.ReadFile(s.awgProfilePath(id))
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), data...), nil
}

func (s *FileStore) RemoveAWGProfile(id string) error {
	profiles, err := s.ListAWGProfiles()
	if err != nil {
		return err
	}
	filtered := profiles[:0]
	found := false
	for _, profile := range profiles {
		if profile.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, profile)
	}
	if !found {
		return nil
	}
	if err := os.Remove(s.awgProfilePath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := AtomicWriteJSON(s.paths.AWGProfilesPath, filtered); err != nil {
		return fmt.Errorf("save AmneziaWG profile index: %w", err)
	}
	return nil
}

// LoadLegacyAWGProfile and RemoveLegacyAWGProfile support the v0.1.29
// single-profile layout. Migration is coordinated by the application layer so
// it can validate the profile and update runtime identity under one store lock.
func (s *FileStore) LoadLegacyAWGProfile() ([]byte, error) {
	data, err := os.ReadFile(s.paths.AWGProfilePath)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), data...), nil
}

func (s *FileStore) RemoveLegacyAWGProfile() error {
	err := os.Remove(s.paths.AWGProfilePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *FileStore) awgProfilePath(id string) string {
	return filepath.Join(s.paths.AWGProfilesDir, filepath.Base(strings.TrimSpace(id))+".conf")
}
