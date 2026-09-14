package app

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/backend"
	"github.com/design-maestro/fastlane/internal/domain"
)

type managedRecordingBackend struct {
	*recordingBackend
	selected     string
	selects      []string
	persistCalls int
	removed      []string
	removeErr    error
}

func (b *managedRecordingBackend) PrepareOutbound(_ context.Context, node domain.Node, _ int) (string, error) {
	return "fastlane-node-" + node.ID, nil
}
func (b *managedRecordingBackend) RemoveOutbound(_ context.Context, tag string) error {
	b.removed = append(b.removed, tag)
	return b.removeErr
}
func (b *managedRecordingBackend) SelectOutbound(_ context.Context, tag string) error {
	b.selected = tag
	b.selects = append(b.selects, tag)
	return nil
}
func (b *managedRecordingBackend) SelectDirect(ctx context.Context) error {
	return b.SelectOutbound(ctx, "fastlane-direct")
}
func (b *managedRecordingBackend) SelectedOutbound(context.Context) (string, error) {
	if b.selected == "" {
		return "", fmt.Errorf("no selected outbound")
	}
	return b.selected, nil
}
func (b *managedRecordingBackend) SetProbeOutbound(context.Context, int, string) error { return nil }
func (b *managedRecordingBackend) ClearProbeOutbound(context.Context, int) error       { return nil }
func (b *managedRecordingBackend) ProbeHTTPPort(slot int) (int, error)                 { return 10810 + slot, nil }
func (b *managedRecordingBackend) PersistConfig(context.Context, backend.ConfigRequest) error {
	b.persistCalls++
	return nil
}

func TestApplyNodeSelectionUsesManagedSwitchWithoutReloadingDNSOrFirewall(t *testing.T) {
	current := domain.Node{ID: "current", Protocol: domain.ProtocolSocks, Address: "192.0.2.1", Port: 1080}
	candidate := domain.Node{ID: "candidate", Protocol: domain.ProtocolSocks, Address: "192.0.2.2", Port: 1080}
	sub := domain.Subscription{ID: "sub", Nodes: []domain.Node{current, candidate}}
	settings := domain.DefaultSettings()
	store := &memoryStore{settings: settings, subs: []domain.Subscription{sub}, state: domain.DefaultRuntimeState()}
	store.state.Connected = true
	store.state.OperationalMode = domain.OperationalModeVPN
	store.state.ActiveTransport = domain.TransportModeProxy
	store.state.Mode = domain.SelectionModeManual
	store.state.ActiveSubscriptionID = sub.ID
	store.state.ActiveNodeID = current.ID
	base := &recordingBackend{}
	managed := &managedRecordingBackend{recordingBackend: base}
	dns := &recordingDNSManager{}
	firewall := &recordingFirewaller{}
	service := NewService(Dependencies{Store: store, Backend: managed, DNSManager: dns, Firewaller: firewall})
	service.managedOutboundProbe = func(context.Context, backend.ManagedBackend, int, string) error { return nil }

	if err := service.applyNodeSelection(context.Background(), sub, candidate, domain.SelectionModeManual, applyNodeSelectionOptions{}); err != nil {
		t.Fatalf("apply managed node: %v", err)
	}
	if len(base.requests) != 0 {
		t.Fatalf("managed switch reloaded the backend: %d", len(base.requests))
	}
	if dns.applyCalls != 0 || dns.disableCalls != 0 {
		t.Fatalf("managed switch touched DNS: apply=%d disable=%d", dns.applyCalls, dns.disableCalls)
	}
	if len(firewall.applied) != 0 || firewall.disableCalls != 0 {
		t.Fatalf("managed switch touched firewall: apply=%d disable=%d", len(firewall.applied), firewall.disableCalls)
	}
	if store.state.ActiveNodeID != candidate.ID || store.state.OperationalMode != domain.OperationalModeVPN {
		t.Fatalf("unexpected runtime state: %+v", store.state)
	}
	if store.state.SelectedOutboundTag != "fastlane-node-candidate" || store.state.CurrentOperation != nil {
		t.Fatalf("switch state was not committed: %+v", store.state)
	}
	if store.state.RuntimeConfigGeneration != 1 {
		t.Fatalf("runtime config generation = %d, want 1", store.state.RuntimeConfigGeneration)
	}
	if managed.persistCalls != 1 {
		t.Fatalf("restart config persist calls = %d, want 1", managed.persistCalls)
	}
}

