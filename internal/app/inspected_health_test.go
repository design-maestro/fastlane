package app

import (
	"reflect"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/speedtest"
)

func TestInspectedHealthPersistsCountryWithoutChangingConnection(t *testing.T) {
	now := time.Date(2026, 9, 18, 7, 0, 0, 0, time.UTC)
	node := domain.Node{ID: "one", SubscriptionID: "sub", Address: "example.invalid"}
	store := &memoryStore{settings: domain.DefaultSettings(), subs: []domain.Subscription{{ID: "sub", Nodes: []domain.Node{node}}}, state: domain.DefaultRuntimeState()}
	store.state.ActiveNodeID, store.state.ActiveSubscriptionID = "active", "other"
	store.state.Connected = true
	store.state.Mode = domain.SelectionModeManual
	store.state.LastSwitchAt = now.Add(-time.Hour)
	service := NewService(Dependencies{Store: store})
	original := store.state
	result := speedtest.URLTestResult{LatencyMS: 42, CheckedAt: now, CountryCode: "SE", EgressIP: "192.0.2.2"}
	if err := service.persistInspectedHealth("sub", node, result, store.settings); err != nil {
		t.Fatal(err)
	}
	// A later GET with unavailable auxiliary GeoIP keeps the last identity.
	result.CheckedAt, result.CountryCode, result.EgressIP = now.Add(time.Minute), "", ""
	if err := service.persistInspectedHealth("sub", node, result, store.settings); err != nil {
		t.Fatal(err)
	}
	h := store.state.Health[node.ID]
	if h.CountryCode != "SE" || h.EgressIP != "192.0.2.2" || h.SuccessCount != 2 || !h.Healthy {
		t.Fatalf("unexpected persisted health: %+v", h)
	}
	// Reopening the service must retain both identity and health.
	reopened := NewService(Dependencies{Store: store})
	reloaded, err := reopened.loadStateWithAutoHealthCache()
	if err != nil || !reflect.DeepEqual(reloaded.Health[node.ID], h) {
		t.Fatalf("reloaded health changed: %v", err)
	}
	state := store.state
	state.Health, original.Health = nil, nil
	state.OperationalMode = original.OperationalMode
	if !reflect.DeepEqual(state, original) {
		t.Fatal("row GET changed connection state")
	}
	result.CheckedAt, result.CountryCode = now, "NL"
	_ = service.persistInspectedHealth("sub", node, result, store.settings)
	if !reflect.DeepEqual(store.state.Health[node.ID], h) {
		t.Fatal("stale result overwrote a newer observation")
	}
	withoutIdentity := h
	withoutIdentity.CountryCode, withoutIdentity.EgressIP = "", ""
	store.state.Health[node.ID] = withoutIdentity
	_ = service.persistInspectedHealth("sub", node, result, store.settings)
	h = store.state.Health[node.ID]
	if h.CountryCode != "NL" || h.SuccessCount != withoutIdentity.SuccessCount || !h.LastCheckedAt.Equal(withoutIdentity.LastCheckedAt) {
		t.Fatal("concurrent metrics must retain their freshness while missing identity is filled")
	}
	store.subs = nil
	result.CheckedAt = now.Add(time.Hour)
	_ = service.persistInspectedHealth("sub", node, result, store.settings)
	if !reflect.DeepEqual(store.state.Health[node.ID], h) {
		t.Fatal("removed node was updated by an in-flight result")
	}
}

func TestAutoHealthCacheDoesNotEraseNewerPersistedCountry(t *testing.T) {
	now := time.Now().UTC()
	store := &memoryStore{state: domain.DefaultRuntimeState()}
	store.state.Mode, store.state.ActiveSubscriptionID = domain.SelectionModeAuto, "sub"
	store.state.Health = map[string]domain.NodeHealth{"one": {LastCheckedAt: now, LastLatency: domain.NewDuration(time.Second)}}
	service := NewService(Dependencies{Store: store})
	service.rememberAutoHealthState(store.state, true)
	store.state.Health["one"] = domain.NodeHealth{LastCheckedAt: now.Add(time.Minute), CountryCode: "SE", EgressIP: "192.0.2.3", LastLatency: domain.NewDuration(42 * time.Millisecond)}
	merged := service.mergeAutoHealthState(store.state)
	if !reflect.DeepEqual(merged.Health, store.state.Health) {
		t.Fatal("stale daemon cache erased a CLI check")
	}
	// Newer metrics without a lookup must still keep the persisted identity.
	service.autoHealthState.health["one"] = domain.NodeHealth{LastCheckedAt: now.Add(2 * time.Minute), Healthy: false}
	merged = service.mergeAutoHealthState(store.state)
	if merged.Health["one"].CountryCode != "SE" || merged.Health["one"].Healthy || !merged.Health["one"].LastCheckedAt.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("incorrect independent metric/identity merge: %+v", merged.Health["one"])
	}
}
