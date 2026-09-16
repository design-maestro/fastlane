package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/amneziawg"
	"github.com/design-maestro/fastlane/internal/backend"
	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/probe"
)

type awgProfileMemoryStore struct{ raw []byte }

func (s *awgProfileMemoryStore) SaveAWGProfile(raw []byte) error {
	s.raw = append([]byte(nil), raw...)
	return nil
}
func (s *awgProfileMemoryStore) LoadAWGProfile() ([]byte, error) {
	if len(s.raw) == 0 {
		return nil, errors.New("profile missing")
	}
	return append([]byte(nil), s.raw...), nil
}
func (s *awgProfileMemoryStore) RemoveAWGProfile() error { s.raw = nil; return nil }

type awgControllerFake struct {
	prepared     int
	connected    int
	disconnected int
	removed      int
	status       amneziawg.InterfaceStatus
}

func (c *awgControllerFake) Preflight(context.Context) (amneziawg.Compatibility, error) {
	return amneziawg.Compatibility{Compatible: true}, nil
}
func (c *awgControllerFake) Prepare(context.Context, amneziawg.Profile) error {
	c.prepared++
	return nil
}
func (c *awgControllerFake) Connect(context.Context) (amneziawg.InterfaceStatus, error) {
	c.connected++
	c.status = amneziawg.InterfaceStatus{Up: true, Device: "awg0", Address: "10.8.0.2", LastHandshake: 1}
	return c.status, nil
}
func (c *awgControllerFake) Disconnect(context.Context) error {
	c.disconnected++
	c.status = amneziawg.InterfaceStatus{}
	return nil
}
func (c *awgControllerFake) Remove(context.Context) error {
	c.removed++
	c.status = amneziawg.InterfaceStatus{}
	return nil
}
func (c *awgControllerFake) Status(context.Context) (amneziawg.InterfaceStatus, error) {
	return c.status, nil
}

type awgManagedBackend struct{ *managedRecordingBackend }

func (b *awgManagedBackend) PrepareInterfaceOutbound(context.Context, string, string, int) (string, error) {
	return "fastlane-node-awg-test", nil
}

func TestAWGImportRejectsHooksWithoutPersistingSecret(t *testing.T) {
	stateStore := &memoryStore{settings: domain.DefaultSettings(), state: domain.DefaultRuntimeState()}
	profileStore := &awgProfileMemoryStore{}
	service := NewService(Dependencies{Store: stateStore, AWGStore: profileStore})
	_, err := service.ImportAWGProfile("unsafe", []byte(validAWGProfile+"\nPostUp = touch /tmp/pwned\n"))
	if !errors.Is(err, amneziawg.ErrUnsafeDirective) {
		t.Fatalf("error = %v", err)
	}
	if len(profileStore.raw) != 0 {
		t.Fatal("unsafe profile was persisted")
	}
}

func TestAWGImportAcceptsLegacyAndReportsProtocol(t *testing.T) {
	stateStore := &memoryStore{settings: domain.DefaultSettings(), state: domain.DefaultRuntimeState()}
	profileStore := &awgProfileMemoryStore{}
	service := NewService(Dependencies{Store: stateStore, AWGStore: profileStore})
	legacy := strings.ReplaceAll(strings.ReplaceAll(validAWGProfile, "S3 = 0\n", ""), "S4 = 0\n", "")
	status, err := service.ImportAWGProfile("legacy", []byte(legacy))
	if err != nil {
		t.Fatalf("ImportAWGProfile: %v", err)
	}
	if status.Protocol != "AmneziaWG Legacy" || status.Profile == nil || status.Profile.Version != amneziawg.VersionLegacy {
		t.Fatalf("status = %+v", status)
	}
}

func TestAWGConnectUsesDedicatedOutboundWithoutReload(t *testing.T) {
	stateStore := &memoryStore{settings: domain.DefaultSettings(), state: domain.DefaultRuntimeState()}
	profileStore := &awgProfileMemoryStore{raw: []byte(validAWGProfile)}
	controller := &awgControllerFake{}
	managedBase := &managedRecordingBackend{recordingBackend: &recordingBackend{}, selected: "fastlane-direct"}
	managed := &awgManagedBackend{managedRecordingBackend: managedBase}
	service := NewService(Dependencies{Store: stateStore, AWGStore: profileStore, AWGController: controller, Backend: managed})
	service.managedOutboundProbe = func(context.Context, backend.ManagedBackend, int, string) error { return nil }

	if err := service.ConnectAWG(context.Background()); err != nil {
		t.Fatalf("ConnectAWG: %v", err)
	}
	if len(managed.recordingBackend.requests) != 0 {
		t.Fatalf("backend reloads = %d", len(managed.recordingBackend.requests))
	}
	if managed.selected != "fastlane-node-awg-test" {
		t.Fatalf("selected = %q", managed.selected)
	}
	if stateStore.state.ActiveConnectionKind != "amneziawg" || !stateStore.state.Connected || stateStore.state.OperationalMode != domain.OperationalModeVPN {
		t.Fatalf("state = %+v", stateStore.state)
	}
	if controller.prepared != 1 || controller.connected != 1 {
		t.Fatalf("controller prepare/connect = %d/%d", controller.prepared, controller.connected)
	}
}

