package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/design-maestro/fastlane/internal/backend"
	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/probe"
)

const managedReserveLimit = 2
const managedReserveProbeLimit = 2
const managedReserveHealthyConfirmations = 2

type managedReserveCandidate struct {
	sub        domain.Subscription
	node       domain.Node
	tag        string
	verifiedAt time.Time
}

// MaintainManagedReserves verifies up to two candidates, with no more than
// two concurrent checks, through the live Xray probe inbounds. It retains at
// most two verified reserves and never changes the user balancer target.
func (s *Service) MaintainManagedReserves(ctx context.Context) error {
	managed, ok := s.backend.(backend.ManagedBackend)
	if !ok || s.store == nil {
		return nil
	}
	snapshot, err := s.captureAutoSelectionSnapshot()
	if err != nil {
		return err
	}
	state := snapshot.state
	if state.Mode != domain.SelectionModeAuto && state.Mode != domain.SelectionModeManual {
		return nil
	}
	selected, err := managed.SelectedOutbound(ctx)
	if err != nil {
		return nil
	}
	now := s.currentTime().UTC()
	allCandidates := collectManagedReserveCandidates(snapshot.subscriptions, snapshot.settings, state, now)
	ranked := rankManagedReserveCandidates(allCandidates, state.Health)
	existing := existingManagedReserveCandidates(state.RuntimeOutbounds, allCandidates)
	ordered := managedReserveProbeOrder(existing, ranked, state.Health, now)
	candidates := make([]managedReserveCandidate, 0, managedReserveProbeLimit)
	seenNodes := make(map[string]struct{}, len(ordered))
	seenTags := map[string]struct{}{selected: {}}
	knownTags := runtimeOutboundTags(state.RuntimeOutbounds)
	preparedNew := make(map[string]struct{})
	for _, candidate := range ordered {
		if len(candidates) == managedReserveProbeLimit {
			break
		}
		nodeKey := domain.AutoExcludedNodeKey(candidate.sub.ID, candidate.node.ID)
		if _, duplicate := seenNodes[nodeKey]; duplicate {
			continue
		}
		seenNodes[nodeKey] = struct{}{}
		if candidate.tag == "" {
			resolved, resolveErr := s.resolveNodeAddress(ctx, candidate.node)
			if resolveErr != nil {
				continue
			}
			tag, prepareErr := managed.PrepareOutbound(ctx, resolved, 0)
			if prepareErr != nil {
				continue
			}
			candidate.tag = tag
			if _, known := knownTags[tag]; !known {
				preparedNew[tag] = struct{}{}
			}
		}
		if _, duplicate := seenTags[candidate.tag]; duplicate {
			continue
		}
		seenTags[candidate.tag] = struct{}{}
		if backoff, exists := state.CandidateBackoff[candidate.tag]; exists && now.Before(backoff.RetryAfter) {
			continue
		}
		candidates = append(candidates, candidate)
	}

	type result struct {
		candidate managedReserveCandidate
		err       error
		latency   time.Duration
	}
	// The probe balancers are shared by the daemon and one-shot CLI processes.
	// Hold the inter-process store lock for the whole probe batch so a manual
	// AWG check cannot have its selected target replaced or cleared midway.
	return runStoreWriteLocked(s, func() error {
		results := make(chan result, len(candidates))
		jobs := make(chan managedReserveCandidate)
		var wg sync.WaitGroup
		workerCount := min(2, len(candidates))
		for slot := 0; slot < workerCount; slot++ {
			slot := slot
			wg.Add(1)
			go func() {
				defer wg.Done()
				for candidate := range jobs {
					started := time.Now()
					probeCandidate := s.probeManagedOutbound
					if s.managedOutboundProbe != nil {
						probeCandidate = func(probeCtx context.Context, probeBackend backend.ManagedBackend, slot int, tag string) error {
							return s.managedOutboundProbe(probeCtx, probeBackend, slot, tag)
						}
					}
					probeErr := probeCandidate(ctx, managed, slot, candidate.tag)
					results <- result{candidate: candidate, err: probeErr, latency: time.Since(started)}
				}
			}()
		}
		go func() {
			for _, candidate := range candidates {
				jobs <- candidate
			}
			close(jobs)
			wg.Wait()
			close(results)
		}()

		state.CandidateBackoff = cloneCandidateBackoff(state.CandidateBackoff)
		state.Health = cloneHealthMap(state.Health)
		successful := make([]managedReserveCandidate, 0, len(candidates))
		for item := range results {
			previous := state.Health[item.candidate.node.ID]
			previous.NodeID = item.candidate.node.ID
			if item.err == nil {
				delete(state.CandidateBackoff, item.candidate.tag)
				state.Health[item.candidate.node.ID] = probe.UpdateHealth(previous, true, item.latency, now, "", 2)
				successful = append(successful, item.candidate)
				continue
			}
			entry := state.CandidateBackoff[item.candidate.tag]
			entry.Tag = item.candidate.tag
			entry.ConsecutiveFailures++
			entry.RetryAfter = now.Add(managedCandidateBackoff(entry.ConsecutiveFailures))
			state.CandidateBackoff[item.candidate.tag] = entry
			state.Health[item.candidate.node.ID] = probe.UpdateHealth(previous, false, item.latency, now, item.err.Error(), 2)
		}

		nextManaged := selectManagedReserveStates(successful, state.Health, state.RuntimeOutbounds, now)
		keptTags := make(map[string]struct{}, len(nextManaged))
		for _, outbound := range nextManaged {
			keptTags[outbound.Tag] = struct{}{}
		}
		kept := make([]domain.RuntimeOutboundState, 0, len(state.RuntimeOutbounds)+len(nextManaged))
		obsolete := make(map[string]struct{})
		for _, outbound := range state.RuntimeOutbounds {
			if outbound.Role != "reserve" && outbound.Role != "candidate" {
				kept = append(kept, outbound)
				continue
			}
			if _, retain := keptTags[outbound.Tag]; !retain {
				obsolete[outbound.Tag] = struct{}{}
			}
		}
		state.RuntimeOutbounds = append(kept, nextManaged...)

		current, currentErr := s.autoSelectionSnapshotCurrentLocked(snapshot)
		if currentErr != nil {
			return currentErr
		}
		if !current {
			var cleanupErr error
			for tag := range preparedNew {
				if err := s.safeRemoveManagedOutbound(ctx, managed, tag); err != nil {
					cleanupErr = err
				}
			}
			s.logDebug("managed reserve result discarded because runtime inputs changed during probes")
			return cleanupErr
		}
		if saveErr := s.saveState(state); saveErr != nil {
			for tag := range preparedNew {
				s.safeRemoveManagedOutbound(ctx, managed, tag)
			}
			return saveErr
		}
		var cleanupErr error
		for tag := range obsolete {
			if strings.HasPrefix(strings.TrimSpace(tag), "fastlane-node-") {
				if removeErr := s.safeRemoveManagedOutbound(ctx, managed, tag); removeErr != nil {
					s.logWarn("remove obsolete managed reserve", "tag", tag, "error", removeErr.Error())
					cleanupErr = errors.Join(cleanupErr, removeErr)
				}
			}
		}
		for tag := range preparedNew {
			if _, retain := keptTags[tag]; !retain {
				if err := s.safeRemoveManagedOutbound(ctx, managed, tag); err != nil {
					cleanupErr = errors.Join(cleanupErr, err)
				}
			}
		}
		return cleanupErr
	})
}

