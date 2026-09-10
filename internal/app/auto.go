package app

import (
	"context"
	"fmt"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/probe"
)

const autoScopeAll = "all"

const activeGETFailureReasonPrefix = "active GET failed: "

type autoHealthContextKey uint8

const postFailoverOptimizationKey autoHealthContextKey = iota

func withPostFailoverOptimization(ctx context.Context) context.Context {
	return context.WithValue(ctx, postFailoverOptimizationKey, true)
}

func isPostFailoverOptimization(ctx context.Context) bool {
	requested, _ := ctx.Value(postFailoverOptimizationKey).(bool)
	return requested
}

type autoSelectionDecision struct {
	CurrentNodeID       string
	CandidateNode       domain.Node
	CandidateScore      domain.ScoreResult
	SelectedNode        domain.Node
	Health              map[string]domain.NodeHealth
	HasHealthyCandidate bool
	Switch              bool
	Reconnect           bool
	Reason              string
}

// RunAutoHealthCheck probes the active auto-mode subscription and reconnects when needed.
func (s *Service) RunAutoHealthCheck(ctx context.Context) error {
	snapshot, err := s.captureAutoSelectionSnapshot()
	if err != nil {
		return err
	}
	state := snapshot.state
	if state.ZapretTest.Active {
		return nil
	}
	if state.Mode != domain.SelectionModeAuto || state.ActiveSubscriptionID == "" {
		return nil
	}

	scope := state.ActiveSubscriptionID
	if state.AutoScope == autoScopeAll {
		scope = autoScopeAll
	}
	for _, sub := range snapshot.subscriptions {
		if sub.ID == state.ActiveSubscriptionID && sub.IsExpired(s.currentTime().UTC()) {
			scope = autoScopeAll
			break
		}
	}
	prepared, err := s.prepareAutoSelection(ctx, scope, snapshot)
	if err != nil {
		return err
	}

	return runStoreWriteLocked(s, func() error {
		current, err := s.autoSelectionSnapshotCurrentLocked(snapshot)
		if err != nil {
			return err
		}
		if !current {
			s.logDebug("auto health result discarded because runtime inputs changed during probes")
			return nil
		}
		return s.commitPreparedAutoHealthCheck(ctx, prepared)
	})
}

// RunAutoFailover immediately moves an unhealthy auto route to the best
// previously measured candidate. It intentionally skips the full pool probe;
// the scheduler starts that pass after this fast recovery attempt completes.
func (s *Service) RunAutoFailover(ctx context.Context, failureReason string) error {
	snapshot, err := s.captureAutoSelectionSnapshot()
	if err != nil {
		return err
	}
	state := snapshot.state
	if state.ZapretTest.Active || state.Mode != domain.SelectionModeAuto || state.ActiveSubscriptionID == "" {
		return nil
	}

	state.Health = cloneHealthMap(state.Health)
	if failureReason == "" {
		failureReason = "active auto route failed"
	}
	forceHealthFailure(
		state.Health,
		state.ActiveNodeID,
		failureReason,
		s.currentTime().UTC(),
		switchPolicyFromSettings(snapshot.settings).FailureThreshold,
	)

	scope := state.ActiveSubscriptionID
	if state.AutoScope == autoScopeAll {
		scope = autoScopeAll
	}
	for _, sub := range snapshot.subscriptions {
		if sub.ID == state.ActiveSubscriptionID && sub.IsExpired(s.currentTime().UTC()) {
			scope = autoScopeAll
			break
		}
	}

	failoverSnapshot := snapshot
	failoverSnapshot.state = state
	prepared, err := s.prepareAutoSelectionWithProbes(ctx, scope, failoverSnapshot, false)
	if err != nil {
		return err
	}
	if !prepared.decision.HasHealthyCandidate || prepared.decision.SelectedNode.ID == "" {
		return fmt.Errorf("no previously healthy auto candidate is available")
	}
	if prepared.decision.SelectedNode.ID == state.ActiveNodeID && prepared.selectedSub.ID == state.ActiveSubscriptionID {
		return fmt.Errorf("no different previously healthy auto candidate is available")
	}

	return runStoreWriteLocked(s, func() error {
		current, err := s.autoSelectionSnapshotCurrentLocked(snapshot)
		if err != nil {
			return err
		}
		if !current {
			return errAutoSelectionSnapshotChanged
		}
		_, err = s.commitPreparedAutoSelection(ctx, prepared)
		return err
	})
}