func TestPermanentFirewallChangeUsesOneCoordinatedReload(t *testing.T) {
	node := domain.Node{ID: "current", Protocol: domain.ProtocolSocks, Address: "192.0.2.1", Port: 1080}
	sub := domain.Subscription{ID: "sub", Nodes: []domain.Node{node}}
	state := domain.DefaultRuntimeState()
	state.Connected = true
	state.OperationalMode = domain.OperationalModeVPN
	state.ActiveTransport = domain.TransportModeProxy
	state.Mode = domain.SelectionModeManual
	state.ActiveSubscriptionID = sub.ID
	state.ActiveNodeID = node.ID
	state.SelectedOutboundTag = "fastlane-node-current"
	store := &memoryStore{settings: domain.DefaultSettings(), state: state, subs: []domain.Subscription{sub}}
	base := &recordingBackend{}
	managed := &managedRecordingBackend{recordingBackend: base, selected: state.SelectedOutboundTag}
	firewall := &recordingFirewaller{}
	service := NewService(Dependencies{Store: store, Backend: managed, Firewaller: firewall})

	if _, err := service.ConfigureFirewall(context.Background(), []string{"1.1.1.1"}, true, 12345); err != nil {
		t.Fatalf("configure firewall: %v", err)
	}
	if len(base.requests) != 1 {
		t.Fatalf("permanent setting reloads = %d, want 1", len(base.requests))
	}
	if len(firewall.applied) != 1 {
		t.Fatalf("firewall apply calls = %d, want 1", len(firewall.applied))
	}
}

func TestActivateManagedDirectKeepsSelectionModeAndMarksFailOpen(t *testing.T) {
	state := domain.DefaultRuntimeState()
	state.Connected = true
	state.OperationalMode = domain.OperationalModeVPN
	state.ActiveTransport = domain.TransportModeProxy
	state.Mode = domain.SelectionModeManual
	state.ActiveSubscriptionID = "sub"
	state.ActiveNodeID = "node"
	store := &memoryStore{settings: domain.DefaultSettings(), state: state, subs: []domain.Subscription{{ID: "sub", Nodes: []domain.Node{{ID: "node", Protocol: domain.ProtocolSocks, Address: "192.0.2.3", Port: 1080}}}}}
	managed := &managedRecordingBackend{recordingBackend: &recordingBackend{}}
	service := NewService(Dependencies{Store: store, Backend: managed})

	if err := service.activateManagedDirect(context.Background(), managed, state, "all VPN candidates failed"); err != nil {
		t.Fatalf("activate direct: %v", err)
	}
	if store.state.OperationalMode != domain.OperationalModeDirect || store.state.Connected || store.state.ActiveTransport != domain.TransportModeDirect {
		t.Fatalf("direct state mismatch: %+v", store.state)
	}
	if store.state.Mode != domain.SelectionModeManual {
		t.Fatalf("manual mode was lost: %s", store.state.Mode)
	}
	if managed.selected != "fastlane-direct" {
		t.Fatalf("selected outbound = %q", managed.selected)
	}
	if managed.persistCalls != 1 {
		t.Fatalf("direct startup config was not persisted")
	}
}

