package app

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/probe"
)

func TestCheckHealthDoesNotSelectOrChangeConnectionMode(t *testing.T) {
	for _, mode := range []domain.SelectionMode{domain.SelectionModeManual, domain.SelectionModeAuto} {
		t.Run(string(mode), func(t *testing.T) {
			state := domain.DefaultRuntimeState()
			state.Mode, state.Connected, state.ActiveNodeID, state.ActiveSubscriptionID = mode, true, "slow", "pool"
			state.SelectedOutboundTag = "fastlane-node-slow"
			store := &memoryStore{settings: domain.DefaultSettings(), state: state, subs: []domain.Subscription{{ID: "pool", Nodes: []domain.Node{{ID: "slow"}, {ID: "fast"}}}}}
			checker := &countingProbeChecker{counts: make(map[string]int), results: map[string]probe.Result{
				"slow": {Healthy: true, Latency: 800 * time.Millisecond, Checked: time.Now()},
				"fast": {Healthy: true, Latency: 20 * time.Millisecond, Checked: time.Now()},
			}}
			service := NewService(Dependencies{Store: store, Checker: checker})
			if err := service.CheckHealth(context.Background(), "all"); err != nil {
				t.Fatal(err)
			}
			if len(store.state.Health) != 2 || !store.state.Health["fast"].Healthy {
				t.Fatal("observations not saved")
			}
			actual := store.state
			actual.Health = state.Health
			if !reflect.DeepEqual(actual, state) {
				t.Fatal("GET check modified connection state")
			}
		})
	}
}