// AutoRecoveryNeeded checks the live selected route without changing it. A
// failed result is used by the daemon to trigger cached failover followed by a
// full background reselection.
func (s *Service) AutoRecoveryNeeded(ctx context.Context) (bool, string, error) {
	if s == nil || s.store == nil {
		return false, "", fmt.Errorf("store is not configured")
	}
	state, err := s.store.LoadState()
	if err != nil {
		return false, "", fmt.Errorf("load state: %w", err)
	}
	if state.Mode != domain.SelectionModeAuto || state.ZapretTest.Active {
		return false, "", nil
	}
	if !state.Connected || state.ActiveSubscriptionID == "" || state.ActiveNodeID == "" {
		return true, "auto route is disconnected", nil
	}
	sub, err := s.subscriptionByID(state.ActiveSubscriptionID)
	if err != nil {
		return true, "active subscription is missing", nil
	}
	if sub.IsExpired(s.currentTime().UTC()) {
		return true, "active subscription expired", nil
	}
	if s.backend != nil {
		status, statusErr := s.backend.Status(ctx)
		if statusErr != nil {
			return true, "backend status failed", nil
		}
		if !status.Running {
			return true, "backend is not running", nil
		}
	}
	if s.backendEgressProbe == nil {
		return false, "", nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.backendEgressProbe(probeCtx); err != nil {
		return true, fmt.Sprintf("%s%v", activeGETFailureReasonPrefix, err), nil
	}
	return false, "", nil
}

func (s *Service) commitPreparedAutoHealthCheck(ctx context.Context, prepared preparedAutoSelection) error {
	if prepared.all {
		return s.commitPreparedAutoHealthCheckAll(ctx, prepared)
	}

	settings := prepared.snapshot.settings
	persistedState := prepared.snapshot.persistedState
	state := prepared.snapshot.state
	sub := prepared.sub
	decision := prepared.decision

	s.logAutoDecision("auto health decision", sub, decision)

	if effectiveActiveTransport(state) == domain.TransportModeZapret {
		return s.handleZapretAutoHealthDecision(ctx, sub, settings, persistedState, state, decision)
	}

	if !decision.HasHealthyCandidate {
		if settings.Zapret.Enabled {
			return s.activateZapretFallback(ctx, sub, state, settings, decision.Reason)
		}
		return s.persistAutoFailure(ctx, sub, state, decision)
	}

	if !decision.Reconnect && !decision.Switch {
		state.Health = decision.Health
		state.LastFailureReason = ""
		if s.shouldPersistAutoHealthState(persistedState, state) {
			if err := s.saveState(state); err != nil {
				return fmt.Errorf("save state: %w", err)
			}
		} else {
			s.rememberAutoHealthState(state, false)
		}
		return nil
	}

	_, err := s.commitAutoSelection(ctx, sub, state, decision)
	return err
}

func (s *Service) commitPreparedAutoHealthCheckAll(ctx context.Context, prepared preparedAutoSelection) error {
	settings := prepared.snapshot.settings
	persistedState := prepared.snapshot.persistedState
	state := prepared.snapshot.state
	decision := prepared.decision
	selectedSub := prepared.selectedSub
	fallbackSub := prepared.fallbackSub
	s.logAutoDecision("global auto health decision", selectedSub, decision)

	if effectiveActiveTransport(state) == domain.TransportModeZapret {
		if selectedSub.ID == "" {
			selectedSub = fallbackSub
		}
		if err := s.handleZapretAutoHealthDecision(ctx, selectedSub, settings, persistedState, state, decision); err != nil {
			return err
		}
		return s.persistAutoScope(autoScopeAll)
	}

	if !decision.HasHealthyCandidate {
		if settings.Zapret.Enabled && fallbackSub.ID != "" {
			if err := s.activateZapretFallback(ctx, fallbackSub, state, settings, decision.Reason); err != nil {
				return err
			}
			return s.persistAutoScope(autoScopeAll)
		}
		if fallbackSub.ID == "" {
			state.Health = decision.Health
			state.Connected = false
			state.LastFailureReason = decision.Reason
			return s.saveState(state)
		}
		return s.persistGlobalAutoFailure(ctx, fallbackSub, state, decision)
	}

	if !decision.Reconnect && !decision.Switch {
		state.Health = decision.Health
		state.AutoScope = autoScopeAll
		state.LastFailureReason = ""
		if s.shouldPersistAutoHealthState(persistedState, state) {
			return s.saveState(state)
		}
		s.rememberAutoHealthState(state, false)
		return nil
	}

	if _, err := s.commitAutoSelection(ctx, selectedSub, state, decision); err != nil {
		return err
	}
	return s.persistAutoScope(autoScopeAll)
}

func (s *Service) evaluateAutoSelectionAll(ctx context.Context, subscriptions []domain.Subscription, settings domain.Settings, state domain.RuntimeState, runProbes bool) (autoSelectionDecision, domain.Subscription, domain.Subscription, error) {
	health := healthForSubscriptions(state.Health, subscriptions)
	if health == nil {
		health = make(map[string]domain.NodeHealth)
	}

	failureThreshold := switchPolicyFromSettings(settings).FailureThreshold
	activeTransport := effectiveActiveTransport(state)
	currentNodeID := ""
	if activeTransport == domain.TransportModeProxy {
		currentNodeID = state.ActiveNodeID
	}

	var fallbackSub domain.Subscription
	var currentSub domain.Subscription
	candidates := make([]domain.Node, 0)
	nodeSubscriptions := make(map[string]domain.Subscription)
	for _, sub := range subscriptions {
		if sub.IsExpired(s.currentTime().UTC()) {
			if sub.ID == state.ActiveSubscriptionID && currentNodeID != "" {
				forceHealthFailure(health, currentNodeID, "active subscription expired", s.currentTime().UTC(), failureThreshold)
			}
			continue
		}
		if fallbackSub.ID == "" && len(sub.Nodes) > 0 {
			fallbackSub = sub
		}
		if sub.ID == state.ActiveSubscriptionID {
			currentSub = sub
			fallbackSub = sub
		}

		selectable := autoSelectableNodes(sub, settings)
		if autoNodeExcluded(sub, currentNodeID, settings) && sub.ID == state.ActiveSubscriptionID {
			forceHealthFailure(health, currentNodeID, "current node is excluded from auto mode", s.currentTime().UTC(), failureThreshold)
		}
		if len(selectable) == 0 {
			continue
		}
		for _, node := range selectable {
			candidates = append(candidates, node)
			if _, exists := nodeSubscriptions[node.ID]; !exists {
				nodeSubscriptions[node.ID] = sub
			}
		}
	}

	if len(candidates) == 0 {
		return autoSelectionDecision{CurrentNodeID: currentNodeID, Health: health, Reason: "all nodes are excluded from auto mode"}, domain.Subscription{}, fallbackSub, nil
	}
	if runProbes {
		s.probeSubscription(ctx, domain.Subscription{ID: autoScopeAll, Nodes: candidates}, health, failureThreshold)
	}

	if runProbes && state.Connected && activeTransport == domain.TransportModeProxy && currentNodeID != "" {
		currentHealth := health[currentNodeID]
		if currentHealth.Healthy {
			if reason, err := s.ensureBackendEgress(ctx, settings, state.ActiveSubscriptionID, currentNodeID, domain.SelectionModeAuto); err != nil {
				forceHealthFailure(health, currentNodeID, reason, s.currentTime().UTC(), failureThreshold)
			}
		}
	}

	candidateNode, candidateScore, err := probe.SelectBestNode(candidates, health, probe.DefaultScoreConfig())
	if err != nil {
		return autoSelectionDecision{}, domain.Subscription{}, fallbackSub, err
	}
	selectedSub := nodeSubscriptions[candidateNode.ID]
	candidateHealth := health[candidateNode.ID]
	decision := autoSelectionDecision{
		CurrentNodeID:       currentNodeID,
		CandidateNode:       candidateNode,
		CandidateScore:      candidateScore,
		Health:              health,
		HasHealthyCandidate: candidateHealth.Healthy,
		Reason:              "no healthy nodes available",
	}
	if !candidateHealth.Healthy {
		return decision, selectedSub, fallbackSub, nil
	}

	if !state.Connected {
		decision.SelectedNode = candidateNode
		decision.Switch = candidateNode.ID != currentNodeID || selectedSub.ID != state.ActiveSubscriptionID
		decision.Reconnect = true
		decision.Reason = "recover disconnected global auto mode"
		return decision, selectedSub, fallbackSub, nil
	}

	currentHealth := health[currentNodeID]
	policy := switchPolicyFromSettings(settings)
	if isPostFailoverOptimization(ctx) {
		policy.Cooldown = 0
	}
	shouldSwitch, reason := probe.ShouldSwitch(currentHealth, candidateHealth, s.currentTime().UTC(), state.LastSwitchAt, policy)
	selectedNode := candidateNode
	if !shouldSwitch && currentNodeID != "" {
		if activeNode, ok := currentSub.NodeByID(currentNodeID); ok {
			selectedNode = activeNode
			selectedSub = currentSub
		} else {
			shouldSwitch = true
			reason = "current node missing"
		}
	}
	if shouldSwitch && reason == "" {
		reason = "candidate selected"
	}

	decision.SelectedNode = selectedNode
	decision.Switch = selectedNode.ID != currentNodeID || selectedSub.ID != state.ActiveSubscriptionID
	decision.Reason = reason
	return decision, selectedSub, fallbackSub, nil
}

func (s *Service) persistGlobalAutoFailure(ctx context.Context, sub domain.Subscription, state domain.RuntimeState, decision autoSelectionDecision) error {
	state.AutoScope = autoScopeAll
	if err := s.persistAutoFailure(ctx, sub, state, decision); err != nil {
		return err
	}
	return s.persistAutoScope(autoScopeAll)
}

func (s *Service) persistAutoScope(scope string) error {
	state, err := s.store.LoadState()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	state.AutoScope = scope
	if err := s.saveState(state); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	return nil
}

func (s *Service) evaluateAutoSelection(ctx context.Context, sub domain.Subscription, settings domain.Settings, state domain.RuntimeState, runProbes bool) (autoSelectionDecision, error) {
	health := cloneHealthMap(state.Health)
	if health == nil {
		health = make(map[string]domain.NodeHealth)
	}

	failureThreshold := switchPolicyFromSettings(settings).FailureThreshold
	activeTransport := effectiveActiveTransport(state)
	currentNodeID := ""
	if state.ActiveSubscriptionID == sub.ID && activeTransport == domain.TransportModeProxy {
		currentNodeID = state.ActiveNodeID
	}
	currentNodeExcluded := autoNodeExcluded(sub, currentNodeID, settings)

	candidateNodes := autoSelectableNodes(sub, settings)
	if len(candidateNodes) == 0 {
		return autoSelectionDecision{
			CurrentNodeID: currentNodeID,
			Health:        health,
			Reason:        "all nodes are excluded from auto mode",
		}, nil
	}

	if currentNodeExcluded {
		forceHealthFailure(health, currentNodeID, "current node is excluded from auto mode", s.currentTime().UTC(), failureThreshold)
	}

	if runProbes {
		probeSub := sub
		probeSub.Nodes = candidateNodes
		s.probeSubscription(ctx, probeSub, health, failureThreshold)
	}
	if runProbes && state.Connected && activeTransport == domain.TransportModeProxy && currentNodeID != "" {
		currentHealth := health[currentNodeID]
		if currentHealth.Healthy {
			if reason, err := s.ensureBackendEgress(ctx, settings, sub.ID, currentNodeID, domain.SelectionModeAuto); err != nil {
				forceHealthFailure(health, currentNodeID, reason, s.currentTime().UTC(), failureThreshold)
			}
		}
	}

	candidateNode, candidateScore, err := probe.SelectBestNode(candidateNodes, health, probe.DefaultScoreConfig())
	if err != nil {
		return autoSelectionDecision{}, err
	}

	candidateHealth := health[candidateNode.ID]
	decision := autoSelectionDecision{
		CurrentNodeID:       currentNodeID,
		CandidateNode:       candidateNode,
		CandidateScore:      candidateScore,
		Health:              health,
		HasHealthyCandidate: candidateHealth.Healthy,
		Reason:              "no healthy nodes available",
	}
	if !candidateHealth.Healthy {
		return decision, nil
	}

	if !state.Connected {
		decision.SelectedNode = candidateNode
		decision.Switch = candidateNode.ID != currentNodeID
		decision.Reconnect = true
		decision.Reason = "recover disconnected auto mode"
		return decision, nil
	}

	currentHealth := health[currentNodeID]
	policy := switchPolicyFromSettings(settings)
	if isPostFailoverOptimization(ctx) {
		policy.Cooldown = 0
	}
	shouldSwitch, reason := probe.ShouldSwitch(currentHealth, candidateHealth, time.Now().UTC(), state.LastSwitchAt, policy)

	selectedNode := candidateNode
	if !shouldSwitch && currentNodeID != "" {
		activeNode, ok := sub.NodeByID(currentNodeID)
		if ok {
			selectedNode = activeNode
		} else {
			shouldSwitch = true
			reason = "current node missing"
		}
	}

	if shouldSwitch && reason == "" {
		reason = "candidate selected"
	}

	decision.SelectedNode = selectedNode
	decision.Switch = selectedNode.ID != currentNodeID
	decision.Reason = reason
	return decision, nil
}

func autoSelectableNodes(sub domain.Subscription, settings domain.Settings) []domain.Node {
	if sub.IsExpired(time.Now().UTC()) {
		return nil
	}
	if len(settings.AutoExcludedNodes) == 0 && len(settings.AutoHideKeywords) == 0 {
		return sub.Nodes
	}

	nodes := make([]domain.Node, 0, len(sub.Nodes))
	for _, node := range sub.Nodes {
		if domain.IsNodeExcludedFromAuto(settings, sub.ID, node) {
			continue
		}
		nodes = append(nodes, node)
	}

	return nodes
}

func autoNodeExcluded(sub domain.Subscription, nodeID string, settings domain.Settings) bool {
	if nodeID == "" {
		return false
	}
	node, ok := sub.NodeByID(nodeID)
	if !ok {
		node = domain.Node{ID: nodeID}
	}
	return domain.IsNodeExcludedFromAuto(settings, sub.ID, node)
}

func (s *Service) commitAutoSelection(ctx context.Context, sub domain.Subscription, currentState domain.RuntimeState, decision autoSelectionDecision) (domain.Node, error) {
	previousTransport := effectiveActiveTransport(currentState)
	if err := s.applyNodeSelection(ctx, sub, decision.SelectedNode, domain.SelectionModeAuto, selectionOptionsForState(currentState)); err != nil {
		return domain.Node{}, err
	}

	state, err := s.store.LoadState()
	if err != nil {
		return domain.Node{}, fmt.Errorf("load state: %w", err)
	}

	state.Health = decision.Health
	state.Mode = domain.SelectionModeAuto
	state.Connected = true
	state.ActiveSubscriptionID = sub.ID
	state.ActiveNodeID = decision.SelectedNode.ID
	if previousTransport != domain.TransportModeProxy {
		state.LastTransportSwitchAt = s.currentTime().UTC()
	}
	state.ActiveTransport = domain.TransportModeProxy
	state.LastTransportFailureReason = ""
	if decision.Switch {
		state.LastSwitchAt = s.currentTime().UTC()
	}

	if err := s.saveState(state); err != nil {
		return domain.Node{}, fmt.Errorf("save state: %w", err)
	}

	return decision.SelectedNode, nil
}

func (s *Service) persistAutoFailure(ctx context.Context, sub domain.Subscription, state domain.RuntimeState, decision autoSelectionDecision) error {
	if state.Connected {
		failedNodeID := decision.CurrentNodeID
		if failedNodeID == "" {
			failedNodeID = state.ActiveNodeID
		}
		if err := s.markConnectionFailed(ctx, sub.ID, failedNodeID, domain.SelectionModeAuto, decision.Reason); err != nil {
			return err
		}

		reloaded, err := s.store.LoadState()
		if err != nil {
			return fmt.Errorf("load state: %w", err)
		}
		state = reloaded
	}

	state.Health = decision.Health
	state.Mode = domain.SelectionModeAuto
	state.Connected = false
	if state.ActiveSubscriptionID == "" {
		state.ActiveSubscriptionID = sub.ID
	}
	if state.ActiveNodeID == "" {
		state.ActiveNodeID = decision.CurrentNodeID
	}
	state.LastFailureReason = decision.Reason

	if err := s.saveState(state); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	return nil
}

func (s *Service) handleZapretAutoHealthDecision(
	ctx context.Context,
	sub domain.Subscription,
	settings domain.Settings,
	persistedState domain.RuntimeState,
	state domain.RuntimeState,
	decision autoSelectionDecision,
) error {
	state.ActiveTransport = domain.TransportModeZapret
	state.Mode = domain.SelectionModeAuto
	state.ActiveSubscriptionID = sub.ID
	state.Connected = true
	state.Health = decision.Health
	if state.LastFailureReason == "" {
		state.LastFailureReason = decision.Reason
	}

	if !decision.HasHealthyCandidate || !s.canFailbackFromZapret(settings, state, decision) {
		if s.shouldPersistAutoHealthState(persistedState, state) {
			if err := s.saveState(state); err != nil {
				return fmt.Errorf("save state: %w", err)
			}
		} else {
			s.rememberAutoHealthState(state, false)
		}
		return nil
	}

	_, err := s.commitAutoSelection(ctx, sub, state, decision)
	return err
}

func (s *Service) logAutoDecision(msg string, sub domain.Subscription, decision autoSelectionDecision) {
	selectedNodeID := decision.SelectedNode.ID
	if !decision.HasHealthyCandidate {
		selectedNodeID = ""
	}

	s.logInfo(
		msg,
		"subscription", sub.ID,
		"current_node", decision.CurrentNodeID,
		"candidate_node", decision.CandidateNode.ID,
		"selected_node", selectedNodeID,
		"switch", decision.Switch,
		"reconnect", decision.Reconnect,
		"reason", decision.Reason,
		"candidate_score", decision.CandidateScore.Score,
	)
}

func switchPolicyFromSettings(settings domain.Settings) probe.SwitchPolicy {
	policy := probe.DefaultSwitchPolicy()
	if settings.SwitchCooldown.Duration() >= 0 {
		policy.Cooldown = settings.SwitchCooldown.Duration()
	}
	if settings.LatencyThreshold.Duration() >= 0 {
		policy.LatencyImprovement = settings.LatencyThreshold.Duration()
	}
	return policy
}

func cloneHealthMap(source map[string]domain.NodeHealth) map[string]domain.NodeHealth {
	cloned := make(map[string]domain.NodeHealth, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func forceHealthFailure(health map[string]domain.NodeHealth, nodeID, reason string, checkedAt time.Time, failureThreshold int) {
	if health == nil || nodeID == "" {
		return
	}
	if failureThreshold < 1 {
		failureThreshold = 1
	}

	updated := health[nodeID]
	updated.NodeID = nodeID
	updated = probe.UpdateHealth(updated, false, updated.LastLatency.Duration(), checkedAt, reason, 1)
	if updated.ConsecutiveFailures < failureThreshold {
		updated.ConsecutiveFailures = failureThreshold
	}
	updated.Score = probe.CalculateScore(updated, probe.DefaultScoreConfig()).Score
	health[nodeID] = updated
}