func TestActivateManagedDirectPersistsWhenActiveNodeDisappeared(t *testing.T) {
	state := domain.DefaultRuntimeState()
	state.Connected = true
	state.OperationalMode = domain.OperationalModeVPN
	state.ActiveTransport = domain.TransportModeProxy
	state.Mode = domain.SelectionModeManual
	state.ActiveSubscriptionID = "sub"
	state.ActiveNodeID = "removed-node"
	store := &memoryStore{settings: domain.DefaultSettings(), state: state, subs: []domain.Subscription{{ID: "sub"}}}
	managed := &managedRecordingBackend{recordingBackend: &recordingBackend{}}
	service := NewService(Dependencies{Store: store, Backend: managed})

	if err := service.activateManagedDirect(context.Background(), managed, state, "active server was removed"); err != nil {
		t.Fatalf("activate direct after node removal: %v", err)
	}
	if store.state.OperationalMode != domain.OperationalModeDirect || managed.persistCalls != 1 {
		t.Fatalf("direct fallback was not persisted: state=%+v persists=%d", store.state, managed.persistCalls)
	}
}

func TestDirectRecoveryRequiresTwoSuccessfulManagedProbes(t *testing.T) {
	current := domain.Node{ID: "old", Protocol: domain.ProtocolSocks, Address: "192.0.2.1", Port: 1080}
	candidate := domain.Node{ID: "candidate", Protocol: domain.ProtocolSocks, Address: "192.0.2.2", Port: 1080}
	sub := domain.Subscription{ID: "sub", Nodes: []domain.Node{current, candidate}}
	state := domain.DefaultRuntimeState()
	state.OperationalMode = domain.OperationalModeDirect
	state.ActiveTransport = domain.TransportModeDirect
	state.Mode = domain.SelectionModeManual
	state.ActiveSubscriptionID = sub.ID
	state.ActiveNodeID = current.ID
	state.SelectedOutboundTag = "fastlane-direct"
	store := &memoryStore{settings: domain.DefaultSettings(), state: state, subs: []domain.Subscription{sub}}
	managed := &managedRecordingBackend{recordingBackend: &recordingBackend{}, selected: "fastlane-direct"}
	service := NewService(Dependencies{Store: store, Backend: managed})
	service.managedRecoveryDelay = time.Millisecond
	probeCalls := 0
	service.managedOutboundProbe = func(context.Context, backend.ManagedBackend, int, string) error {
		probeCalls++
		return nil
	}

	if err := service.applyNodeSelection(context.Background(), sub, candidate, domain.SelectionModeManual, applyNodeSelectionOptions{}); err != nil {
		t.Fatalf("recover direct route: %v", err)
	}
	if probeCalls != 2 {
		t.Fatalf("recovery probe calls = %d, want 2", probeCalls)
	}
	if store.state.OperationalMode != domain.OperationalModeVPN || store.state.ActiveNodeID != candidate.ID {
		t.Fatalf("VPN route was not restored: %+v", store.state)
	}
}

func TestCleanupManagedOutboundsWaitsForGraceAndProtectsSelected(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	state := domain.DefaultRuntimeState()
	state.RuntimeOutbounds = []domain.RuntimeOutboundState{
		{Tag: "old", Role: "draining", RetireAfter: now.Add(-time.Second)},
		{Tag: "selected", Role: "draining", RetireAfter: now.Add(-time.Hour)},
		{Tag: "young", Role: "draining", RetireAfter: now.Add(time.Minute)},
	}
	store := &memoryStore{settings: domain.DefaultSettings(), state: state}
	managed := &managedRecordingBackend{recordingBackend: &recordingBackend{}, selected: "selected"}
	service := NewService(Dependencies{Store: store, Backend: managed})
	service.now = func() time.Time { return now }
	if err := service.CleanupManagedOutbounds(context.Background()); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if !reflect.DeepEqual(managed.removed, []string{"old"}) {
		t.Fatalf("removed = %v", managed.removed)
	}
	if len(store.state.RuntimeOutbounds) != 2 {
		t.Fatalf("retained outbounds = %+v", store.state.RuntimeOutbounds)
	}
}

