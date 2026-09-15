package store

import (
	"errors"
	"fmt"
	"os"
)

// SaveAWGProfile persists the already validated native profile with owner-only
// permissions. Parsing and validation remain in the application layer.
func (s *FileStore) SaveAWGProfile(raw []byte) error {
	if len(raw) == 0 {
		return fmt.Errorf("AmneziaWG profile is empty")
	}
	return atomicWriteFile(s.paths.AWGProfilePath, append(append([]byte(nil), raw...), '\n'), SecretFilePerm)
}

func (s *FileStore) LoadAWGProfile() ([]byte, error) {
	data, err := os.ReadFile(s.paths.AWGProfilePath)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), data...), nil
}

func (s *FileStore) RemoveAWGProfile() error {
	err := os.Remove(s.paths.AWGProfilePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
