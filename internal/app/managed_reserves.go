package app

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/design-maestro/fastlane/internal/backend"
	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/probe"
)

const managedReserveLimit = 2

type managedReserveCandidate struct {
	sub  domain.Subscription
	node domain.Node
	tag  string
	slot int
}

// MaintainManagedReserves verifies at most two standby handlers through the
// live Xray probe inbounds. It never changes the user balancer target.
func (s *Service) MaintainManagedReserves(ctx context.Context) error {
	managed, ok := s.backend.(backend.ManagedBackend)
	if !ok || s.store == nil {
		return nil
	}
	return runStoreWriteLocked(s, func() error {
		state, err := s.store.LoadState()
		if err != nil {
			return err
		}
		if state.Mode != domain.SelectionModeAuto && state.Mode != domain.SelectionModeManual {
			return nil
		}
		selected, err := managed.SelectedOutbound(ctx)
		if err != nil {
			return nil
		}
		subscriptions, err := s.store.LoadSubscriptions()
		if err != nil {
			return err
		}
		settings, err := s.store.LoadSettings()
		if err != nil {
			return err
		}
		excluded := make(map[string]struct{}, len(settings.AutoExcludedNodes))
		for _, key := range domain.NormalizeAutoExcludedNodes(settings.AutoExcludedNodes) {
			excluded[key] = struct{}{}
		}
		now := s.currentTime().UTC()
		candidates := make([]managedReserveCandidate, 0, managedReserveLimit)
		seenTags := map[string]struct{}{selected: {}}
		for _, sub := range subscriptions {
			if sub.IsExpired(now) {
				continue
			}
			for _, node := range sub.Nodes {
				if state.OperationalMode != domain.OperationalModeDirect && sub.ID == state.ActiveSubscriptionID && node.ID == state.ActiveNodeID {
					continue
				}
				if _, hidden := excluded[domain.AutoExcludedNodeKey(sub.ID, node.ID)]; hidden {
					continue
				}
				resolved, resolveErr := s.resolveNodeAddress(ctx, node)
				if resolveErr != nil {
					continue
				}
				tag, prepareErr := managed.PrepareOutbound(ctx, resolved, 0)
				if prepareErr != nil {
					continue
				}
				if _, duplicate := seenTags[tag]; duplicate {
					continue
				}
				seenTags[tag] = struct{}{}
				if backoff, exists := state.CandidateBackoff[tag]; exists && now.Before(backoff.RetryAfter) {
					_ = managed.RemoveOutbound(ctx, tag)
					continue
				}
				candidates = append(candidates, managedReserveCandidate{sub: sub, node: node, tag: tag, slot: len(candidates)})
				break // prefer reserves from different providers
			}
			if len(candidates) == managedReserveLimit {
				break
			}
		}
		if len(candidates) < managedReserveLimit {
			for _, sub := range subscriptions {
				for _, node := range sub.Nodes {
					if len(candidates) == managedReserveLimit {
						break
					}
					if sub.IsExpired(now) || (state.OperationalMode != domain.OperationalModeDirect && sub.ID == state.ActiveSubscriptionID && node.ID == state.ActiveNodeID) {
						continue
					}
					if _, hidden := excluded[domain.AutoExcludedNodeKey(sub.ID, node.ID)]; hidden {
						continue
					}
					resolved, resolveErr := s.resolveNodeAddress(ctx, node)
					if resolveErr != nil {
						continue
					}
					tag, prepareErr := managed.PrepareOutbound(ctx, resolved, 0)
					if prepareErr != nil {
						continue
					}
					if _, duplicate := seenTags[tag]; duplicate {
						continue
					}
					seenTags[tag] = struct{}{}
					if backoff, exists := state.CandidateBackoff[tag]; exists && now.Before(backoff.RetryAfter) {
						_ = managed.RemoveOutbound(ctx, tag)
						continue
					}
					candidates = append(candidates, managedReserveCandidate{sub: sub, node: node, tag: tag, slot: len(candidates)})
				}
			}
		}

		type result struct {
			candidate managedReserveCandidate
			err       error
			latency   time.Duration
		}
		results := make(chan result, len(candidates))
		var wg sync.WaitGroup
		for _, candidate := range candidates {
			candidate := candidate
			wg.Add(1)
			go func() {
				defer wg.Done()
				started := time.Now()
				probe := s.probeManagedOutbound
				if s.managedOutboundProbe != nil {
					probe = func(probeCtx context.Context, probeBackend backend.ManagedBackend, slot int, tag string) error {
						return s.managedOutboundProbe(probeCtx, probeBackend, slot, tag)
					}
				}
				probeErr := probe(ctx, managed, candidate.slot, candidate.tag)
				results <- result{candidate: candidate, err: probeErr, latency: time.Since(started)}
			}()
		}
		wg.Wait()
		close(results)

		state.CandidateBackoff = cloneCandidateBackoff(state.CandidateBackoff)
		state.Health = cloneHealthMap(state.Health)
		verified := make([]domain.RuntimeOutboundState, 0, managedReserveLimit)
		for item := range results {
			if item.err == nil {
				delete(state.CandidateBackoff, item.candidate.tag)
				verified = append(verified, domain.RuntimeOutboundState{Tag: item.candidate.tag, SubscriptionID: item.candidate.sub.ID, NodeID: item.candidate.node.ID, Role: "reserve", VerifiedAt: now})
				previous := state.Health[item.candidate.node.ID]
				previous.NodeID = item.candidate.node.ID
				state.Health[item.candidate.node.ID] = probe.UpdateHealth(previous, true, item.latency, now, "", 2)
				continue
			}
			entry := state.CandidateBackoff[item.candidate.tag]
			entry.Tag = item.candidate.tag
			entry.ConsecutiveFailures++
			entry.RetryAfter = now.Add(managedCandidateBackoff(entry.ConsecutiveFailures))
			state.CandidateBackoff[item.candidate.tag] = entry
			previous := state.Health[item.candidate.node.ID]
			previous.NodeID = item.candidate.node.ID
			state.Health[item.candidate.node.ID] = probe.UpdateHealth(previous, false, item.latency, now, item.err.Error(), 2)
			_ = managed.RemoveOutbound(ctx, item.candidate.tag)
		}
		kept := make([]domain.RuntimeOutboundState, 0, len(state.RuntimeOutbounds)+len(verified))
		verifiedTags := make(map[string]struct{}, len(verified))
		for _, outbound := range verified {
			verifiedTags[outbound.Tag] = struct{}{}
		}
		for _, outbound := range state.RuntimeOutbounds {
			if outbound.Role != "reserve" {
				kept = append(kept, outbound)
				continue
			}
			if _, retain := verifiedTags[outbound.Tag]; !retain {
				if !strings.HasPrefix(strings.TrimSpace(outbound.Tag), "fastlane-node-") {
					kept = append(kept, outbound)
				} else {
					_ = managed.RemoveOutbound(ctx, outbound.Tag)
				}
			}
		}
		state.RuntimeOutbounds = append(kept, verified...)
		return s.saveState(state)
	})
}

func managedCandidateBackoff(failures int) time.Duration {
	switch failures {
	case 1:
		return 5 * time.Minute
	case 2:
		return 10 * time.Minute
	case 3:
		return 20 * time.Minute
	default:
		return 30 * time.Minute
	}
}