func TestMaintainManagedReservesChecksTwoCandidatesWithoutSwitching(t *testing.T) {
	nodes := []domain.Node{
		{ID: "active", Protocol: domain.ProtocolSocks, Address: "192.0.2.1", Port: 1080},
		{ID: "reserve-1", Protocol: domain.ProtocolSocks, Address: "192.0.2.2", Port: 1080},
		{ID: "reserve-2", Protocol: domain.ProtocolSocks, Address: "192.0.2.3", Port: 1080},
		{ID: "reserve-3", Protocol: domain.ProtocolSocks, Address: "192.0.2.4", Port: 1080},
	}
	state := domain.DefaultRuntimeState()
	state.Connected = true
	state.OperationalMode = domain.OperationalModeVPN
	state.ActiveTransport = domain.TransportModeProxy
	state.Mode = domain.SelectionModeManual
	state.ActiveSubscriptionID = "sub"
	state.ActiveNodeID = "active"
	store := &memoryStore{settings: domain.DefaultSettings(), state: state, subs: []domain.Subscription{{ID: "sub", Nodes: nodes}}}
	managed := &managedRecordingBackend{recordingBackend: &recordingBackend{}, selected: "fastlane-node-active"}
	service := NewService(Dependencies{Store: store, Backend: managed})
	entered := make(chan int, 3)
	release := make(chan struct{})
	service.managedOutboundProbe = func(_ context.Context, _ backend.ManagedBackend, slot int, _ string) error {
		entered <- slot
		<-release
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- service.MaintainManagedReserves(context.Background()) }()
	first, second := <-entered, <-entered
	if first == second {
		t.Fatalf("probe slots were not isolated: %d and %d", first, second)
	}
	select {
	case third := <-entered:
		t.Fatalf("more than two probes ran concurrently: slot %d", third)
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("maintain reserves: %v", err)
	}
	reserves := 0
	for _, outbound := range store.state.RuntimeOutbounds {
		if outbound.Role == "reserve" {
			reserves++
		}
	}
	if reserves != 2 {
		t.Fatalf("reserve count = %d, want 2: %+v", reserves, store.state.RuntimeOutbounds)
	}
	if len(managed.selects) != 0 {
		t.Fatalf("reserve check changed active route: %v", managed.selects)
	}
}

func TestMaintainManagedReservesChecksPreviousActiveNodeInDirectMode(t *testing.T) {
	node := domain.Node{ID: "only-node", Protocol: domain.ProtocolSocks, Address: "192.0.2.1", Port: 1080}
	state := domain.DefaultRuntimeState()
	state.OperationalMode = domain.OperationalModeDirect
	state.ActiveTransport = domain.TransportModeDirect
	state.Mode = domain.SelectionModeManual
	state.ActiveSubscriptionID = "sub"
	state.ActiveNodeID = node.ID
	state.SelectedOutboundTag = "fastlane-direct"
	store := &memoryStore{settings: domain.DefaultSettings(), state: state, subs: []domain.Subscription{{ID: "sub", Nodes: []domain.Node{node}}}}
	managed := &managedRecordingBackend{recordingBackend: &recordingBackend{}, selected: "fastlane-direct"}
	service := NewService(Dependencies{Store: store, Backend: managed})
	probed := ""
	service.managedOutboundProbe = func(_ context.Context, _ backend.ManagedBackend, _ int, tag string) error {
		probed = tag
		return nil
	}

	if err := service.MaintainManagedReserves(context.Background()); err != nil {
		t.Fatalf("maintain direct-mode reserves: %v", err)
	}
	if probed != "fastlane-node-only-node" {
		t.Fatalf("previous active node was not rechecked: %q", probed)
	}
}

