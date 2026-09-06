package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/backend"
	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/probe"
)

func TestConnectAutoStopsCandidateFallbackOnApplyFailure(t *testing.T) {
	t.Parallel()

	for _, scope := range []struct {
		name           string
		subscriptionID string
	}{
		{name: "single subscription", subscriptionID: "sub-1"},
		{name: "all subscriptions", subscriptionID: ""},
	} {
		scope := scope
		t.Run(scope.name, func(t *testing.T) {
			t.Parallel()

			store, checker := twoCandidateAutoFixture()
			recording := &recordingBackend{}
			runtimeBackend := &applyFailingBackend{
				recordingBackend: recording,
				err:              errors.New("write shared xray config failed"),
			}
			service := NewService(Dependencies{Store: store, Backend: runtimeBackend, Checker: checker})

			_, err := service.ConnectAuto(context.Background(), scope.subscriptionID)
			if err == nil {
				t.Fatal("expected backend apply failure")
			}
			if len(recording.requests) != 1 {
				t.Fatalf("expected shared apply failure to stop after one candidate, got %d attempts", len(recording.requests))
			}
		})
	}
}

func TestConnectAutoStopsCandidateFallbackOnBackendControlFailure(t *testing.T) {
	t.Parallel()

	for _, scope := range []struct {
		name           string
		subscriptionID string
	}{
		{name: "single subscription", subscriptionID: "sub-1"},
		{name: "all subscriptions", subscriptionID: ""},
	} {
		scope := scope
		t.Run(scope.name, func(t *testing.T) {
			t.Parallel()

			store, checker := twoCandidateAutoFixture()
			runtimeBackend := &recordingBackend{statusErr: errors.New("service controller unavailable")}
			service := NewService(Dependencies{Store: store, Backend: runtimeBackend, Checker: checker})

			_, err := service.ConnectAuto(context.Background(), scope.subscriptionID)
			if err == nil {
				t.Fatal("expected backend status failure")
			}
			if len(runtimeBackend.requests) != 1 {
				t.Fatalf("expected backend-control failure to stop after one candidate, got %d attempts", len(runtimeBackend.requests))
			}
		})
	}
}

func TestConnectAutoStopsCandidateFallbackOnStoreFailure(t *testing.T) {
	t.Parallel()

	for _, scope := range []struct {
		name           string
		subscriptionID string
	}{
		{name: "single subscription", subscriptionID: "sub-1"},
		{name: "all subscriptions", subscriptionID: ""},
	} {
		scope := scope
		t.Run(scope.name, func(t *testing.T) {
			t.Parallel()

			memory, checker := twoCandidateAutoFixture()
			memory.settings.StrictEgressCheck = false
			store := &failLoadStateStore{memoryStore: memory}
			recording := &recordingBackend{}
			runtimeBackend := &hookApplyBackend{
				recordingBackend: recording,
				hook:             func() { store.fail = true },
			}
			service := NewService(Dependencies{Store: store, Backend: runtimeBackend, Checker: checker})

			_, err := service.ConnectAuto(context.Background(), scope.subscriptionID)
			if err == nil {
				t.Fatal("expected state-store failure")
			}
			if len(recording.requests) != 1 {
				t.Fatalf("expected state-store failure to stop after one candidate, got %d attempts", len(recording.requests))
			}
		})
	}
}

func TestConnectAutoStopsCandidateFallbackWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	for _, scope := range []struct {
		name           string
		subscriptionID string
	}{
		{name: "single subscription", subscriptionID: "sub-1"},
		{name: "all subscriptions", subscriptionID: ""},
	} {
		scope := scope
		t.Run(scope.name, func(t *testing.T) {
			t.Parallel()

			store, checker := twoCandidateAutoFixture()
			runtimeBackend := &recordingBackend{}
			service := NewService(Dependencies{Store: store, Backend: runtimeBackend, Checker: checker})
			ctx, cancel := context.WithCancel(context.Background())
			service.backendEgressProbe = func(context.Context) error {
				cancel()
				return context.Canceled
			}
			service.backendEgressRetryDelay = time.Millisecond
			service.backendEgressTimeout = time.Second

			_, err := service.ConnectAuto(ctx, scope.subscriptionID)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("expected context cancellation, got %v", err)
			}
			if len(runtimeBackend.requests) != 1 {
				t.Fatalf("expected cancellation to stop after one candidate, got %d attempts", len(runtimeBackend.requests))
			}
		})
	}
}

func TestConnectAutoAllFallsBackAfterNodeSpecificEgressFailure(t *testing.T) {
	t.Parallel()

	store, checker := twoCandidateAutoFixture()
	runtimeBackend := &recordingBackend{}
	service := NewService(Dependencies{Store: store, Backend: runtimeBackend, Checker: checker})
	service.backendEgressProbe = func(context.Context) error {
		if runtimeBackend.requests[len(runtimeBackend.requests)-1].SelectedNodeID == "node-fast" {
			return errors.New("HTTPS GET through candidate failed")
		}
		return nil
	}
	service.backendEgressRetryDelay = time.Millisecond
	service.backendEgressTimeout = 5 * time.Millisecond

	selected, err := service.ConnectAuto(context.Background(), "")
	if err != nil {
		t.Fatalf("connect global auto: %v", err)
	}
	if selected.ID != "node-working" {
		t.Fatalf("expected verified fallback candidate, got %q", selected.ID)
	}
	if len(runtimeBackend.requests) != 2 {
		t.Fatalf("expected two node-specific candidate attempts, got %d", len(runtimeBackend.requests))
	}
}

func twoCandidateAutoFixture() (*memoryStore, fakeChecker) {
	nodes := []domain.Node{
		{ID: "node-fast", SubscriptionID: "sub-1", Name: "Fast", Protocol: domain.ProtocolVLESS, Address: "fast.example.com", Port: 443},
		{ID: "node-working", SubscriptionID: "sub-1", Name: "Working", Protocol: domain.ProtocolVLESS, Address: "working.example.com", Port: 443},
	}
	return &memoryStore{
			subs:     []domain.Subscription{{ID: "sub-1", Nodes: nodes}},
			settings: domain.DefaultSettings(),
			state:    domain.DefaultRuntimeState(),
		}, fakeChecker{results: map[string]probe.Result{
			"node-fast":    {NodeID: "node-fast", Healthy: true, Latency: 10 * time.Millisecond, Checked: time.Now().UTC()},
			"node-working": {NodeID: "node-working", Healthy: true, Latency: 40 * time.Millisecond, Checked: time.Now().UTC()},
		}}
}

type applyFailingBackend struct {
	*recordingBackend
	err error
}

func (b *applyFailingBackend) ApplyConfig(_ context.Context, req backend.ConfigRequest) error {
	b.requests = append(b.requests, req)
	return b.err
}

type hookApplyBackend struct {
	*recordingBackend
	hook func()
}

func (b *hookApplyBackend) ApplyConfig(_ context.Context, req backend.ConfigRequest) error {
	b.requests = append(b.requests, req)
	if b.hook != nil {
		b.hook()
	}
	return nil
}

type failLoadStateStore struct {
	*memoryStore
	fail bool
}

func (s *failLoadStateStore) LoadState() (domain.RuntimeState, error) {
	if s.fail {
		return domain.RuntimeState{}, errors.New("shared state store unavailable")
	}
	return s.memoryStore.LoadState()
}