func (s *Service) safeRemoveManagedOutbound(ctx context.Context, managed backend.ManagedBackend, tag string) error {
	if selected, err := managed.SelectedOutbound(ctx); err == nil && selected == tag {
		return nil
	}
	if current, err := s.store.LoadState(); err == nil {
		for _, outbound := range current.RuntimeOutbounds {
			if outbound.Tag == tag {
				return nil
			}
		}
	}
	if err := managed.RemoveOutbound(ctx, tag); err != nil {
		s.logWarn("remove unused managed outbound", "tag", tag, "error", err.Error())
		current, loadErr := s.store.LoadState()
		if loadErr != nil {
			return fmt.Errorf("remove outbound %s: %v; load cleanup state: %w", tag, err, loadErr)
		}
		now := s.currentTime().UTC()
		current.RuntimeOutbounds = updateRuntimeOutbound(current.RuntimeOutbounds, domain.RuntimeOutboundState{
			Tag: tag, Role: "draining", RetireAfter: now, RemoveBy: now.Add(30 * time.Minute),
		})
		if saveErr := s.saveState(current); saveErr != nil {
			return fmt.Errorf("remove outbound %s: %v; save cleanup state: %w", tag, err, saveErr)
		}
		return err
	}
	return nil
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

// tryManagedReserveFailover verifies and applies retained reserves in the same
// score order used by maintenance. The regular candidate search remains the
// fallback when no retained reserve succeeds.
func (s *Service) tryManagedReserveFailover(ctx context.Context, snapshot autoSelectionSnapshot, mode domain.SelectionMode, failureReason string) (switched, attempted bool, err error) {
	if _, ok := s.backend.(backend.ManagedBackend); !ok {
		return false, false, nil
	}
	now := s.currentTime().UTC()
	eligible := collectManagedReserveCandidates(snapshot.subscriptions, snapshot.settings, snapshot.state, now)
	byNode := make(map[string]managedReserveCandidate, len(eligible))
	for _, candidate := range eligible {
		byNode[domain.AutoExcludedNodeKey(candidate.sub.ID, candidate.node.ID)] = candidate
	}
	reserves := make([]managedReserveCandidate, 0, managedReserveLimit)
	for _, outbound := range snapshot.state.RuntimeOutbounds {
		if outbound.Role != "reserve" {
			continue
		}
		candidate, ok := byNode[domain.AutoExcludedNodeKey(outbound.SubscriptionID, outbound.NodeID)]
		if !ok {
			continue
		}
		if backoff, paused := snapshot.state.CandidateBackoff[outbound.Tag]; paused && now.Before(backoff.RetryAfter) {
			continue
		}
		candidate.tag = outbound.Tag
		candidate.verifiedAt = outbound.VerifiedAt
		reserves = append(reserves, candidate)
	}
	reserves = rankManagedReserveCandidates(reserves, snapshot.state.Health)
	if len(reserves) == 0 {
		return false, false, nil
	}

	err = runStoreWriteLocked(s, func() error {
		current, currentErr := s.autoSelectionSnapshotCurrentLocked(snapshot)
		if currentErr != nil {
			return currentErr
		}
		if !current {
			return errAutoSelectionSnapshotChanged
		}
		var lastErr error
		for _, reserve := range reserves {
			attempted = true
			// applyNodeSelection always verifies the dedicated outbound first. This
			// is deliberately stricter than the 60-second freshness requirement.
			options := selectionOptionsForState(snapshot.state)
			checkedAt := s.currentTime().UTC()
			verifiedAge := checkedAt.Sub(reserve.verifiedAt)
			options.skipManagedProbe = snapshot.state.OperationalMode != domain.OperationalModeDirect &&
				!reserve.verifiedAt.IsZero() && verifiedAge >= 0 && verifiedAge <= time.Minute
			if applyErr := s.applyNodeSelection(ctx, reserve.sub, reserve.node, mode, options); applyErr != nil {
				lastErr = applyErr
				continue
			}
			updated, loadErr := s.store.LoadState()
			if loadErr != nil {
				return loadErr
			}
			updated.Mode = mode
			if mode == domain.SelectionModeManual {
				updated.AutoScope = ""
			}
			updated.LastSwitchAt = s.currentTime().UTC()
			updated.LastSwitchReason = switchReason("emergency failover", activeNodeLabel(snapshot.subscriptions, snapshot.state), reserve.node, "verified reserve HTTPS GET; "+failureReason)
			updated.LastFailureReason = failureReason
			if saveErr := s.saveState(updated); saveErr != nil {
				return saveErr
			}
			s.logInfo("emergency failover applied", "from_node", activeNodeLabel(snapshot.subscriptions, snapshot.state), "to_node", nodeLabel(reserve.node), "result", "verified reserve HTTPS GET", "trigger", failureReason)
			switched = true
			return nil
		}
		return lastErr
	})
	return switched, attempted, err
}