func TestManagedFailoverUsesHighestRankedReserveAndKeepsManualMode(t *testing.T) {
	current := domain.Node{ID: "current", Protocol: domain.ProtocolSocks, Address: "192.0.2.1", Port: 1080}
	slower := domain.Node{ID: "slower", Protocol: domain.ProtocolSocks, Address: "192.0.2.2", Port: 1080}
	faster := domain.Node{ID: "faster", Protocol: domain.ProtocolSocks, Address: "192.0.2.3", Port: 1080}
	sub := domain.Subscription{ID: "sub", Nodes: []domain.Node{current, slower, faster}}
	state := domain.DefaultRuntimeState()
	state.Connected = true
	state.OperationalMode = domain.OperationalModeVPN
	state.ActiveTransport = domain.TransportModeProxy
	state.Mode = domain.SelectionModeManual
	state.ActiveSubscriptionID = sub.ID
	state.ActiveNodeID = current.ID
	state.SelectedOutboundTag = "fastlane-node-current"
	state.Health = map[string]domain.NodeHealth{
		slower.ID: reserveHealth(true, 200*time.Millisecond),
		faster.ID: reserveHealth(true, 50*time.Millisecond),
	}
	state.RuntimeOutbounds = []domain.RuntimeOutboundState{
		{Tag: "fastlane-node-current", SubscriptionID: sub.ID, NodeID: current.ID, Role: "active"},
		{Tag: "fastlane-node-slower", SubscriptionID: sub.ID, NodeID: slower.ID, Role: "reserve", VerifiedAt: time.Now().UTC()},
		{Tag: "fastlane-node-faster", SubscriptionID: sub.ID, NodeID: faster.ID, Role: "reserve", VerifiedAt: time.Now().UTC()},
	}
	store := &memoryStore{settings: domain.DefaultSettings(), state: state, subs: []domain.Subscription{sub}}
	managed := &managedRecordingBackend{recordingBackend: &recordingBackend{}, selected: state.SelectedOutboundTag}
	service := NewService(Dependencies{Store: store, Backend: managed})
	probed := make([]string, 0, 1)
	service.managedOutboundProbe = func(_ context.Context, _ backend.ManagedBackend, _ int, tag string) error {
		probed = append(probed, tag)
		return nil
	}
	snapshot, err := service.captureAutoSelectionSnapshot()
	if err != nil {
		t.Fatalf("capture snapshot: %v", err)
	}

	switched, attempted, err := service.tryManagedReserveFailover(context.Background(), snapshot, domain.SelectionModeManual, "active failed")
	if err != nil {
		t.Fatalf("reserve failover: %v", err)
	}
	if !switched || !attempted || len(probed) != 0 {
		t.Fatalf("unexpected reserve attempt: switched=%t attempted=%t probed=%v", switched, attempted, probed)
	}
	if store.state.ActiveNodeID != faster.ID || store.state.Mode != domain.SelectionModeManual {
		t.Fatalf("manual failover state = %+v", store.state)
	}
}

func TestMaintainManagedReservesLimitsCycleToFourProbes(t *testing.T) {
	nodes := []domain.Node{{ID: "active", Protocol: domain.ProtocolSocks, Address: "192.0.2.1", Port: 1080}}
	for index := 2; index <= 8; index++ {
		nodes = append(nodes, domain.Node{ID: fmt.Sprintf("node-%d", index), Protocol: domain.ProtocolSocks, Address: fmt.Sprintf("192.0.2.%d", index), Port: 1080})
	}
	state := domain.DefaultRuntimeState()
	state.Connected = true
	state.OperationalMode = domain.OperationalModeVPN
	state.ActiveTransport = domain.TransportModeProxy
	state.Mode = domain.SelectionModeAuto
	state.ActiveSubscriptionID = "sub"
	state.ActiveNodeID = "active"
	store := &memoryStore{settings: domain.DefaultSettings(), state: state, subs: []domain.Subscription{{ID: "sub", Nodes: nodes}}}
	managed := &managedRecordingBackend{recordingBackend: &recordingBackend{}, selected: "fastlane-node-active"}
	service := NewService(Dependencies{Store: store, Backend: managed})
	var probes atomic.Int32
	service.managedOutboundProbe = func(context.Context, backend.ManagedBackend, int, string) error {
		probes.Add(1)
		return nil
	}

	if err := service.MaintainManagedReserves(context.Background()); err != nil {
		t.Fatalf("maintain reserves: %v", err)
	}
	if probes.Load() != managedReserveProbeLimit {
		t.Fatalf("probe count = %d, want %d", probes.Load(), managedReserveProbeLimit)
	}
}

