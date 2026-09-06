package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/app"
	"github.com/design-maestro/fastlane/internal/backend"
	"github.com/design-maestro/fastlane/internal/domain"
)

func TestConnectCommandCommitsLuCIBatchSelection(t *testing.T) {
	t.Parallel()

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
	store := &cliMemoryStore{
		subs:     []domain.Subscription{{ID: "sub-1", Nodes: []domain.Node{node}}},
		settings: settings,
		state:    domain.DefaultRuntimeState(),
	}
	runtimeBackend := &cliConnectBackend{}
	service := app.NewService(app.Dependencies{Store: store, Backend: runtimeBackend})
	cmd := newConnectCmd(&rootOptions{service: service})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"--auto", "--subscription", "sub-1", "--node", "node-1", "--latency-ms", "131.5", "--global"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute LuCI batch connect command: %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "Auto selected Netherlands (node-1) by HTTPS GET") {
		t.Fatalf("unexpected command output: %q", got)
	}
	if len(runtimeBackend.requests) != 1 || runtimeBackend.requests[0].Mode != domain.SelectionModeAuto || runtimeBackend.requests[0].SelectedNodeID != node.ID {
		t.Fatalf("unexpected backend request: %+v", runtimeBackend.requests)
	}
	if !store.state.Connected || store.state.ActiveSubscriptionID != "sub-1" || store.state.ActiveNodeID != node.ID || store.state.AutoScope != "all" {
		t.Fatalf("LuCI batch selection/scope was not persisted: %+v", store.state)
	}
	if store.state.Mode != domain.SelectionModeAuto || store.state.ActiveTransport != domain.TransportModeProxy {
		t.Fatalf("unexpected persisted runtime mode: %+v", store.state)
	}
	if got := store.state.Health[node.ID].LastLatency.Duration(); got != 131500*time.Microsecond {
		t.Fatalf("persisted GET latency = %s, want 131.5ms", got)
	}
	if !store.settings.AutoMode || store.settings.Mode != domain.SelectionModeAuto {
		t.Fatalf("auto settings were not persisted: %+v", store.settings)
	}
}

func TestConnectCommandPropagatesLuCIBatchApplyError(t *testing.T) {
	t.Parallel()

	node := domain.Node{ID: "node-1", SubscriptionID: "sub-1", Protocol: domain.ProtocolVLESS, Address: "192.0.2.10", Port: 443}
	settings := domain.DefaultSettings()
	settings.StrictEgressCheck = false
	store := &cliMemoryStore{
		subs:     []domain.Subscription{{ID: "sub-1", Nodes: []domain.Node{node}}},
		settings: settings,
		state:    domain.DefaultRuntimeState(),
	}
	runtimeBackend := &cliConnectBackend{applyErr: errors.New("synthetic config write failure")}
	service := app.NewService(app.Dependencies{Store: store, Backend: runtimeBackend})
	cmd := newConnectCmd(&rootOptions{service: service})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"--auto", "--subscription", "sub-1", "--node", "node-1", "--latency-ms", "27", "--global"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "apply backend config: synthetic config write failure") {
		t.Fatalf("expected backend apply error, got %v", err)
	}
	if strings.Contains(stdout.String(), "Auto selected") {
		t.Fatalf("failed command printed success output: %q", stdout.String())
	}
	if store.state.Connected || store.state.ActiveTransport != domain.TransportModeDirect {
		t.Fatalf("failed command left runtime connected: %+v", store.state)
	}
	if store.state.ActiveSubscriptionID != "sub-1" || store.state.ActiveNodeID != node.ID || store.state.AutoScope != "" {
		t.Fatalf("failed selection state/scope is incorrect: %+v", store.state)
	}
	if store.settings.AutoMode {
		t.Fatalf("failed command enabled auto settings: %+v", store.settings)
	}
}

func TestConnectCommandDoesNotPersistMissingBatchLatencyAsHealthy(t *testing.T) {
	t.Parallel()

	node := domain.Node{ID: "node-1", SubscriptionID: "sub-1", Protocol: domain.ProtocolVLESS, Address: "192.0.2.10", Port: 443}
	settings := domain.DefaultSettings()
	settings.StrictEgressCheck = false
	store := &cliMemoryStore{
		subs:     []domain.Subscription{{ID: "sub-1", Nodes: []domain.Node{node}}},
		settings: settings,
		state:    domain.DefaultRuntimeState(),
	}
	runtimeBackend := &cliConnectBackend{}
	service := app.NewService(app.Dependencies{Store: store, Backend: runtimeBackend})
	cmd := newConnectCmd(&rootOptions{service: service})
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"--auto", "--subscription", "sub-1", "--node", node.ID})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "positive finite") {
		t.Fatalf("expected missing measured-latency error, got %v", err)
	}
	if len(runtimeBackend.requests) != 0 || store.state.Connected || store.settings.AutoMode {
		t.Fatalf("missing latency changed runtime state: requests=%+v state=%+v settings=%+v", runtimeBackend.requests, store.state, store.settings)
	}
	if _, ok := store.state.Health[node.ID]; ok {
		t.Fatalf("missing latency was persisted as healthy: %+v", store.state.Health[node.ID])
	}
}

type cliConnectBackend struct {
	requests []backend.ConfigRequest
	applyErr error
}

func (b *cliConnectBackend) GenerateConfig(backend.ConfigRequest) ([]byte, error) { return nil, nil }
func (b *cliConnectBackend) ApplyConfig(_ context.Context, request backend.ConfigRequest) error {
	b.requests = append(b.requests, request)
	return b.applyErr
}
func (b *cliConnectBackend) CaptureRollback() (backend.RollbackSnapshot, error) {
	return backend.RollbackSnapshot{}, nil
}
func (b *cliConnectBackend) RollbackConfig(context.Context, backend.RollbackSnapshot) error {
	return nil
}
func (b *cliConnectBackend) Start(context.Context) error  { return nil }
func (b *cliConnectBackend) Stop(context.Context) error   { return nil }
func (b *cliConnectBackend) Reload(context.Context) error { return nil }
func (b *cliConnectBackend) Status(context.Context) (backend.RuntimeStatus, error) {
	return backend.RuntimeStatus{Running: true, ServiceState: "running"}, nil
}
