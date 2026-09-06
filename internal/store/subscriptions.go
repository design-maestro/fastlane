package store

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/design-maestro/fastlane/internal/domain"
)

// FileStore persists Fast Lane state as JSON files.
type FileStore struct {
	paths  Paths
	logger *slog.Logger
}

// NewFileStore creates a file-backed store rooted at the provided directory.
func NewFileStore(root string) *FileStore {
	return &FileStore{paths: NewPaths(root)}
}

// WithLogger configures an optional logger for recovery warnings.
func (s *FileStore) WithLogger(logger *slog.Logger) *FileStore {
	s.logger = logger
	return s
}

// SaveSubscriptions persists all subscriptions.
func (s *FileStore) SaveSubscriptions(subscriptions []domain.Subscription) error {
	return AtomicWriteJSON(s.paths.SubscriptionsPath, subscriptions)
}

// LoadSubscriptions loads subscriptions, returning an empty list if the file does not exist.
func (s *FileStore) LoadSubscriptions() ([]domain.Subscription, error) {
	subscriptions, _, err := s.readSubscriptions()
	return subscriptions, err
}

// readSubscriptions is deliberately read-only. Corrupt-file recovery is
// performed separately by RecoverCorruptFiles while holding the write lock.
func (s *FileStore) readSubscriptions() ([]domain.Subscription, bool, error) {
	data, err := os.ReadFile(s.paths.SubscriptionsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []domain.Subscription{}, false, nil
		}
		return nil, false, err
	}

	var subscriptions []domain.Subscription
	if err := json.Unmarshal(data, &subscriptions); err != nil {
		return nil, true, fmt.Errorf("unmarshal %s: %w", s.paths.SubscriptionsPath, err)
	}

	if subscriptions == nil {
		return []domain.Subscription{}, true, nil
	}

	return subscriptions, true, nil
}

func (s *FileStore) logWarn(msg string, args ...any) {
	if s != nil && s.logger != nil {
		s.logger.Warn(msg, args...)
	}
}
