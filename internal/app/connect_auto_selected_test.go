package app

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
)

func TestConnectAutoSelectedRejectsInvalidMeasuredLatencyWithoutPersistence(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		latencyMS float64
	}{
		{name: "zero", latencyMS: 0},
		{name: "negative", latencyMS: -1},
		{name: "not a number", latencyMS: math.NaN()},
		{name: "infinity", latencyMS: math.Inf(1)},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			node := domain.Node{ID: "node-1", SubscriptionID: "sub-1", Protocol: domain.ProtocolVLESS, Address: "192.0.2.10", Port: 443}
			settings := domain.DefaultSettings()
			settings.StrictEgressCheck = false
			store := &memoryStore{
				subs:     []domain.Subscription{{ID: "sub-1", Nodes: []domain.Node{node}}},
				settings: settings,
				state:    domain.DefaultRuntimeState(),
			}
			runtimeBackend := &recordingBackend{}
			service := NewService(Dependencies{Store: store, Backend: runtimeBackend})

			_, err := service.ConnectAutoSelected(context.Background(), "sub-1", node.ID, test.latencyMS, false)
			if err == nil || !strings.Contains(err.Error(), "positive finite") {
				t.Fatalf("expected invalid measured-latency error, got %v", err)
			}
			if len(runtimeBackend.requests) != 0 {
				t.Fatalf("invalid latency applied backend config: %+v", runtimeBackend.requests)
			}
			if _, ok := store.state.Health[node.ID]; ok {
				t.Fatalf("invalid latency was persisted as node health: %+v", store.state.Health[node.ID])
			}
			if store.state.Connected || store.settings.AutoMode {
				t.Fatalf("invalid latency changed runtime state: state=%+v settings=%+v", store.state, store.settings)
			}
		})
	}
}

func TestConnectAutoSelectedPersistsMeasuredHealthAndScope(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		global    bool
		wantScope string
	}{
		{name: "selected subscription", global: false, wantScope: "sub-1"},
		{name: "all subscriptions", global: true, wantScope: autoScopeAll},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			checkedAt := time.Date(2026, 9, 4, 13, 30, 0, 0, time.UTC)
			node := domain.Node{
				ID:             "node-1",
				SubscriptionID: "sub-1",
				Name:           "Netherlands",
				Protocol:       domain.ProtocolVLESS,
				Address:        "192.0.2.10",
				Port:           443,
			}
			settings := domain.DefaultSettings()
			settings.StrictEgressCheck = false
			state := domain.DefaultRuntimeState()
			state.Health["node-1"] = domain.NodeHealth{
				NodeID:              "node-1",
				SuccessCount:        2,
				ConsecutiveFailures: 3,
				LastFailureReason:   "old failure",
			}
			state.Health["other-node"] = domain.NodeHealth{NodeID: "other-node", Healthy: true}
			store := &memoryStore{
				subs:     []domain.Subscription{{ID: "sub-1", Nodes: []domain.Node{node}}},
				settings: settings,
				state:    state,
			}
			runtimeBackend := &recordingBackend{}
			service := NewService(Dependencies{Store: store, Backend: runtimeBackend})
			service.now = func() time.Time { return checkedAt }

			selected, err := service.ConnectAutoSelected(context.Background(), "sub-1", "node-1", 131.5, test.global)
			if err != nil {
				t.Fatalf("connect pre-tested auto node: %v", err)
			}
			if selected.ID != node.ID {
				t.Fatalf("selected node = %q, want %q", selected.ID, node.ID)
			}
			if len(runtimeBackend.requests) != 1 {
				t.Fatalf("backend apply calls = %d, want 1", len(runtimeBackend.requests))
			}
			request := runtimeBackend.requests[0]
			if request.Mode != domain.SelectionModeAuto || request.SelectedNodeID != node.ID {
				t.Fatalf("unexpected backend request: %+v", request)
			}

			persisted, err := store.LoadState()
			if err != nil {
				t.Fatalf("load persisted state: %v", err)
			}
			if !persisted.Connected || persisted.Mode != domain.SelectionModeAuto || persisted.ActiveTransport != domain.TransportModeProxy {
				t.Fatalf("unexpected persisted connection state: %+v", persisted)
			}
			if persisted.ActiveSubscriptionID != "sub-1" || persisted.ActiveNodeID != node.ID || persisted.AutoScope != test.wantScope {
				t.Fatalf("unexpected persisted selection/scope: %+v", persisted)
			}
			health := persisted.Health[node.ID]
			if got := health.LastLatency.Duration(); got != 131500*time.Microsecond {
				t.Fatalf("last GET latency = %s, want 131.5ms", got)
			}
			if got := health.AverageLatency.Duration(); got != 131500*time.Microsecond {
				t.Fatalf("average GET latency = %s, want 131.5ms", got)
			}
			if !health.Healthy || health.SuccessCount != 3 || health.ConsecutiveSuccesses != 1 || health.ConsecutiveFailures != 0 {
				t.Fatalf("unexpected persisted GET health: %+v", health)
			}
			if !health.LastCheckedAt.Equal(checkedAt) || health.LastFailureReason != "" {
				t.Fatalf("unexpected persisted GET timestamp/failure: %+v", health)
			}
			if !persisted.Health["other-node"].Healthy {
				t.Fatalf("unrelated health entry was lost: %+v", persisted.Health)
			}
			if !store.settings.AutoMode || store.settings.Mode != domain.SelectionModeAuto {
				t.Fatalf("auto settings were not persisted: %+v", store.settings)
			}
		})
	}
}

