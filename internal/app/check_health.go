package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/design-maestro/fastlane/internal/domain"
)

// CheckHealth updates observations only. In particular, a user asking for a
// latency measurement does not authorize selecting an outbound or changing mode.
func (s *Service) CheckHealth(ctx context.Context, scope string) error {
	scope = strings.TrimSpace(scope)
	s.probeAWGProfilesForAuto(ctx, scope)
	snapshot, err := s.captureAutoSelectionSnapshot()
	if err != nil {
		return err
	}
	health := cloneHealthMap(snapshot.state.Health)
	observed := make(map[string]domain.NodeHealth)
	found := scope == "" || scope == autoScopeAll
	for _, sub := range snapshot.subscriptions {
		if scope != "" && scope != autoScopeAll && sub.ID != scope {
			continue
		}
		found = true
		if sub.IsExpired(s.currentTime().UTC()) {
			continue
		}
		var nodes []domain.Node
		for _, node := range sub.Nodes {
			if node.Protocol != domain.ProtocolAmneziaWG && !domain.IsNodeExcludedFromAuto(snapshot.settings, sub.ID, node) {
				nodes = append(nodes, node)
			}
		}
		sub.Nodes = nodes
		for _, result := range s.probeSubscription(ctx, sub, health, switchPolicyFromSettings(snapshot.settings).FailureThreshold) {
			observed[result.NodeID] = health[result.NodeID]
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	if !found {
		return fmt.Errorf("subscription %q not found", scope)
	}
	return runStoreWriteLocked(s, func() error {
		state, err := s.store.LoadState()
		if err != nil {
			return err
		}
		if state.Health == nil {
			state.Health = make(map[string]domain.NodeHealth)
		}
		for id, result := range observed {
			if !state.Health[id].LastCheckedAt.After(result.LastCheckedAt) {
				state.Health[id] = result
			}
		}
		return s.saveState(state)
	})
}
