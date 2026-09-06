package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/design-maestro/fastlane/internal/domain"
)

var errAutoSelectionSnapshotChanged = errors.New("auto selection inputs changed while probes were running")

// retryableCandidateError marks a failure that is known to belong to the
// candidate node rather than to shared Fast Lane state or runtime control.
// Auto selection may try the next already-probed node only for this error.
type retryableCandidateError struct {
	err error
}

func (e *retryableCandidateError) Error() string {
	if e == nil || e.err == nil {
		return "candidate verification failed"
	}
	return e.err.Error()
}

func (e *retryableCandidateError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func markRetryableCandidateError(err error) error {
	if err == nil || isRetryableCandidateError(err) {
		return err
	}
	return &retryableCandidateError{err: err}
}

func isRetryableCandidateError(err error) bool {
	var candidateErr *retryableCandidateError
	return errors.As(err, &candidateErr)
}

type autoSelectionSnapshot struct {
	subscriptions  []domain.Subscription
	settings       domain.Settings
	persistedState domain.RuntimeState
	state          domain.RuntimeState
}

type preparedAutoSelection struct {
	snapshot    autoSelectionSnapshot
	all         bool
	sub         domain.Subscription
	decision    autoSelectionDecision
	selectedSub domain.Subscription
	fallbackSub domain.Subscription
}

func (s *Service) captureAutoSelectionSnapshot() (autoSelectionSnapshot, error) {
	return runStoreWriteLockedResult(s, s.captureAutoSelectionSnapshotLocked)
}

func (s *Service) captureAutoSelectionSnapshotLocked() (autoSelectionSnapshot, error) {
	subscriptions, err := s.store.LoadSubscriptions()
	if err != nil {
		return autoSelectionSnapshot{}, fmt.Errorf("load subscriptions: %w", err)
	}
	settings, err := s.store.LoadSettings()
	if err != nil {
		return autoSelectionSnapshot{}, fmt.Errorf("load settings: %w", err)
	}
	persistedState, err := s.store.LoadState()
	if err != nil {
		return autoSelectionSnapshot{}, fmt.Errorf("load state: %w", err)
	}

	return autoSelectionSnapshot{
		subscriptions:  subscriptions,
		settings:       settings,
		persistedState: persistedState,
		state:          s.mergeAutoHealthState(persistedState),
	}, nil
}

func (s *Service) prepareAutoSelection(ctx context.Context, subscriptionID string, snapshot autoSelectionSnapshot) (preparedAutoSelection, error) {
	all := strings.TrimSpace(subscriptionID) == "" || strings.EqualFold(strings.TrimSpace(subscriptionID), autoScopeAll)
	prepared := preparedAutoSelection{snapshot: snapshot, all: all}
	if all {
		decision, selectedSub, fallbackSub, err := s.evaluateAutoSelectionAll(ctx, snapshot.subscriptions, snapshot.settings, snapshot.state, true)
		if err != nil {
			return preparedAutoSelection{}, err
		}
		prepared.decision = decision
		prepared.selectedSub = selectedSub
		prepared.fallbackSub = fallbackSub
		return prepared, nil
	}

	for _, sub := range snapshot.subscriptions {
		if sub.ID != subscriptionID {
			continue
		}
		if sub.IsExpired(s.currentTime().UTC()) {
			return preparedAutoSelection{}, fmt.Errorf("subscription %q expired; its servers are view-only", subscriptionID)
		}
		decision, err := s.evaluateAutoSelection(ctx, sub, snapshot.settings, snapshot.state, true)
		if err != nil {
			return preparedAutoSelection{}, err
		}
		prepared.sub = sub
		prepared.selectedSub = sub
		prepared.fallbackSub = sub
		prepared.decision = decision
		return prepared, nil
	}

	return preparedAutoSelection{}, fmt.Errorf("subscription %q not found", subscriptionID)
}

func (s *Service) connectAutoUsingSnapshot(ctx context.Context, subscriptionID string, snapshot autoSelectionSnapshot) (domain.Node, error) {
	prepared, err := s.prepareAutoSelection(ctx, subscriptionID, snapshot)
	if err != nil {
		return domain.Node{}, err
	}

	return runStoreWriteLockedResult(s, func() (domain.Node, error) {
		current, err := s.autoSelectionSnapshotCurrentLocked(snapshot)
		if err != nil {
			return domain.Node{}, err
		}
		if !current {
			return domain.Node{}, errAutoSelectionSnapshotChanged
		}
		return s.commitPreparedAutoSelection(ctx, prepared)
	})
}

func (s *Service) autoSelectionSnapshotCurrentLocked(snapshot autoSelectionSnapshot) (bool, error) {
	current, err := s.captureAutoSelectionSnapshotLocked()
	if err != nil {
		return false, err
	}

	return reflect.DeepEqual(current.subscriptions, snapshot.subscriptions) &&
		reflect.DeepEqual(current.settings, snapshot.settings) &&
		reflect.DeepEqual(current.state, snapshot.state), nil
}

func mergeProbeHealth(current map[string]domain.NodeHealth, probed map[string]domain.NodeHealth, failedNodeID string) map[string]domain.NodeHealth {
	merged := cloneHealthMap(probed)
	if merged == nil {
		merged = make(map[string]domain.NodeHealth)
	}
	for nodeID, health := range current {
		if nodeID == failedNodeID {
			merged[nodeID] = health
		}
	}
	return merged
}
