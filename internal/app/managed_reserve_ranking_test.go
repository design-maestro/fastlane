package app

import (
	"reflect"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
)

func TestRankManagedReserveCandidatesHealthyHistoryBeforeUnknown(t *testing.T) {
	candidates := []managedReserveCandidate{
		reserveCandidate("sub-a", "unknown"),
		reserveCandidate("sub-b", "healthy"),
		reserveCandidate("sub-c", "unhealthy"),
	}
	health := map[string]domain.NodeHealth{
		"healthy":   reserveHealth(true, 350*time.Millisecond),
		"unhealthy": reserveHealth(false, 20*time.Millisecond),
	}

	got := rankedReserveNodeIDs(rankManagedReserveCandidates(candidates, health))
	want := []string{"healthy", "unknown", "unhealthy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ranked node IDs = %v, want %v", got, want)
	}
}

func TestRankManagedReserveCandidatesIsIndependentOfInputOrder(t *testing.T) {
	forward := []managedReserveCandidate{
		reserveCandidate("sub-b", "node-b"),
		reserveCandidate("sub-a", "node-c"),
		reserveCandidate("sub-a", "node-a"),
	}
	reversed := []managedReserveCandidate{forward[2], forward[1], forward[0]}
	health := map[string]domain.NodeHealth{
		"node-a": reserveHealth(true, 80*time.Millisecond),
		"node-b": reserveHealth(true, 80*time.Millisecond),
		"node-c": reserveHealth(true, 40*time.Millisecond),
	}

	gotForward := rankedReserveNodeIDs(rankManagedReserveCandidates(forward, health))
	gotReversed := rankedReserveNodeIDs(rankManagedReserveCandidates(reversed, health))
	want := []string{"node-c", "node-b", "node-a"}
	if !reflect.DeepEqual(gotForward, want) {
		t.Fatalf("forward ranking = %v, want %v", gotForward, want)
	}
	if !reflect.DeepEqual(gotReversed, want) {
		t.Fatalf("reversed ranking = %v, want %v", gotReversed, want)
	}
}

func TestRankManagedReserveCandidatesPrefersDifferentSubscriptionWithin100Milliseconds(t *testing.T) {
	candidates := []managedReserveCandidate{
		reserveCandidate("sub-a", "best"),
		reserveCandidate("sub-a", "same-sub"),
		reserveCandidate("sub-b", "other-sub"),
	}
	health := map[string]domain.NodeHealth{
		"best":      reserveHealth(true, 20*time.Millisecond),
		"same-sub":  reserveHealth(true, 40*time.Millisecond),
		"other-sub": reserveHealth(true, 140*time.Millisecond),
	}

	got := rankedReserveNodeIDs(rankManagedReserveCandidates(candidates, health))
	want := []string{"best", "other-sub", "same-sub"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ranked node IDs = %v, want %v", got, want)
	}
}

func TestRankManagedReserveCandidatesKeepsFasterSecondOutsideEquivalence(t *testing.T) {
	candidates := []managedReserveCandidate{
		reserveCandidate("sub-a", "best"),
		reserveCandidate("sub-a", "same-sub"),
		reserveCandidate("sub-b", "other-sub"),
	}
	health := map[string]domain.NodeHealth{
		"best":      reserveHealth(true, 20*time.Millisecond),
		"same-sub":  reserveHealth(true, 40*time.Millisecond),
		"other-sub": reserveHealth(true, 141*time.Millisecond),
	}

	got := rankedReserveNodeIDs(rankManagedReserveCandidates(candidates, health))
	want := []string{"best", "same-sub", "other-sub"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ranked node IDs = %v, want %v", got, want)
	}
}

func TestRankManagedReserveCandidatesBalancesReliabilityAndSpeed(t *testing.T) {
	candidates := []managedReserveCandidate{
		reserveCandidate("sub-a", "fast-unreliable"),
		reserveCandidate("sub-b", "steady"),
	}
	fastUnreliable := reserveHealth(true, 30*time.Millisecond)
	fastUnreliable.SuccessCount = 1
	fastUnreliable.FailureCount = 1
	steady := reserveHealth(true, 180*time.Millisecond)
	steady.SuccessCount = 10

	health := map[string]domain.NodeHealth{
		"fast-unreliable": fastUnreliable,
		"steady":          steady,
	}

	got := rankedReserveNodeIDs(rankManagedReserveCandidates(candidates, health))
	want := []string{"steady", "fast-unreliable"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ranked node IDs = %v, want %v", got, want)
	}
}

