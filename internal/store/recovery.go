package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
)

// RecoverCorruptFiles performs the only mutating part of persisted-file
// recovery while holding the same write lock used by application updates.
// Normal Load* methods stay read-only, so they are safe to call from inside an
// existing write transaction without attempting to acquire the lock again.
func (s *FileStore) RecoverCorruptFiles() error {
	return s.WithWriteLock(s.recoverCorruptFilesLocked)
}

func (s *FileStore) recoverCorruptFilesLocked() error {
	if _, present, err := s.readSettings(); err != nil {
		if !present {
			return fmt.Errorf("read settings for recovery: %w", err)
		}
		if !errors.Is(err, ErrUnsupportedSettingsSchema) {
			if _, recoverErr := s.recoverCorruptJSON(s.paths.SettingsPath, domain.DefaultSettings(), err); recoverErr != nil {
				return fmt.Errorf("recover settings: %w", recoverErr)
			}
		}
	}

	if _, present, err := s.readState(); err != nil {
		if !present {
			return fmt.Errorf("read state for recovery: %w", err)
		}
		if !errors.Is(err, ErrUnsupportedStateSchema) {
			if _, recoverErr := s.recoverCorruptJSON(s.paths.StatePath, domain.DefaultRuntimeState(), err); recoverErr != nil {
				return fmt.Errorf("recover state: %w", recoverErr)
			}
		}
	}

	if _, present, err := s.readSubscriptions(); err != nil {
		if !present {
			return fmt.Errorf("read subscriptions for recovery: %w", err)
		}
		if _, recoverErr := s.recoverCorruptJSON(s.paths.SubscriptionsPath, []domain.Subscription{}, err); recoverErr != nil {
			return fmt.Errorf("recover subscriptions: %w", recoverErr)
		}
	}

	return nil
}

func (s *FileStore) recoverCorruptJSON(path string, replacement any, loadErr error) (string, error) {
	backupPath, err := renameCorruptFile(path)
	if err != nil {
		return "", fmt.Errorf("backup corrupt file %s: %w", path, err)
	}
	if err := AtomicWriteJSON(path, replacement); err != nil {
		return "", fmt.Errorf("rewrite recovered file %s: %w", path, err)
	}
	if loadErr != nil {
		s.logWarn("recovered corrupt persisted file", "path", path, "backup", backupPath, "error", loadErr.Error())
	}
	return backupPath, nil
}

func renameCorruptFile(path string) (string, error) {
	backupPath, err := corruptBackupPath(path, time.Now().UTC())
	if err != nil {
		return "", err
	}
	if err := os.Rename(path, backupPath); err != nil {
		return "", err
	}
	if err := os.Chmod(backupPath, SecretFilePerm); err != nil {
		return "", err
	}
	return backupPath, nil
}

func corruptBackupPath(path string, now time.Time) (string, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	timestamp := now.UTC().Format("20060102T150405Z")

	for attempt := 0; attempt < 100; attempt++ {
		suffix := ""
		if attempt > 0 {
			suffix = fmt.Sprintf("-%d", attempt)
		}
		candidate := filepath.Join(dir, fmt.Sprintf("%s.corrupt-%s%s%s", stem, timestamp, suffix, ext))
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}

	return "", fmt.Errorf("allocate unique corrupt backup path for %s", path)
}