func TestAWGFailedCheckDoesNotChangeUserRoute(t *testing.T) {
	state := domain.DefaultRuntimeState()
	state.SelectedOutboundTag = "fastlane-direct"
	stateStore := &memoryStore{settings: domain.DefaultSettings(), state: state}
	profileStore := &awgProfileMemoryStore{raw: []byte(validAWGProfile)}
	controller := &awgControllerFake{}
	managed := &awgManagedBackend{managedRecordingBackend: &managedRecordingBackend{recordingBackend: &recordingBackend{}, selected: "fastlane-direct"}}
	service := NewService(Dependencies{Store: stateStore, AWGStore: profileStore, AWGController: controller, Backend: managed})
	service.managedOutboundProbe = func(context.Context, backend.ManagedBackend, int, string) error { return errors.New("probe failed") }

	if _, err := service.CheckAWG(context.Background()); err == nil {
		t.Fatal("expected failed probe")
	}
	if managed.selected != "fastlane-direct" {
		t.Fatalf("active route changed to %q", managed.selected)
	}
	if stateStore.state.AWGLastProbe == nil || stateStore.state.AWGLastProbe.Success {
		t.Fatalf("probe state = %+v", stateStore.state.AWGLastProbe)
	}
}

func TestAWGRepeatedCheckReusesPreparedInterface(t *testing.T) {
	stateStore := &memoryStore{settings: domain.DefaultSettings(), state: domain.DefaultRuntimeState()}
	profileStore := &awgProfileMemoryStore{raw: []byte(validAWGProfile)}
	controller := &awgControllerFake{}
	managed := &awgManagedBackend{managedRecordingBackend: &managedRecordingBackend{recordingBackend: &recordingBackend{}, selected: "fastlane-direct"}}
	service := NewService(Dependencies{Store: stateStore, AWGStore: profileStore, AWGController: controller, Backend: managed})
	service.managedOutboundProbe = func(context.Context, backend.ManagedBackend, int, string) error { return nil }

	if _, err := service.CheckAWG(context.Background()); err != nil {
		t.Fatalf("first CheckAWG: %v", err)
	}
	if _, err := service.CheckAWG(context.Background()); err != nil {
		t.Fatalf("second CheckAWG: %v", err)
	}
	if controller.prepared != 1 || controller.connected != 1 {
		t.Fatalf("controller prepare/connect = %d/%d, want 1/1", controller.prepared, controller.connected)
	}
}

func TestAWGDisconnectPreservesActiveVLESSState(t *testing.T) {
	state := domain.DefaultRuntimeState()
	state.ActiveConnectionKind = "xray"
	state.ActiveSubscriptionID = "sub-vless"
	state.ActiveNodeID = "node-vless"
	state.Mode = domain.SelectionModeManual
	state.Connected = true
	state.OperationalMode = domain.OperationalModeVPN
	state.SelectedOutboundTag = "fastlane-node-vless"
	stateStore := &memoryStore{settings: domain.DefaultSettings(), state: state}
	controller := &awgControllerFake{}
	managed := &awgManagedBackend{managedRecordingBackend: &managedRecordingBackend{recordingBackend: &recordingBackend{}, selected: state.SelectedOutboundTag}}
	service := NewService(Dependencies{Store: stateStore, AWGController: controller, Backend: managed})

	if err := service.DisconnectAWG(context.Background()); err != nil {
		t.Fatalf("DisconnectAWG: %v", err)
	}
	if stateStore.state.ActiveConnectionKind != "xray" || !stateStore.state.Connected || stateStore.state.ActiveNodeID != "node-vless" {
		t.Fatalf("active VLESS state changed: %+v", stateStore.state)
	}
	if managed.selected != state.SelectedOutboundTag {
		t.Fatalf("active VLESS route changed to %q", managed.selected)
	}
	if controller.disconnected != 1 {
		t.Fatalf("prepared AWG interface disconnects = %d, want 1", controller.disconnected)
	}
}

