package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/probe"
)

func TestAutoSelectableNodesExcludesKeywordMatches(t *testing.T) {
	t.Parallel()

	settings := domain.DefaultSettings()
	settings.AutoHideKeywords = []string{"LTE", "Russia"}
	sub := domain.Subscription{ID: "sub-1", Nodes: []domain.Node{
		{ID: "lte", Name: "Finland · LTE Reserve"},
		{ID: "russia", Name: "Россия", Remark: "Russia Premium"},
		{ID: "visible", Name: "Finland · Gaming"},
	}}

	nodes := autoSelectableNodes(sub, settings)
	if len(nodes) != 1 || nodes[0].ID != "visible" {
		t.Fatalf("unexpected selectable nodes: %+v", nodes)
	}
}

func TestSetAutoHideKeywordsReconnectsAwayFromMatchingActiveNode(t *testing.T) {
	t.Parallel()

	store := &memoryStore{
		settings: domain.DefaultSettings(),
		state: domain.RuntimeState{
			Connected:            true,
			Mode:                 domain.SelectionModeAuto,
			ActiveSubscriptionID: "sub-1",
			ActiveNodeID:         "node-lte",
			Health:               map[string]domain.NodeHealth{},
		},
		subs: []domain.Subscription{{
			ID: "sub-1",
			Nodes: []domain.Node{
				{ID: "node-lte", Name: "Finland", Remark: "LTE Reserve", Protocol: domain.ProtocolVLESS, Address: "lte.example.com", Port: 443, UUID: "11111111-1111-1111-1111-111111111111"},
				{ID: "node-main", Name: "Finland", Remark: "Gaming", Protocol: domain.ProtocolVLESS, Address: "main.example.com", Port: 443, UUID: "22222222-2222-2222-2222-222222222222"},
			},
		}},
	}
	store.settings.AutoMode = true
	store.settings.Mode = domain.SelectionModeAuto
	service := NewService(Dependencies{
		Store: store,
		Checker: fakeChecker{results: map[string]probe.Result{
			"node-lte":  {NodeID: "node-lte", Healthy: true, Latency: 10 * time.Millisecond, Checked: time.Now().UTC()},
			"node-main": {NodeID: "node-main", Healthy: true, Latency: 40 * time.Millisecond, Checked: time.Now().UTC()},
		}},
	})

	settings, err := service.SetSetting("auto.hide-keywords", " LTE\nRussia\nlte ")
	if err != nil {
		t.Fatalf("set auto hide keywords: %v", err)
	}
	if want := []string{"LTE", "Russia"}; !reflect.DeepEqual(settings.AutoHideKeywords, want) {
		t.Fatalf("unexpected keywords: want=%v got=%v", want, settings.AutoHideKeywords)
	}
	if store.state.ActiveNodeID != "node-main" {
		t.Fatalf("expected auto mode to leave keyword-hidden node, got %+v", store.state)
	}
}

func TestAutoHideKeywordsStillApplyAfterSubscriptionRefresh(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("vless://11111111-1111-1111-1111-111111111111@node.example.com:443?encryption=none&security=tls&type=tcp#Finland%20LTE%20Reserve"))
	}))
	defer server.Close()

	settings := domain.DefaultSettings()
	settings.AutoHideKeywords = []string{"LTE"}
	store := &memoryStore{
		settings: settings,
		state:    domain.DefaultRuntimeState(),
		subs: []domain.Subscription{{
			ID:         "sub-1",
			SourceType: domain.SourceTypeURL,
			Source:     server.URL,
			Nodes:      []domain.Node{{ID: "old", Name: "Old server"}},
		}},
	}
	service := NewService(Dependencies{Store: store, HTTPClient: server.Client()})

	refreshed, err := service.RefreshSubscription(context.Background(), "sub-1")
	if err != nil {
		t.Fatalf("refresh subscription: %v", err)
	}
	if len(refreshed.Nodes) != 1 || domain.MatchingAutoHideKeyword(store.settings.AutoHideKeywords, refreshed.Nodes[0]) != "LTE" {
		t.Fatalf("refreshed LTE node did not match persistent rule: %+v", refreshed.Nodes)
	}
	if nodes := autoSelectableNodes(refreshed, store.settings); len(nodes) != 0 {
		t.Fatalf("keyword-hidden refreshed node returned to auto pool: %+v", nodes)
	}
	if want := []string{"LTE"}; !reflect.DeepEqual(store.settings.AutoHideKeywords, want) {
		t.Fatalf("refresh changed persistent rules: want=%v got=%v", want, store.settings.AutoHideKeywords)
	}
}
