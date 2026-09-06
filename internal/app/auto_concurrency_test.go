package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/probe"
)

func TestAutoProbeConcurrencyContract(t *testing.T) {
	t.Parallel()

	if autoProbeConcurrency != 10 {
		t.Fatalf("auto probe concurrency = %d, want 10", autoProbeConcurrency)
	}
}

func TestProbeSubscriptionUsesBoundedConcurrency(t *testing.T) {
	t.Parallel()

	nodes := make([]domain.Node, autoProbeConcurrency+5)
	for index := range nodes {
		nodes[index] = domain.Node{
			ID:      fmt.Sprintf("node-%02d", index),
			Address: fmt.Sprintf("192.0.2.%d", index+1),
			Port:    443,
		}
	}
	checker := &gatedProbeChecker{
		started: make(chan struct{}, len(nodes)),
		release: make(chan struct{}),
	}
	service := NewService(Dependencies{Store: &memoryStore{}, Checker: checker})
	done := make(chan []probe.Result, 1)
	go func() {
		done <- service.probeSubscription(context.Background(), domain.Subscription{ID: "pool", Nodes: nodes}, make(map[string]domain.NodeHealth), 1)
	}()

	for index := 0; index < autoProbeConcurrency; index++ {
		select {
		case <-checker.started:
		case <-time.After(time.Second):
			t.Fatalf("only %d probes started before timeout", index)
		}
	}
	select {
	case <-checker.started:
		t.Fatalf("more than %d probes started concurrently", autoProbeConcurrency)
	case <-time.After(25 * time.Millisecond):
	}

	close(checker.release)
	select {
	case results := <-done:
		if len(results) != len(nodes) {
			t.Fatalf("expected %d results, got %d", len(nodes), len(results))
		}
	case <-time.After(time.Second):
		t.Fatal("parallel probe batch did not finish")
	}
	if got := checker.maxInFlight.Load(); got != autoProbeConcurrency {
		t.Fatalf("expected peak concurrency %d, got %d", autoProbeConcurrency, got)
	}
}

func TestProbeSubscriptionReportsEachCompletedResult(t *testing.T) {
	t.Parallel()

	nodes := []domain.Node{
		{ID: "node-1", Address: "192.0.2.1", Port: 443},
		{ID: "node-2", Address: "192.0.2.2", Port: 443},
		{ID: "node-3", Address: "192.0.2.3", Port: 443},
	}
	checker := &countingProbeChecker{
		results: map[string]probe.Result{
			"node-1": {Healthy: true, Latency: 41 * time.Millisecond, Checked: time.Now().UTC()},
			"node-2": {Healthy: false, Err: errors.New("GET timeout"), Checked: time.Now().UTC()},
			"node-3": {Healthy: true, Latency: 63 * time.Millisecond, Checked: time.Now().UTC()},
		},
		counts: make(map[string]int),
	}
	service := NewService(Dependencies{Store: &memoryStore{}, Checker: checker})
	seen := make(map[string]domain.NodeHealth)
	ctx := WithAutoHealthProgress(context.Background(), func(health domain.NodeHealth) {
		seen[health.NodeID] = health
	})

	results := service.probeSubscription(ctx, domain.Subscription{ID: "pool", Nodes: nodes}, make(map[string]domain.NodeHealth), 1)
	if len(results) != len(nodes) || len(seen) != len(nodes) {
		t.Fatalf("completed results=%d progress events=%d, want %d", len(results), len(seen), len(nodes))
	}
	if !seen["node-1"].Healthy || seen["node-1"].LastLatency.Duration() != 41*time.Millisecond {
		t.Fatalf("unexpected successful progress result: %+v", seen["node-1"])
	}
	if seen["node-2"].Healthy || seen["node-2"].LastFailureReason == "" {
		t.Fatalf("unexpected failed progress result: %+v", seen["node-2"])
	}
}