func TestAWGDisconnectSynchronizesDisconnectedSettings(t *testing.T) {
	state := domain.DefaultRuntimeState()
	state.ActiveConnectionKind = "amneziawg"
	state.Mode = domain.SelectionModeManual
	state.Connected = true
	state.OperationalMode = domain.OperationalModeVPN
	state.SelectedOutboundTag = "fastlane-node-awg-test"
	settings := domain.DefaultSettings()
	settings.Mode = domain.SelectionModeManual
	stateStore := &memoryStore{settings: settings, state: state}
	managed := &awgManagedBackend{managedRecordingBackend: &managedRecordingBackend{recordingBackend: &recordingBackend{}, selected: state.SelectedOutboundTag}}
	service := NewService(Dependencies{Store: stateStore, AWGController: &awgControllerFake{}, Backend: managed})

	if err := service.DisconnectAWG(context.Background()); err != nil {
		t.Fatalf("DisconnectAWG: %v", err)
	}
	if stateStore.state.Mode != domain.SelectionModeDisconnected || stateStore.state.Connected {
		t.Fatalf("runtime state = %+v", stateStore.state)
	}
	if stateStore.settings.Mode != domain.SelectionModeDisconnected || stateStore.settings.AutoMode {
		t.Fatalf("settings = %+v", stateStore.settings)
	}
}

func TestAWGConnectReusesFreshSuccessfulCheck(t *testing.T) {
	stateStore := &memoryStore{settings: domain.DefaultSettings(), state: domain.DefaultRuntimeState()}
	profileStore := &awgProfileMemoryStore{raw: []byte(validAWGProfile)}
	controller := &awgControllerFake{}
	managed := &awgManagedBackend{managedRecordingBackend: &managedRecordingBackend{recordingBackend: &recordingBackend{status: backend.RuntimeStatus{Running: true}}, selected: "fastlane-direct"}}
	service := NewService(Dependencies{Store: stateStore, AWGStore: profileStore, AWGController: controller, Backend: managed})
	checks := 0
	service.managedOutboundProbe = func(context.Context, backend.ManagedBackend, int, string) error {
		checks++
		return nil
	}

	if _, err := service.CheckAWG(context.Background()); err != nil {
		t.Fatalf("CheckAWG: %v", err)
	}
	if err := service.ConnectAWG(context.Background()); err != nil {
		t.Fatalf("ConnectAWG: %v", err)
	}
	if checks != 1 {
		t.Fatalf("checks = %d, want one fresh check", checks)
	}
}

func TestAWGFailureUsesVLESSReserveAndKeepsManualMode(t *testing.T) {
	fallback := domain.Node{ID: "fallback", SubscriptionID: "sub-vless", Name: "VLESS reserve", Protocol: domain.ProtocolVLESS, Address: "fallback.example", Port: 443}
	state := domain.DefaultRuntimeState()
	state.ActiveConnectionKind = "amneziawg"
	state.ActiveSubscriptionID = awgSubscriptionID
	state.ActiveNodeID = awgNodeID
	state.Mode = domain.SelectionModeManual
	state.Connected = true
	state.OperationalMode = domain.OperationalModeVPN
	state.SelectedOutboundTag = "fastlane-node-awg-test"
	state.Health[fallback.ID] = domain.NodeHealth{NodeID: fallback.ID, Healthy: true, SuccessCount: 3, LastLatency: domain.NewDuration(80 * time.Millisecond)}
	stateStore := &memoryStore{settings: domain.DefaultSettings(), state: state, subs: []domain.Subscription{{ID: "sub-vless", Nodes: []domain.Node{fallback}}}}
	service := NewService(Dependencies{Store: stateStore, Backend: &recordingBackend{}, Checker: &countingProbeChecker{results: map[string]probe.Result{}, counts: map[string]int{}}})

	if err := service.RunConnectionFailover(context.Background(), "AmneziaWG failed"); err != nil {
		t.Fatalf("RunConnectionFailover: %v", err)
	}
	if stateStore.state.ActiveConnectionKind != "xray" || stateStore.state.ActiveNodeID != fallback.ID {
		t.Fatalf("fallback state = %+v", stateStore.state)
	}
	if stateStore.state.Mode != domain.SelectionModeManual || !stateStore.state.Connected {
		t.Fatalf("manual mode not preserved: %+v", stateStore.state)
	}
}