func TestConnectionFailoverFallsBackDirectAfterReserveFailure(t *testing.T) {
	current := domain.Node{ID: "current", Protocol: domain.ProtocolSocks, Address: "192.0.2.1", Port: 1080}
	reserve := domain.Node{ID: "reserve", Protocol: domain.ProtocolSocks, Address: "192.0.2.2", Port: 1080}
	sub := domain.Subscription{ID: "sub", Nodes: []domain.Node{current, reserve}}
	state := domain.DefaultRuntimeState()
	state.Connected = true
	state.OperationalMode = domain.OperationalModeVPN
	state.ActiveTransport = domain.TransportModeProxy
	state.Mode = domain.SelectionModeManual
	state.ActiveSubscriptionID = sub.ID
	state.ActiveNodeID = current.ID
	state.SelectedOutboundTag = "fastlane-node-current"
	state.Health = map[string]domain.NodeHealth{reserve.ID: reserveHealth(true, 50*time.Millisecond)}
	state.RuntimeOutbounds = []domain.RuntimeOutboundState{
		{Tag: state.SelectedOutboundTag, SubscriptionID: sub.ID, NodeID: current.ID, Role: "active"},
		{Tag: "fastlane-node-reserve", SubscriptionID: sub.ID, NodeID: reserve.ID, Role: "reserve", VerifiedAt: time.Now().UTC().Add(-2 * time.Minute)},
	}
	store := &memoryStore{settings: domain.DefaultSettings(), state: state, subs: []domain.Subscription{sub}}
	managed := &managedRecordingBackend{recordingBackend: &recordingBackend{}, selected: state.SelectedOutboundTag}
	service := NewService(Dependencies{Store: store, Backend: managed})
	service.managedOutboundProbe = func(context.Context, backend.ManagedBackend, int, string) error {
		return fmt.Errorf("reserve unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := service.RunConnectionFailover(ctx, "active failed"); err != nil {
		t.Fatalf("fail open: %v", err)
	}
	if store.state.OperationalMode != domain.OperationalModeDirect || store.state.Connected || managed.selected != "fastlane-direct" {
		t.Fatalf("direct fallback state = %+v, selected=%s", store.state, managed.selected)
	}
}

func TestSafeRemoveManagedOutboundProtectsSelectedAndTracksFailure(t *testing.T) {
	store := &memoryStore{settings: domain.DefaultSettings(), state: domain.DefaultRuntimeState()}
	managed := &managedRecordingBackend{recordingBackend: &recordingBackend{}, selected: "selected"}
	service := NewService(Dependencies{Store: store, Backend: managed})
	if err := service.safeRemoveManagedOutbound(context.Background(), managed, "selected"); err != nil {
		t.Fatalf("protect selected outbound: %v", err)
	}
	if len(managed.removed) != 0 {
		t.Fatalf("selected outbound was removed: %v", managed.removed)
	}

	managed.removeErr = fmt.Errorf("xray API unavailable")
	if err := service.safeRemoveManagedOutbound(context.Background(), managed, "orphan"); err == nil {
		t.Fatal("expected outbound removal error")
	}
	if !hasRuntimeRole(store.state.RuntimeOutbounds, "orphan", "draining") {
		t.Fatalf("failed removal was not tracked: %+v", store.state.RuntimeOutbounds)
	}
}

func TestManagedCandidateBackoffCapsAtThirtyMinutes(t *testing.T) {
	t.Parallel()
	want := []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 30 * time.Minute, 30 * time.Minute}
	for idx, expected := range want {
		if got := managedCandidateBackoff(idx + 1); got != expected {
			t.Fatalf("failure %d backoff = %s, want %s", idx+1, got, expected)
		}
	}
}