func TestCollectManagedReserveCandidatesExcludesAndDeduplicatesConnections(t *testing.T) {
	now := time.Date(2026, 9, 14, 18, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Minute)
	shared := domain.Node{ID: "shared-a", Name: "Old name", Protocol: domain.ProtocolSocks, Address: "192.0.2.10", Port: 1080}
	renamed := shared
	renamed.ID = "shared-b"
	renamed.Name = "New name"
	hidden := domain.Node{ID: "hidden", Name: "Russia 1", Protocol: domain.ProtocolSocks, Address: "192.0.2.11", Port: 1080}
	active := domain.Node{ID: "active", Protocol: domain.ProtocolSocks, Address: "192.0.2.12", Port: 1080}
	subscriptions := []domain.Subscription{
		{ID: "a", Nodes: []domain.Node{active, shared, hidden}},
		{ID: "b", Nodes: []domain.Node{renamed}},
		{ID: "expired", ExpiresAt: &expired, Nodes: []domain.Node{{ID: "expired-node"}}},
	}
	settings := domain.DefaultSettings()
	settings.AutoHideKeywords = []string{"Россия", "Russia"}
	state := domain.DefaultRuntimeState()
	state.OperationalMode = domain.OperationalModeVPN
	state.ActiveSubscriptionID = "a"
	state.ActiveNodeID = "active"

	got := collectManagedReserveCandidates(subscriptions, settings, state, now)
	if len(got) != 1 || got[0].node.ID != "shared-a" {
		t.Fatalf("eligible candidates = %+v, want one deduplicated connection", got)
	}
}

func TestSelectManagedReserveStatesRequiresTwoMeaningfulWins(t *testing.T) {
	now := time.Date(2026, 9, 14, 18, 0, 0, 0, time.UTC)
	first := reserveCandidate("sub-a", "first")
	second := reserveCandidate("sub-a", "second")
	challenger := reserveCandidate("sub-b", "challenger")
	health := map[string]domain.NodeHealth{
		"first":      reserveHealth(true, 100*time.Millisecond),
		"second":     reserveHealth(true, 300*time.Millisecond),
		"challenger": reserveHealth(true, 100*time.Millisecond),
	}
	previous := []domain.RuntimeOutboundState{
		{Tag: first.tag, SubscriptionID: first.sub.ID, NodeID: first.node.ID, Role: "reserve"},
		{Tag: second.tag, SubscriptionID: second.sub.ID, NodeID: second.node.ID, Role: "reserve"},
	}

	cycleOne := selectManagedReserveStates([]managedReserveCandidate{first, second, challenger}, health, previous, now)
	if !hasRuntimeRole(cycleOne, second.tag, "reserve") || !hasRuntimeRole(cycleOne, challenger.tag, "candidate") {
		t.Fatalf("first cycle replaced reserve too early: %+v", cycleOne)
	}
	cycleTwo := selectManagedReserveStates([]managedReserveCandidate{first, second, challenger}, health, cycleOne, now.Add(time.Minute))
	if hasRuntimeRole(cycleTwo, second.tag, "reserve") || !hasRuntimeRole(cycleTwo, challenger.tag, "reserve") {
		t.Fatalf("second confirmed cycle did not promote challenger: %+v", cycleTwo)
	}
}

func TestSelectManagedReserveStatesKeepsHealthyReserveWhenReplacementFails(t *testing.T) {
	now := time.Date(2026, 9, 14, 18, 0, 0, 0, time.UTC)
	first := reserveCandidate("sub-a", "first")
	second := reserveCandidate("sub-b", "second")
	failed := reserveCandidate("sub-c", "failed")
	health := map[string]domain.NodeHealth{
		"first":  reserveHealth(true, 100*time.Millisecond),
		"second": reserveHealth(true, 120*time.Millisecond),
		"failed": reserveHealth(false, 10*time.Millisecond),
	}
	previous := []domain.RuntimeOutboundState{
		{Tag: first.tag, SubscriptionID: first.sub.ID, NodeID: first.node.ID, Role: "reserve"},
		{Tag: second.tag, SubscriptionID: second.sub.ID, NodeID: second.node.ID, Role: "reserve"},
		{Tag: failed.tag, SubscriptionID: failed.sub.ID, NodeID: failed.node.ID, Role: "candidate", PromotionWins: 1},
	}

	got := selectManagedReserveStates([]managedReserveCandidate{first, second}, health, previous, now)
	if !hasRuntimeRole(got, first.tag, "reserve") || !hasRuntimeRole(got, second.tag, "reserve") || hasRuntimeRole(got, failed.tag, "candidate") {
		t.Fatalf("failed replacement changed healthy reserves: %+v", got)
	}
}