func TestAWGFallsBackDirectAndRequiresTwoChecksToRecover(t *testing.T) {
	state := domain.DefaultRuntimeState()
	state.ActiveConnectionKind = "amneziawg"
	state.ActiveSubscriptionID = awgSubscriptionID
	state.ActiveNodeID = awgNodeID
	state.Mode = domain.SelectionModeManual
	state.Connected = true
	state.OperationalMode = domain.OperationalModeVPN
	state.SelectedOutboundTag = "fastlane-node-awg-test"
	stateStore := &memoryStore{settings: domain.DefaultSettings(), state: state}
	profileStore := &awgProfileMemoryStore{raw: []byte(validAWGProfile)}
	controller := &awgControllerFake{status: amneziawg.InterfaceStatus{Up: true, Device: "awg0", Address: "10.8.0.2", LastHandshake: 1}}
	managed := &awgManagedBackend{managedRecordingBackend: &managedRecordingBackend{recordingBackend: &recordingBackend{status: backend.RuntimeStatus{Running: true}}, selected: "fastlane-node-awg-test"}}
	service := NewService(Dependencies{Store: stateStore, AWGStore: profileStore, AWGController: controller, Backend: managed})
	checks := 0
	service.managedOutboundProbe = func(context.Context, backend.ManagedBackend, int, string) error {
		checks++
		return nil
	}
	service.managedRecoveryDelay = time.Millisecond

	if err := service.RunConnectionFailover(context.Background(), "AmneziaWG failed"); err != nil {
		t.Fatalf("direct fallback: %v", err)
	}
	if stateStore.state.OperationalMode != domain.OperationalModeDirect || stateStore.state.Connected || managed.selected != "fastlane-direct" {
		t.Fatalf("direct state = %+v, selected=%q", stateStore.state, managed.selected)
	}
	if err := service.RunConnectionFailover(context.Background(), "recovery check"); err != nil {
		t.Fatalf("AWG recovery: %v", err)
	}
	if checks != 2 {
		t.Fatalf("recovery checks = %d, want 2", checks)
	}
	if stateStore.state.ActiveConnectionKind != "amneziawg" || !stateStore.state.Connected || stateStore.state.OperationalMode != domain.OperationalModeVPN {
		t.Fatalf("recovered state = %+v", stateStore.state)
	}
}

func TestRestoreUnavailableAWGFailsOpenWithoutStoppingXray(t *testing.T) {
	state := domain.DefaultRuntimeState()
	state.ActiveConnectionKind = "amneziawg"
	state.ActiveSubscriptionID = awgSubscriptionID
	state.ActiveNodeID = awgNodeID
	state.ActiveNodeName = "AWG stand"
	state.Mode = domain.SelectionModeManual
	state.Connected = true
	state.OperationalMode = domain.OperationalModeVPN
	state.ActiveTransport = domain.TransportModeProxy
	state.SelectedOutboundTag = "fastlane-node-awg-test"
	state.AWGLastProbe = &domain.AWGProbeState{Success: true, CheckedAt: time.Now().UTC()}
	stateStore := &memoryStore{settings: domain.DefaultSettings(), state: state}
	profileStore := &awgProfileMemoryStore{raw: []byte(validAWGProfile)}
	controller := &awgControllerFake{status: amneziawg.InterfaceStatus{Up: true, Device: "awg0", Address: "10.8.0.2", LastHandshake: 1}}
	base := &recordingBackend{status: backend.RuntimeStatus{Running: true}}
	managed := &awgManagedBackend{managedRecordingBackend: &managedRecordingBackend{recordingBackend: base, selected: state.SelectedOutboundTag}}
	service := NewService(Dependencies{Store: stateStore, AWGStore: profileStore, AWGController: controller, Backend: managed})
	service.backendEgressTimeout = 5 * time.Millisecond
	service.backendEgressRetryDelay = time.Millisecond
	service.backendEgressProbe = func(context.Context) error { return errors.New("tunnel unavailable") }

	if err := service.RestoreRuntime(context.Background()); err != nil {
		t.Fatalf("RestoreRuntime: %v", err)
	}
	if base.stopCalls != 0 || len(base.requests) != 0 {
		t.Fatalf("managed restore stopped or reloaded Xray: stops=%d reloads=%d", base.stopCalls, len(base.requests))
	}
	if managed.selected != "fastlane-direct" || managed.persistCalls != 1 {
		t.Fatalf("managed direct fallback selected=%q persist=%d", managed.selected, managed.persistCalls)
	}
	if stateStore.state.OperationalMode != domain.OperationalModeDirect || stateStore.state.Connected {
		t.Fatalf("direct state = %+v", stateStore.state)
	}
	if stateStore.state.ActiveConnectionKind != "amneziawg" || stateStore.state.Mode != domain.SelectionModeManual {
		t.Fatalf("AWG recovery identity was lost: %+v", stateStore.state)
	}
	if stateStore.state.CurrentOperation != nil {
		t.Fatalf("switch intent was not resolved: %+v", stateStore.state.CurrentOperation)
	}
}

const validAWGProfile = `[Interface]
PrivateKey = AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=
Address = 10.8.0.2/32
Jc = 4
Jmin = 40
Jmax = 70
S1 = 0
S2 = 0
S3 = 0
S4 = 0
H1 = 1
H2 = 2
H3 = 3
H4 = 4

[Peer]
PublicKey = AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=
AllowedIPs = 0.0.0.0/0
Endpoint = 198.51.100.1:51820
PersistentKeepalive = 25
`