func TestConnectAutoSelectedHonorsStrictEgressSetting(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		strict    bool
		probeErr  error
		wantError bool
		wantCalls bool
	}{
		{name: "disabled skips verification", strict: false, probeErr: errors.New("must not be called")},
		{name: "enabled accepts verified runtime", strict: true, wantCalls: true},
		{name: "enabled rejects broken runtime", strict: true, probeErr: errors.New("GET through selected runtime failed"), wantError: true, wantCalls: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			node := domain.Node{ID: "node-1", SubscriptionID: "sub-1", Protocol: domain.ProtocolVLESS, Address: "192.0.2.10", Port: 443}
			settings := domain.DefaultSettings()
			settings.StrictEgressCheck = test.strict
			store := &memoryStore{
				subs:     []domain.Subscription{{ID: "sub-1", Nodes: []domain.Node{node}}},
				settings: settings,
				state:    domain.DefaultRuntimeState(),
			}
			runtimeBackend := &recordingBackend{}
			service := NewService(Dependencies{Store: store, Backend: runtimeBackend})
			probeCalls := 0
			service.backendEgressProbe = func(context.Context) error {
				probeCalls++
				return test.probeErr
			}
			service.backendEgressTimeout = 3 * time.Millisecond
			service.backendEgressRetryDelay = time.Millisecond

			_, err := service.ConnectAutoSelected(context.Background(), "sub-1", "node-1", 42, false)
			if test.wantError {
				if err == nil || !strings.Contains(err.Error(), "check backend egress") {
					t.Fatalf("expected strict-egress error, got %v", err)
				}
				if store.state.Connected || store.state.ActiveTransport != domain.TransportModeDirect {
					t.Fatalf("failed strict-egress verification left runtime connected: %+v", store.state)
				}
				if store.state.ActiveSubscriptionID != "sub-1" || store.state.ActiveNodeID != "node-1" || store.state.Mode != domain.SelectionModeAuto {
					t.Fatalf("failed selection was not recorded precisely: %+v", store.state)
				}
				if store.state.AutoScope != "" {
					t.Fatalf("failed selection persisted auto scope %q", store.state.AutoScope)
				}
				if store.settings.AutoMode {
					t.Fatalf("failed selection enabled auto settings: %+v", store.settings)
				}
				health := store.state.Health[node.ID]
				if health.Healthy || health.FailureCount == 0 || !strings.Contains(health.LastFailureReason, "backend egress probe failed") {
					t.Fatalf("strict-egress failure was not persisted in node health: %+v", health)
				}
			} else if err != nil {
				t.Fatalf("connect pre-tested auto node: %v", err)
			}
			if got := probeCalls > 0; got != test.wantCalls {
				t.Fatalf("strict-egress probe called = %t (calls=%d), want %t", got, probeCalls, test.wantCalls)
			}
		})
	}
}

func TestConnectAutoSelectedStrictEgressFailureRestoresPreviousSelection(t *testing.T) {
	t.Parallel()

	oldNode := domain.Node{ID: "node-old", SubscriptionID: "sub-1", Protocol: domain.ProtocolVLESS, Address: "192.0.2.10", Port: 443}
	newNode := domain.Node{ID: "node-new", SubscriptionID: "sub-1", Protocol: domain.ProtocolVLESS, Address: "192.0.2.11", Port: 443}
	settings := domain.DefaultSettings()
	settings.StrictEgressCheck = true
	settings.AutoMode = true
	settings.Mode = domain.SelectionModeAuto
	previous := domain.DefaultRuntimeState()
	previous.AutoScope = autoScopeAll
	previous.ActiveSubscriptionID = "sub-1"
	previous.ActiveNodeID = oldNode.ID
	previous.Mode = domain.SelectionModeAuto
	previous.Connected = true
	previous.ActiveTransport = domain.TransportModeProxy
	store := &memoryStore{
		subs:     []domain.Subscription{{ID: "sub-1", Nodes: []domain.Node{oldNode, newNode}}},
		settings: settings,
		state:    previous,
	}
	runtimeBackend := &recordingBackend{}
	service := NewService(Dependencies{Store: store, Backend: runtimeBackend})
	service.backendEgressProbe = func(context.Context) error { return errors.New("candidate GET failed") }
	service.backendEgressTimeout = 3 * time.Millisecond
	service.backendEgressRetryDelay = time.Millisecond

	_, err := service.ConnectAutoSelected(context.Background(), "sub-1", newNode.ID, 18, true)
	if err == nil || !strings.Contains(err.Error(), "candidate verify failed") {
		t.Fatalf("expected candidate verification error, got %v", err)
	}
	if runtimeBackend.rollbackCalls != 1 {
		t.Fatalf("rollback calls = %d, want 1", runtimeBackend.rollbackCalls)
	}
	if !store.state.Connected || store.state.ActiveTransport != domain.TransportModeProxy {
		t.Fatalf("previous runtime was not preserved: %+v", store.state)
	}
	if store.state.ActiveSubscriptionID != previous.ActiveSubscriptionID || store.state.ActiveNodeID != previous.ActiveNodeID || store.state.AutoScope != previous.AutoScope {
		t.Fatalf("previous selection/scope was not preserved: %+v", store.state)
	}
	failedHealth := store.state.Health[newNode.ID]
	if failedHealth.Healthy || failedHealth.FailureCount == 0 || !strings.Contains(failedHealth.LastFailureReason, "candidate verify failed") {
		t.Fatalf("rejected candidate health was not persisted: %+v", failedHealth)
	}
}