func TestRunAutoHealthCheckDoesNotHoldStoreLockWhileProbing(t *testing.T) {
	t.Parallel()

	node := domain.Node{ID: "node-1", SubscriptionID: "sub-1", Address: "192.0.2.1", Port: 443}
	baseStore := &memoryStore{
		subs: []domain.Subscription{{ID: "sub-1", Nodes: []domain.Node{node}}},
		settings: func() domain.Settings {
			settings := domain.DefaultSettings()
			settings.AutoMode = true
			settings.Mode = domain.SelectionModeAuto
			settings.StrictEgressCheck = false
			return settings
		}(),
		state: domain.RuntimeState{
			SchemaVersion:        domain.DefaultRuntimeState().SchemaVersion,
			ActiveSubscriptionID: "sub-1",
			ActiveNodeID:         node.ID,
			Mode:                 domain.SelectionModeAuto,
			Connected:            true,
			ActiveTransport:      domain.TransportModeProxy,
			Health:               map[string]domain.NodeHealth{},
			LastRefreshAt:        map[string]time.Time{},
		},
	}
	store := &serializedMemoryStore{memoryStore: baseStore}
	checker := &gatedProbeChecker{started: make(chan struct{}, 1), release: make(chan struct{})}
	service := NewService(Dependencies{Store: store, Checker: checker})

	healthDone := make(chan error, 1)
	go func() { healthDone <- service.RunAutoHealthCheck(context.Background()) }()
	select {
	case <-checker.started:
	case <-time.After(time.Second):
		t.Fatal("auto health probe did not start")
	}

	settingDone := make(chan error, 1)
	go func() {
		_, err := service.SetSetting("health-check-interval", "2m")
		settingDone <- err
	}()
	select {
	case err := <-settingDone:
		if err != nil {
			t.Fatalf("update setting while probe runs: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("settings update was blocked by auto health probe")
	}

	close(checker.release)
	select {
	case err := <-healthDone:
		if err != nil {
			t.Fatalf("auto health check: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("auto health check did not finish")
	}
}

func TestConnectAutoDoesNotHoldStoreLockWhileProbing(t *testing.T) {
	t.Parallel()

	node := domain.Node{ID: "node-1", SubscriptionID: "sub-1", Address: "192.0.2.1", Port: 443}
	store := &serializedMemoryStore{memoryStore: &memoryStore{
		subs:     []domain.Subscription{{ID: "sub-1", Nodes: []domain.Node{node}}},
		settings: domain.DefaultSettings(),
		state:    domain.DefaultRuntimeState(),
	}}
	checker := &gatedProbeChecker{started: make(chan struct{}, 1), release: make(chan struct{})}
	service := NewService(Dependencies{Store: store, Checker: checker})

	connectDone := make(chan error, 1)
	go func() {
		_, err := service.ConnectAuto(context.Background(), "sub-1")
		connectDone <- err
	}()
	select {
	case <-checker.started:
	case <-time.After(time.Second):
		t.Fatal("auto connect probe did not start")
	}

	settingDone := make(chan error, 1)
	go func() {
		_, err := service.SetSetting("health-check-interval", "2m")
		settingDone <- err
	}()
	select {
	case err := <-settingDone:
		if err != nil {
			t.Fatalf("update setting while connect probe runs: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("settings update was blocked by auto connect probe")
	}

	close(checker.release)
	select {
	case err := <-connectDone:
		if !errors.Is(err, errAutoSelectionSnapshotChanged) {
			t.Fatalf("expected stale probe result to be rejected, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("auto connect did not finish")
	}
}

func TestRefreshAndReconnectDoesNotHoldStoreLockWhileAutoProbing(t *testing.T) {
	t.Parallel()

	node := domain.Node{
		ID:             "node-1",
		SubscriptionID: "sub-1",
		Name:           "Node",
		Protocol:       domain.ProtocolVLESS,
		Address:        "192.0.2.1",
		Port:           443,
		UUID:           "11111111-1111-1111-1111-111111111111",
		Encryption:     "none",
	}
	settings := domain.DefaultSettings()
	settings.AutoMode = true
	settings.Mode = domain.SelectionModeAuto
	settings.StrictEgressCheck = false
	store := &serializedMemoryStore{memoryStore: &memoryStore{
		subs: []domain.Subscription{{
			ID:         "sub-1",
			SourceType: domain.SourceTypeRaw,
			Source:     "vless://11111111-1111-1111-1111-111111111111@192.0.2.1:443?encryption=none#Node",
			Nodes:      []domain.Node{node},
		}},
		settings: settings,
		state: domain.RuntimeState{
			SchemaVersion:        domain.DefaultRuntimeState().SchemaVersion,
			ActiveSubscriptionID: "sub-1",
			ActiveNodeID:         node.ID,
			Mode:                 domain.SelectionModeAuto,
			Connected:            true,
			ActiveTransport:      domain.TransportModeProxy,
			Health:               map[string]domain.NodeHealth{},
			LastRefreshAt:        map[string]time.Time{},
		},
	}}
	checker := &gatedProbeChecker{started: make(chan struct{}, 1), release: make(chan struct{})}
	service := NewService(Dependencies{Store: store, Checker: checker})

	refreshDone := make(chan error, 1)
	go func() { refreshDone <- service.RefreshAndReconnect(context.Background()) }()
	waitForProbeStart(t, checker.started)
	assertSettingUpdateResponsive(t, service)

	close(checker.release)
	select {
	case err := <-refreshDone:
		if !errors.Is(err, errAutoSelectionSnapshotChanged) {
			t.Fatalf("expected refreshed auto snapshot to be rejected after concurrent change, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("refresh and reconnect did not finish")
	}
}

func TestSetAutoModeDoesNotHoldStoreLockWhileProbing(t *testing.T) {
	t.Parallel()

	node := domain.Node{ID: "node-1", SubscriptionID: "sub-1", Address: "192.0.2.1", Port: 443}
	settings := domain.DefaultSettings()
	settings.StrictEgressCheck = false
	store := &serializedMemoryStore{memoryStore: &memoryStore{
		subs:     []domain.Subscription{{ID: "sub-1", Nodes: []domain.Node{node}}},
		settings: settings,
		state: domain.RuntimeState{
			SchemaVersion:        domain.DefaultRuntimeState().SchemaVersion,
			ActiveSubscriptionID: "sub-1",
			ActiveNodeID:         node.ID,
			Mode:                 domain.SelectionModeManual,
			Connected:            true,
			ActiveTransport:      domain.TransportModeProxy,
			Health:               map[string]domain.NodeHealth{},
			LastRefreshAt:        map[string]time.Time{},
		},
	}}
	checker := &gatedProbeChecker{started: make(chan struct{}, 1), release: make(chan struct{})}
	service := NewService(Dependencies{Store: store, Checker: checker})

	autoDone := make(chan error, 1)
	go func() {
		_, err := service.SetSetting("auto-mode", "true")
		autoDone <- err
	}()
	waitForProbeStart(t, checker.started)
	assertSettingUpdateResponsive(t, service)

	close(checker.release)
	select {
	case err := <-autoDone:
		if !errors.Is(err, errAutoSelectionSnapshotChanged) {
			t.Fatalf("expected auto-mode snapshot to be rejected after concurrent change, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("auto-mode setting did not finish")
	}
}

func TestSetAutoExcludedNodesDoesNotHoldStoreLockWhileProbing(t *testing.T) {
	t.Parallel()

	first := domain.Node{ID: "node-1", SubscriptionID: "sub-1", Address: "192.0.2.1", Port: 443}
	second := domain.Node{ID: "node-2", SubscriptionID: "sub-1", Address: "192.0.2.2", Port: 443}
	settings := domain.DefaultSettings()
	settings.AutoMode = true
	settings.Mode = domain.SelectionModeAuto
	settings.StrictEgressCheck = false
	store := &serializedMemoryStore{memoryStore: &memoryStore{
		subs:     []domain.Subscription{{ID: "sub-1", Nodes: []domain.Node{first, second}}},
		settings: settings,
		state: domain.RuntimeState{
			SchemaVersion:        domain.DefaultRuntimeState().SchemaVersion,
			ActiveSubscriptionID: "sub-1",
			ActiveNodeID:         first.ID,
			Mode:                 domain.SelectionModeAuto,
			Connected:            true,
			ActiveTransport:      domain.TransportModeProxy,
			Health:               map[string]domain.NodeHealth{},
			LastRefreshAt:        map[string]time.Time{},
		},
	}}
	checker := &gatedProbeChecker{started: make(chan struct{}, 1), release: make(chan struct{})}
	service := NewService(Dependencies{Store: store, Checker: checker})

	excludedDone := make(chan error, 1)
	go func() {
		_, err := service.SetSetting("auto.excluded-nodes", "sub-1/node-2")
		excludedDone <- err
	}()
	waitForProbeStart(t, checker.started)
	assertSettingUpdateResponsive(t, service)

	close(checker.release)
	select {
	case err := <-excludedDone:
		if !errors.Is(err, errAutoSelectionSnapshotChanged) {
			t.Fatalf("expected exclusion snapshot to be rejected after concurrent change, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("auto exclusion setting did not finish")
	}
}

func TestConnectAutoProbesPoolOnceAndCapsRuntimeCandidates(t *testing.T) {
	t.Parallel()

	nodes := make([]domain.Node, maxAutoCandidateApplyAttempts+2)
	results := make(map[string]probe.Result, len(nodes))
	for index := range nodes {
		nodes[index] = domain.Node{
			ID:             fmt.Sprintf("node-%d", index),
			SubscriptionID: "sub-1",
			Address:        fmt.Sprintf("192.0.2.%d", index+1),
			Port:           443,
		}
		results[nodes[index].ID] = probe.Result{Healthy: true, Latency: time.Duration(index+1) * time.Millisecond, Checked: time.Unix(1_700_000_000, 0).UTC()}
	}
	store := &memoryStore{
		subs:     []domain.Subscription{{ID: "sub-1", Nodes: nodes}},
		settings: domain.DefaultSettings(),
		state:    domain.DefaultRuntimeState(),
	}
	checker := &countingProbeChecker{results: results, counts: make(map[string]int)}
	runtimeBackend := &recordingBackend{}
	service := NewService(Dependencies{Store: store, Backend: runtimeBackend, Checker: checker})
	service.backendEgressProbe = func(context.Context) error { return errors.New("egress unavailable") }
	service.backendEgressTimeout = time.Millisecond
	service.backendEgressRetryDelay = time.Second

	_, err := service.ConnectAuto(context.Background(), "sub-1")
	if err == nil {
		t.Fatal("expected bounded candidate verification to fail")
	}
	if len(runtimeBackend.requests) != maxAutoCandidateApplyAttempts {
		t.Fatalf("expected %d runtime attempts, got %d", maxAutoCandidateApplyAttempts, len(runtimeBackend.requests))
	}
	if got := checker.total(); got != len(nodes) {
		t.Fatalf("expected one probe per node (%d total), got %d", len(nodes), got)
	}
}

func TestStrictEgressCheckFalseSkipsProbeDuringManualConnect(t *testing.T) {
	t.Parallel()

	currentNode := domain.Node{ID: "node-current", SubscriptionID: "sub-1", Address: "192.0.2.1", Port: 443}
	node := domain.Node{ID: "node-next", SubscriptionID: "sub-1", Address: "192.0.2.2", Port: 443}
	settings := domain.DefaultSettings()
	settings.StrictEgressCheck = false
	store := &memoryStore{
		subs:     []domain.Subscription{{ID: "sub-1", Nodes: []domain.Node{currentNode, node}}},
		settings: settings,
		state: domain.RuntimeState{
			SchemaVersion:        domain.DefaultRuntimeState().SchemaVersion,
			ActiveSubscriptionID: "sub-1",
			ActiveNodeID:         currentNode.ID,
			Mode:                 domain.SelectionModeManual,
			Connected:            true,
			ActiveTransport:      domain.TransportModeProxy,
			Health:               map[string]domain.NodeHealth{},
			LastRefreshAt:        map[string]time.Time{},
		},
	}
	var calls atomic.Int32
	runtimeBackend := &recordingBackend{}
	service := NewService(Dependencies{Store: store, Backend: runtimeBackend})
	service.backendEgressProbe = func(context.Context) error {
		calls.Add(1)
		return errors.New("must not run")
	}

	if err := service.ConnectManual(context.Background(), "sub-1", node.ID); err != nil {
		t.Fatalf("manual connect with strict check disabled: %v", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("expected no egress probes, got %d", got)
	}
	if runtimeBackend.rollbackCalls != 0 {
		t.Fatalf("expected no rollback when strict egress check is disabled, got %d", runtimeBackend.rollbackCalls)
	}
	if !store.state.Connected || store.state.ActiveNodeID != node.ID {
		t.Fatalf("unexpected connected state: %+v", store.state)
	}
}

func TestStrictEgressCheckFalseSkipsProbeDuringAutoHealth(t *testing.T) {
	t.Parallel()

	node := domain.Node{ID: "node-1", SubscriptionID: "sub-1", Address: "192.0.2.1", Port: 443}
	settings := domain.DefaultSettings()
	settings.StrictEgressCheck = false
	settings.AutoMode = true
	settings.Mode = domain.SelectionModeAuto
	store := &memoryStore{
		subs:     []domain.Subscription{{ID: "sub-1", Nodes: []domain.Node{node}}},
		settings: settings,
		state: domain.RuntimeState{
			SchemaVersion:        domain.DefaultRuntimeState().SchemaVersion,
			ActiveSubscriptionID: "sub-1",
			ActiveNodeID:         node.ID,
			Mode:                 domain.SelectionModeAuto,
			Connected:            true,
			ActiveTransport:      domain.TransportModeProxy,
			Health:               map[string]domain.NodeHealth{},
			LastRefreshAt:        map[string]time.Time{},
		},
	}
	var calls atomic.Int32
	service := NewService(Dependencies{
		Store:   store,
		Backend: &recordingBackend{},
		Checker: fakeChecker{results: map[string]probe.Result{
			node.ID: {Healthy: true, Latency: 20 * time.Millisecond, Checked: time.Unix(1_700_000_000, 0).UTC()},
		}},
	})
	service.backendEgressProbe = func(context.Context) error {
		calls.Add(1)
		return errors.New("must not run")
	}

	if err := service.RunAutoHealthCheck(context.Background()); err != nil {
		t.Fatalf("auto health with strict check disabled: %v", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("expected no egress probes, got %d", got)
	}
	if !store.state.Connected || store.state.ActiveNodeID != node.ID {
		t.Fatalf("unexpected auto state: %+v", store.state)
	}
}

type gatedProbeChecker struct {
	started     chan struct{}
	release     chan struct{}
	inFlight    atomic.Int32
	maxInFlight atomic.Int32
}

func (c *gatedProbeChecker) Check(ctx context.Context, node domain.Node) probe.Result {
	inFlight := c.inFlight.Add(1)
	for {
		maximum := c.maxInFlight.Load()
		if inFlight <= maximum || c.maxInFlight.CompareAndSwap(maximum, inFlight) {
			break
		}
	}
	c.started <- struct{}{}
	select {
	case <-c.release:
	case <-ctx.Done():
	}
	c.inFlight.Add(-1)
	return probe.Result{NodeID: node.ID, Healthy: true, Latency: time.Millisecond, Checked: time.Unix(1_700_000_000, 0).UTC()}
}

type countingProbeChecker struct {
	mu      sync.Mutex
	results map[string]probe.Result
	counts  map[string]int
}

func (c *countingProbeChecker) Check(_ context.Context, node domain.Node) probe.Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[node.ID]++
	result := c.results[node.ID]
	result.NodeID = node.ID
	return result
}

func (c *countingProbeChecker) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	total := 0
	for _, count := range c.counts {
		total += count
	}
	return total
}

type serializedMemoryStore struct {
	*memoryStore
	writeMu sync.Mutex
}

func (s *serializedMemoryStore) WithWriteLock(fn func() error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return fn()
}

func waitForProbeStart(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("auto probe did not start")
	}
}

func assertSettingUpdateResponsive(t *testing.T, service *Service) {
	t.Helper()
	settingDone := make(chan error, 1)
	go func() {
		_, err := service.SetSetting("health-check-interval", "2m")
		settingDone <- err
	}()
	select {
	case err := <-settingDone:
		if err != nil {
			t.Fatalf("update setting while auto probe runs: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("settings update was blocked by auto probe")
	}
}
