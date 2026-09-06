package api

import (
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/app"
	"github.com/design-maestro/fastlane/internal/domain"
)

func TestSubscriptionSummaryUsesEffectiveGlobalRefreshInterval(t *testing.T) {
	t.Parallel()

	sub := domain.Subscription{
		ID:              "sub-1",
		RefreshInterval: domain.NewDuration(24 * time.Hour),
	}
	effective := domain.NewDuration(5 * time.Minute)

	summary := SubscriptionSummaryFromDomainWithRefresh(sub, false, effective)
	if summary.RefreshEvery != "5m0s" {
		t.Fatalf("refresh_every = %q; want global 5m0s", summary.RefreshEvery)
	}

	snapshot := app.StatusSnapshot{
		Settings:           domain.Settings{RefreshInterval: effective},
		ActiveSubscription: &sub,
	}
	status := StatusResponseFromSnapshot(snapshot)
	if status.ActiveSubscription == nil || status.ActiveSubscription.RefreshEvery != "5m0s" {
		t.Fatalf("status active subscription did not use global interval: %+v", status.ActiveSubscription)
	}
}

func TestStatusResponseCompactsVerboseProbeErrors(t *testing.T) {
	t.Parallel()

	verbose := "GET timeout\nXray started\n" + string(make([]byte, 400))
	state := domain.DefaultRuntimeState()
	state.LastFailureReason = verbose
	state.Health["node-1"] = domain.NodeHealth{NodeID: "node-1", LastFailureReason: verbose}

	response := StatusResponseFromSnapshot(app.StatusSnapshot{State: state})
	if response.State.LastFailureReason != "GET timeout" {
		t.Fatalf("top-level failure was not compacted: %q", response.State.LastFailureReason)
	}
	if response.State.Health["node-1"].LastFailureReason != "GET timeout" {
		t.Fatalf("node failure was not compacted: %q", response.State.Health["node-1"].LastFailureReason)
	}
	if state.Health["node-1"].LastFailureReason != verbose {
		t.Fatal("status conversion mutated the service snapshot")
	}
}
