package app

import (
	"context"
	"errors"
	"testing"

	"github.com/design-maestro/fastlane/internal/backend"
	"github.com/design-maestro/fastlane/internal/domain"
)

func TestSetCountryRoutingFromDisabledRoutingActivatesDefaultProxyFirewall(t *testing.T) {
	t.Parallel()

	settings := domain.DefaultSettings()
	settings.Firewall.Enabled = false
	settings.Firewall.Mode = domain.FirewallModeDisabled
	settings.Firewall.Hosts = []string{"192.168.1.50"}
	settings.Firewall.Targets = domain.FirewallSelectorSet{Domains: []string{"example.com"}}
	settings.Firewall.Split = domain.DefaultFirewallSplitSettings()

	state := domain.DefaultRuntimeState()
	state.Connected = true
	state.Mode = domain.SelectionModeManual
	state.ActiveSubscriptionID = "sub-1"
	state.ActiveNodeID = "node-1"

	store := &memoryStore{
		settings: settings,
		state:    state,
		subs: []domain.Subscription{{
			ID: "sub-1",
			Nodes: []domain.Node{{
				ID:       "node-1",
				Name:     "Germany",
				Protocol: domain.ProtocolVLESS,
				Address:  "203.0.113.10",
				Port:     443,
				UUID:     "11111111-1111-1111-1111-111111111111",
			}},
		}},
	}
	backendRuntime := &recordingBackend{}
	firewall := &recordingFirewaller{}
	service := NewService(Dependencies{
		Store:      store,
		Backend:    backendRuntime,
		Firewaller: firewall,
	})

	if _, err := service.SetSetting("country-routing.country", "DE"); err != nil {
		t.Fatalf("choose country: %v", err)
	}
	updated, err := service.SetSetting("country-routing.enabled", "true")
	if err != nil {
		t.Fatalf("enable country routing from the one-step setting: %v", err)
	}
	if !updated.CountryRouting.Enabled || updated.CountryRouting.CountryCode != "DE" {
		t.Fatalf("country routing was not enabled: %+v", updated.CountryRouting)
	}
	if !updated.Firewall.Enabled || updated.Firewall.Mode != domain.FirewallModeSplit {
		t.Fatalf("one-step country routing must enable split firewall routing: %+v", updated.Firewall)
	}
	if updated.Firewall.Split.DefaultAction != domain.FirewallDefaultActionProxy {
		t.Fatalf("one-step country routing must use VPN as the non-local-country default: %+v", updated.Firewall.Split)
	}
	if !domain.FirewallRoutingEnabled(updated.Firewall) {
		t.Fatalf("one-step country routing produced a non-working firewall contract: %+v", updated.Firewall)
	}
	if len(updated.Firewall.Hosts) != 0 || domain.FirewallSelectorSetHasEntries(updated.Firewall.Targets) {
		t.Fatalf("stale disabled-mode selectors leaked into the default contract: %+v", updated.Firewall)
	}

	if len(backendRuntime.requests) != 1 {
		t.Fatalf("expected the active backend to be reapplied once, got %d", len(backendRuntime.requests))
	}
	request := backendRuntime.requests[0]
	if !request.TransparentProxy || request.TransparentSelectiveCapture {
		t.Fatalf("default-proxy country routing must capture the whole LAN, got %+v", request)
	}
	if !request.TransparentCountryRouting.Enabled || request.TransparentCountryRouting.CountryCode != "DE" || request.TransparentDefaultAction != domain.FirewallDefaultActionProxy {
		t.Fatalf("backend did not receive the country-routing/default-proxy contract: %+v", request)
	}
	if len(firewall.applied) != 1 || !domain.FirewallRoutingEnabled(firewall.applied[0]) {
		t.Fatalf("working firewall rules were not applied once: %+v", firewall.applied)
	}
}

func TestSetCountryRoutingRestoresPreviousSettingsAndRuntimeWhenApplyFails(t *testing.T) {
	t.Parallel()

	settings := domain.DefaultSettings()
	settings.CountryRouting = domain.CountryRouting{CountryCode: "IR"}
	settings.Firewall.Enabled = false
	settings.Firewall.Mode = domain.FirewallModeDisabled

	state := domain.DefaultRuntimeState()
	state.Connected = true
	state.Mode = domain.SelectionModeManual
	state.ActiveSubscriptionID = "sub-1"
	state.ActiveNodeID = "node-1"

	store := &memoryStore{
		settings: settings,
		state:    state,
		subs: []domain.Subscription{{
			ID: "sub-1",
			Nodes: []domain.Node{{
				ID:       "node-1",
				Name:     "Germany",
				Protocol: domain.ProtocolVLESS,
				Address:  "203.0.113.10",
				Port:     443,
				UUID:     "11111111-1111-1111-1111-111111111111",
			}},
		}},
	}
	backendRuntime := &failOnceCountryRoutingBackend{failures: 1}
	firewall := &recordingFirewaller{}
	service := NewService(Dependencies{
		Store:      store,
		Backend:    backendRuntime,
		Firewaller: firewall,
	})

	if _, err := service.SetSetting("country-routing.enabled", "true"); err == nil {
		t.Fatal("expected the failed runtime apply to be reported")
	}
	if store.settings.CountryRouting.Enabled || store.settings.CountryRouting.CountryCode != "IR" {
		t.Fatal("failed activation must restore the previous country-routing setting")
	}
	if store.settings.Firewall.Enabled || store.settings.Firewall.Mode != domain.FirewallModeDisabled {
		t.Fatalf("failed activation must restore the previous firewall settings: %+v", store.settings.Firewall)
	}
	if len(backendRuntime.requests) != 2 {
		t.Fatalf("expected failed candidate apply followed by previous runtime restore, got %d requests", len(backendRuntime.requests))
	}
	if !backendRuntime.requests[0].TransparentCountryRouting.Enabled || !backendRuntime.requests[0].TransparentProxy {
		t.Fatalf("first request did not contain the candidate country-routing runtime: %+v", backendRuntime.requests[0])
	}
	if backendRuntime.requests[1].TransparentCountryRouting.Enabled || backendRuntime.requests[1].TransparentProxy {
		t.Fatalf("second request did not restore the previous runtime: %+v", backendRuntime.requests[1])
	}
	if firewall.disableCalls != 1 {
		t.Fatalf("previous disabled firewall runtime was not restored: %+v", firewall)
	}
}

type failOnceCountryRoutingBackend struct {
	recordingBackend
	failures int
}

func (b *failOnceCountryRoutingBackend) ApplyConfig(_ context.Context, request backend.ConfigRequest) error {
	b.requests = append(b.requests, request)
	if b.failures > 0 {
		b.failures--
		return errors.New("candidate runtime rejected")
	}
	return nil
}
