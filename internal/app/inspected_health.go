package app

import (
	"fmt"
	"reflect"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/probe"
	"github.com/design-maestro/fastlane/internal/speedtest"
)

func (s *Service) persistInspectedHealth(subscriptionID string, node domain.Node, result speedtest.URLTestResult, settings domain.Settings) error {
	if result.LatencyMS <= 0 {
		return nil
	}
	return runStoreWriteLocked(s, func() error {
		// Do not resurrect a deleted or replaced server after a slow GET.
		_, current, err := s.subscriptionNode(subscriptionID, node.ID)
		if err != nil || !reflect.DeepEqual(current, node) {
			return nil
		}
		state, err := s.store.LoadState()
		if err != nil {
			return fmt.Errorf("load GET observation state: %w", err)
		}
		checkedAt := result.CheckedAt
		if checkedAt.IsZero() {
			checkedAt = s.currentTime().UTC()
		}
		previous := state.Health[node.ID]
		if !previous.LastCheckedAt.IsZero() && !checkedAt.After(previous.LastCheckedAt) {
			// A faster concurrent reserve check may have newer metrics but no
			// location. Fill a missing identity without overwriting those metrics.
			if previous.CountryCode == "" && result.CountryCode != "" {
				previous.CountryCode, previous.EgressIP = result.CountryCode, result.EgressIP
				state.Health[node.ID] = previous
				return s.saveState(state)
			}
			return nil
		}
		updated := probe.UpdateHealth(previous, true, time.Duration(result.LatencyMS*float64(time.Millisecond)), checkedAt, "", switchPolicyFromSettings(settings).FailureThreshold)
		updated.NodeID = node.ID
		updated.Score = probe.CalculateScore(updated, probe.DefaultScoreConfig()).Score
		if result.CountryCode != "" {
			updated.CountryCode, updated.EgressIP = result.CountryCode, result.EgressIP
		}
		if state.Health == nil {
			state.Health = make(map[string]domain.NodeHealth)
		}
		state.Health[node.ID] = updated
		return s.saveState(state)
	})
}
