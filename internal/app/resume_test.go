package app

import (
	"context"
	"testing"

	"github.com/design-maestro/fastlane/internal/domain"
)

func TestDisconnectResumePreservesManualSelection(t *testing.T) {
	t.Parallel()
	store := &memoryStore{settings: domain.DefaultSettings(), state: domain.DefaultRuntimeState(), subs: []domain.Subscription{{ID: "sub", Nodes: []domain.Node{{ID: "node", Protocol: domain.ProtocolSocks, Address: "192.0.2.1", Port: 1080}}}}}
	service := NewService(Dependencies{Store: store, Backend: &recordingBackend{}})
	ctx := context.Background()
	if err := service.ConnectManual(ctx, "sub", "node"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := service.Disconnect(ctx); err != nil {
			t.Fatal(err)
		}
	}
	resume := store.state.ResumeConnection
	if resume == nil || resume.NodeID != "node" || resume.Mode != domain.SelectionModeManual || store.state.Connected {
		t.Fatalf("selection lost: %+v", store.state)
	}
	// A fresh service represents reloading the interface or restarting the CLI.
	service = NewService(Dependencies{Store: store, Backend: &recordingBackend{}})
	if err := service.ResumeConnection(ctx); err != nil || !store.state.Connected || store.state.ActiveNodeID != "node" {
		t.Fatalf("resume failed: %+v %v", store.state, err)
	}
	if err := service.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	store.subs = nil
	if err := service.ResumeConnection(ctx); err == nil || store.state.Connected {
		t.Fatal("removed server must not be silently replaced")
	}
}

func TestResumeAutoRestoresPreviousServerWithoutPoolProbe(t *testing.T) {
	for _, scope := range []string{autoScopeAll, "sub"} {
		t.Run(scope, func(t *testing.T) {
			store := &memoryStore{settings: domain.DefaultSettings(), state: domain.DefaultRuntimeState(), subs: []domain.Subscription{{ID: "sub", Nodes: []domain.Node{{ID: "node", Protocol: domain.ProtocolSocks, Address: "192.0.2.1", Port: 1080}}}}}
			service := NewService(Dependencies{Store: store, Backend: &recordingBackend{}})
			ctx := context.Background()
			if err := service.ConnectManual(ctx, "sub", "node"); err != nil {
				t.Fatal(err)
			}
			store.state.Mode = domain.SelectionModeAuto
			store.state.AutoScope = scope
			if err := service.Disconnect(ctx); err != nil {
				t.Fatal(err)
			}
			service = NewService(Dependencies{Store: store, Backend: &recordingBackend{}})
			// No pool prober is configured: resume must directly apply the saved node.
			if err := service.ResumeConnection(ctx); err != nil {
				t.Fatal(err)
			}
			if !store.state.Connected || store.state.ActiveNodeID != "node" || store.state.Mode != domain.SelectionModeAuto || store.state.AutoScope != scope || !store.settings.AutoMode || store.settings.Mode != domain.SelectionModeAuto {
				t.Fatalf("previous automatic connection not restored: %+v %+v", store.state, store.settings)
			}
		})
	}
}