func TestManagedReserveProbeOrderEventuallyMeasuresUnknownCandidates(t *testing.T) {
	unknown := []managedReserveCandidate{
		reserveCandidate("sub", "unknown-a"),
		reserveCandidate("sub", "unknown-b"),
		reserveCandidate("sub", "unknown-c"),
	}
	measured := []managedReserveCandidate{
		reserveCandidate("sub", "measured-a"),
		reserveCandidate("sub", "measured-b"),
		reserveCandidate("sub", "measured-c"),
	}
	health := map[string]domain.NodeHealth{
		"measured-a": reserveHealth(true, 20*time.Millisecond),
		"measured-b": reserveHealth(true, 30*time.Millisecond),
		"measured-c": reserveHealth(true, 40*time.Millisecond),
	}
	ranked := append(measured, unknown...)
	seen := make(map[string]bool)
	base := time.Unix(0, 0).UTC()
	for minute := 0; minute < len(unknown); minute++ {
		order := managedReserveProbeOrder(nil, ranked, health, base.Add(time.Duration(minute)*time.Minute))
		for _, candidate := range order[:managedReserveProbeLimit] {
			if _, known := health[candidate.node.ID]; !known {
				seen[candidate.node.ID] = true
			}
		}
	}
	if len(seen) != len(unknown) {
		t.Fatalf("unknown rotation covered %v, want all candidates", seen)
	}
}

func TestSelectManagedReserveStatesConfirmsProviderDiversityUpgrade(t *testing.T) {
	now := time.Date(2026, 9, 14, 18, 0, 0, 0, time.UTC)
	first := reserveCandidate("sub-a", "first")
	second := reserveCandidate("sub-a", "second")
	diverse := reserveCandidate("sub-b", "diverse")
	health := map[string]domain.NodeHealth{
		"first":   reserveHealth(true, 50*time.Millisecond),
		"second":  reserveHealth(true, 100*time.Millisecond),
		"diverse": reserveHealth(true, 180*time.Millisecond),
	}
	previous := []domain.RuntimeOutboundState{
		{Tag: first.tag, SubscriptionID: first.sub.ID, NodeID: first.node.ID, Role: "reserve"},
		{Tag: second.tag, SubscriptionID: second.sub.ID, NodeID: second.node.ID, Role: "reserve"},
	}
	cycleOne := selectManagedReserveStates([]managedReserveCandidate{first, second, diverse}, health, previous, now)
	cycleTwo := selectManagedReserveStates([]managedReserveCandidate{first, second, diverse}, health, cycleOne, now.Add(time.Minute))
	if !hasRuntimeRole(cycleOne, diverse.tag, "candidate") || !hasRuntimeRole(cycleTwo, diverse.tag, "reserve") {
		t.Fatalf("provider diversity was not confirmed across two cycles: first=%+v second=%+v", cycleOne, cycleTwo)
	}
}

func reserveCandidate(subscriptionID, nodeID string) managedReserveCandidate {
	return managedReserveCandidate{
		sub:  domain.Subscription{ID: subscriptionID},
		node: domain.Node{ID: nodeID, SubscriptionID: subscriptionID},
		tag:  "tag-" + nodeID,
	}
}

func reserveHealth(healthy bool, latency time.Duration) domain.NodeHealth {
	return domain.NodeHealth{
		Healthy:        healthy,
		LastLatency:    domain.Duration(latency),
		AverageLatency: domain.Duration(latency),
	}
}

func rankedReserveNodeIDs(candidates []managedReserveCandidate) []string {
	ids := make([]string, len(candidates))
	for i := range candidates {
		ids[i] = candidates[i].node.ID
	}
	return ids
}

func hasRuntimeRole(states []domain.RuntimeOutboundState, tag, role string) bool {
	for _, state := range states {
		if state.Tag == tag && state.Role == role {
			return true
		}
	}
	return false
}
