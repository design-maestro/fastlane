package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/design-maestro/fastlane/internal/amneziawg"
	"github.com/design-maestro/fastlane/internal/backend"
	"github.com/design-maestro/fastlane/internal/domain"
)

const (
	awgSubscriptionID = "amneziawg"
	awgNodeID         = "profile"
)

// AWGStatus is a secret-free projection for CLI and LuCI.
type AWGStatus struct {
	State         string                    `json:"state"`
	Name          string                    `json:"name,omitempty"`
	Protocol      string                    `json:"protocol"`
	Profile       *amneziawg.RedactedStatus `json:"profile,omitempty"`
	Compatibility *amneziawg.Compatibility  `json:"compatibility,omitempty"`
	Interface     amneziawg.InterfaceStatus `json:"interface"`
	LastProbe     *domain.AWGProbeState     `json:"last_probe,omitempty"`
	Active        bool                      `json:"active"`
	Message       string                    `json:"message,omitempty"`
}

// ImportAWGProfile validates before persisting and never places key material
// in runtime state or logs.
func (s *Service) ImportAWGProfile(name string, raw []byte) (AWGStatus, error) {
	return runStoreWriteLockedResult(s, func() (AWGStatus, error) {
		if s.awgStore == nil {
			return AWGStatus{}, fmt.Errorf("AmneziaWG profile storage is not configured")
		}
		profile, err := amneziawg.Parse(raw)
		if err != nil {
			return AWGStatus{}, err
		}
		state, err := s.store.LoadState()
		if err != nil {
			return AWGStatus{}, fmt.Errorf("load state: %w", err)
		}
		if state.ActiveConnectionKind == "amneziawg" && state.Connected {
			return AWGStatus{}, fmt.Errorf("disconnect the active AmneziaWG profile before replacing it")
		}
		// A prepared interface belongs to the previously persisted profile. Tear
		// it down before replacing the secret so subsequent checks can safely
		// treat an already-up interface as matching the stored profile.
		if s.awgController != nil {
			if err := s.awgController.Remove(context.Background()); err != nil {
				return AWGStatus{}, fmt.Errorf("remove previously prepared AmneziaWG interface: %w", err)
			}
		}
		if err := s.awgStore.SaveAWGProfile(raw); err != nil {
			return AWGStatus{}, fmt.Errorf("save AmneziaWG profile: %w", err)
		}
		name = strings.TrimSpace(name)
		if name == "" {
			name = profile.Peer.Endpoint
		}
		state.AWGProfileName = name
		state.AWGLastProbe = nil
		if err := s.saveState(state); err != nil {
			return AWGStatus{}, fmt.Errorf("save AmneziaWG profile metadata: %w", err)
		}
		return s.awgStatus(context.Background())
	})
}

func (s *Service) GetAWGStatus(ctx context.Context) (AWGStatus, error) {
	return s.awgStatus(ctx)
}

func (s *Service) awgStatus(ctx context.Context) (AWGStatus, error) {
	result := AWGStatus{State: "absent", Protocol: "AmneziaWG 2.0"}
	if s.awgStore == nil {
		result.Message = "AmneziaWG profile storage is not configured"
		return result, nil
	}
	raw, err := s.awgStore.LoadAWGProfile()
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("load AmneziaWG profile: %w", err)
	}
	profile, err := amneziawg.Parse(raw)
	if err != nil {
		result.State = "invalid"
		result.Message = err.Error()
		return result, nil
	}
	redacted := profile.Status()
	result.Profile = &redacted
	state, stateErr := s.store.LoadState()
	if stateErr != nil {
		return result, fmt.Errorf("load state: %w", stateErr)
	}
	result.Name = firstNonEmpty(strings.TrimSpace(state.AWGProfileName), profile.Peer.Endpoint)
	result.LastProbe = state.AWGLastProbe
	result.Active = state.ActiveConnectionKind == "amneziawg" && state.Connected && state.OperationalMode == domain.OperationalModeVPN
	result.State = "imported"
	if state.ActiveConnectionKind == "amneziawg" && state.OperationalMode == domain.OperationalModeDirect {
		result.State = "direct"
		result.Message = "VPN unavailable; internet is direct"
	}
	if s.awgController == nil {
		result.Message = "AmneziaWG is available only on a compatible OpenWrt stand"
		return result, nil
	}
	compat, compatErr := s.awgController.Preflight(ctx)
	result.Compatibility = &compat
	if compatErr != nil {
		result.State = "incompatible"
		result.Message = compatErr.Error()
		return result, nil
	}
	if iface, ifaceErr := s.awgController.Status(ctx); ifaceErr == nil {
		result.Interface = iface
		if iface.Up {
			result.State = "prepared"
		}
	}
	if result.LastProbe != nil && !result.LastProbe.Success {
		result.State = "probe_failed"
		result.Message = result.LastProbe.Error
	}
	if result.Active {
		result.State = "connected"
		result.Message = ""
	}
	return result, nil
}

func (s *Service) CheckAWG(ctx context.Context) (AWGStatus, error) {
	return runStoreWriteLockedResult(s, func() (AWGStatus, error) {
		_, _, err := s.checkAWGLocked(ctx, true)
		status, statusErr := s.awgStatus(ctx)
		if err != nil {
			return status, err
		}
		return status, statusErr
	})
}

func (s *Service) checkAWGLocked(ctx context.Context, prepare bool) (string, amneziawg.InterfaceStatus, error) {
	profile, err := s.loadAWGProfile()
	if err != nil {
		return "", amneziawg.InterfaceStatus{}, err
	}
	if s.awgController == nil {
		return "", amneziawg.InterfaceStatus{}, fmt.Errorf("AmneziaWG controller is not configured")
	}
	if prepare {
		prepared, statusErr := s.awgController.Status(ctx)
		if statusErr != nil || !prepared.Up || prepared.Device == "" || prepared.Address == "" {
			if err := s.awgController.Prepare(ctx, profile); err != nil {
				return "", amneziawg.InterfaceStatus{}, err
			}
		}
	}
	iface, err := s.awgController.Status(ctx)
	if err != nil || !iface.Up || iface.Device == "" || iface.Address == "" {
		iface, err = s.awgController.Connect(ctx)
	}
	if err != nil {
		return "", iface, err
	}
	managed, err := s.ensureAWGManagedRuntime(ctx)
	if err != nil {
		return "", iface, err
	}
	started := time.Now()
	tag, err := managed.PrepareInterfaceOutbound(ctx, iface.Device, iface.Address, amneziawg.RouteMark)
	if err == nil {
		probeCandidate := s.probeManagedOutbound
		if s.managedOutboundProbe != nil {
			probeCandidate = func(probeCtx context.Context, probeBackend backend.ManagedBackend, slot int, outboundTag string) error {
				return s.managedOutboundProbe(probeCtx, probeBackend, slot, outboundTag)
			}
		}
		err = probeCandidate(ctx, managed, 0, tag)
	}
	probeState := &domain.AWGProbeState{Success: err == nil, CheckedAt: s.currentTime().UTC(), LatencyMS: float64(time.Since(started)) / float64(time.Millisecond)}
	if err != nil {
		probeState.Error = err.Error()
		probeState.LatencyMS = 0
	}
	state, stateErr := s.store.LoadState()
	if stateErr == nil {
		state.AWGLastProbe = probeState
		if saveErr := s.saveState(state); saveErr != nil && err == nil {
			err = saveErr
		}
	}
	return tag, iface, err
}

func (s *Service) ConnectAWG(ctx context.Context) error {
	return runStoreWriteLocked(s, func() error { return s.connectAWGLocked(ctx) })
}

func (s *Service) connectAWGLocked(ctx context.Context) error {
	var tag string
	stateBefore, stateErr := s.store.LoadState()
	if stateErr == nil && awgProbeIsFresh(stateBefore.AWGLastProbe, s.currentTime().UTC(), time.Minute) && s.awgController != nil {
		if iface, statusErr := s.awgController.Status(ctx); statusErr == nil && iface.Up && iface.Device != "" && iface.Address != "" {
			if managed, runtimeErr := s.ensureAWGManagedRuntime(ctx); runtimeErr == nil {
				tag, _ = managed.PrepareInterfaceOutbound(ctx, iface.Device, iface.Address, amneziawg.RouteMark)
			}
		}
	}
	if tag == "" {
		var err error
		tag, _, err = s.checkAWGLocked(ctx, true)
		if err != nil {
			return fmt.Errorf("verify AmneziaWG route: %w", err)
		}
	}
	managed, ok := s.backend.(backend.ManagedBackend)
	if !ok {
		return fmt.Errorf("Xray backend does not support live route management")
	}
	state, err := s.store.LoadState()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	previousState := state
	previousTag := strings.TrimSpace(state.SelectedOutboundTag)
	if previousTag == "" {
		previousTag = "fastlane-direct"
	}
	now := s.currentTime().UTC()
	intent := state
	intent.OperationalMode = domain.OperationalModeRecovering
	intent.RuntimeConfigGeneration++
	intent.CurrentOperation = &domain.RuntimeOperation{Kind: "switch_to_amneziawg", From: previousTag, To: tag, StartedAt: now}
	if err := s.saveState(intent); err != nil {
		return fmt.Errorf("save AmneziaWG switch intent: %w", err)
	}
	if err := managed.SelectOutbound(ctx, tag); err != nil {
		state.CurrentOperation = nil
		_ = s.saveState(state)
		return fmt.Errorf("select AmneziaWG outbound: %w", err)
	}
	settings, err := s.store.LoadSettings()
	if err != nil {
		_ = managed.SelectOutbound(ctx, previousTag)
		state.CurrentOperation = nil
		_ = s.saveState(state)
		return fmt.Errorf("load settings: %w", err)
	}
	if reason, egressErr := s.ensureBackendEgress(ctx, settings, awgSubscriptionID, awgNodeID, domain.SelectionModeManual); egressErr != nil {
		_ = managed.SelectOutbound(ctx, previousTag)
		state.CurrentOperation = nil
		_ = s.saveState(state)
		return fmt.Errorf("%s: %w", reason, egressErr)
	}
	state = intent
	state.ActiveConnectionKind = "amneziawg"
	state.ActiveSubscriptionID = awgSubscriptionID
	state.ActiveNodeID = awgNodeID
	state.ActiveNodeName = firstNonEmpty(strings.TrimSpace(state.AWGProfileName), "AmneziaWG")
	state.Mode = domain.SelectionModeManual
	state.AutoScope = ""
	state.Connected = true
	state.OperationalMode = domain.OperationalModeVPN
	state.ActiveTransport = domain.TransportModeProxy
	state.SelectedOutboundTag = tag
	state.RuntimeConfigVersion = tag
	state.CurrentOperation = nil
	state.RuntimeOutbounds = updateRuntimeOutbound(state.RuntimeOutbounds, domain.RuntimeOutboundState{Tag: tag, SubscriptionID: awgSubscriptionID, NodeID: awgNodeID, Role: "active", VerifiedAt: now})
	if previousTag != "fastlane-direct" && previousTag != tag {
		state.RuntimeOutbounds = updateRuntimeOutbound(state.RuntimeOutbounds, domain.RuntimeOutboundState{Tag: previousTag, Role: "draining", RetireAfter: now.Add(5 * time.Minute), RemoveBy: now.Add(30 * time.Minute)})
	}
	state.LastSuccessAt = now
	state.LastSwitchAt = now
	state.LastSwitchReason = "manual switch to AmneziaWG"
	state.LastFailureReason = ""
	state.LastTransportFailureReason = ""
	if err := s.saveState(state); err != nil {
		_ = managed.SelectOutbound(ctx, previousTag)
		previousState.CurrentOperation = nil
		_ = s.saveState(previousState)
		return fmt.Errorf("save AmneziaWG state: %w", err)
	}
	settings.AutoMode = false
	settings.Mode = domain.SelectionModeManual
	_ = s.store.SaveSettings(settings)
	return nil
}

func awgProbeIsFresh(probe *domain.AWGProbeState, now time.Time, maxAge time.Duration) bool {
	if probe == nil || !probe.Success || probe.CheckedAt.IsZero() || maxAge <= 0 {
		return false
	}
	age := now.Sub(probe.CheckedAt)
	return age >= 0 && age <= maxAge
}

func (s *Service) ensureAWGManagedRuntime(ctx context.Context) (backend.InterfaceManagedBackend, error) {
	return s.ensureAWGManagedRuntimeWithReload(ctx, false)
}

func (s *Service) ensureAWGManagedRuntimeWithReload(ctx context.Context, forceReload bool) (backend.InterfaceManagedBackend, error) {
	managed, ok := s.backend.(backend.InterfaceManagedBackend)
	if !ok {
		return nil, fmt.Errorf("Xray backend does not support interface outbounds")
	}
	status, statusErr := s.backend.Status(ctx)
	if !forceReload && statusErr == nil && status.Running {
		if _, err := managed.SelectedOutbound(ctx); err == nil {
			return managed, nil
		}
		// A freshly migrated managed runtime can expose the RoutingService before
		// the main balancer has an override. Pin the persisted route (or direct on
		// a disconnected runtime) through the API instead of misclassifying this
		// valid state as an old Xray configuration and reloading everything.
		if state, stateErr := s.store.LoadState(); stateErr == nil {
			target := strings.TrimSpace(state.SelectedOutboundTag)
			if target == "" || state.OperationalMode == domain.OperationalModeDirect || !state.Connected {
				if err := managed.SelectDirect(ctx); err == nil {
					return managed, nil
				}
			} else if err := managed.SelectOutbound(ctx, target); err == nil {
				return managed, nil
			}
		}
	}
	settings, err := s.store.LoadSettings()
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}
	runtimeSettings, err := s.prepareRuntimeDNSSettings(ctx, settings)
	if err != nil {
		return nil, fmt.Errorf("prepare DNS: %w", err)
	}
	req := s.backendConfigRequest(runtimeSettings, domain.Node{}, domain.SelectionModeManual, 10808, 10809, firewallEnabled(settings.Firewall), s.dns != nil && localDNSRuntimeEnabled(settings.DNS))
	req.Nodes = nil
	req.SelectedNodeID = ""
	req.StartDirect = true
	if err := s.backend.ApplyConfig(ctx, req); err != nil {
		return nil, fmt.Errorf("install Xray managed runtime: %w", err)
	}
	if reason, err := s.ensureBackendRunning(ctx, awgSubscriptionID, awgNodeID, domain.SelectionModeManual); err != nil {
		return nil, fmt.Errorf("%s: %w", reason, err)
	}
	if s.dns != nil {
		if localDNSRuntimeEnabled(settings.DNS) {
			if err := s.dns.Apply(ctx, settings.DNS, localDNSListen, localDNSPort); err != nil {
				return nil, fmt.Errorf("apply DNS runtime: %w", err)
			}
		} else if err := s.dns.Disable(ctx); err != nil {
			return nil, fmt.Errorf("disable DNS runtime: %w", err)
		}
	}
	if s.firewall != nil {
		if domain.FirewallRoutingEnabled(settings.Firewall) {
			runtimeFirewall := domain.CanonicalFirewallSettings(settings.Firewall)
			runtimeFirewall.BlockQUIC = s.managedTransparentBlockQUIC(settings.Firewall)
			if err := s.firewall.Apply(ctx, runtimeFirewall); err != nil {
				return nil, fmt.Errorf("apply firewall: %w", err)
			}
		} else if err := s.firewall.Disable(ctx); err != nil {
			return nil, fmt.Errorf("disable firewall: %w", err)
		}
	}
	return managed, nil
}

func (s *Service) DisconnectAWG(ctx context.Context) error {
	return runStoreWriteLocked(s, func() error {
		state, err := s.store.LoadState()
		if err != nil {
			return err
		}
		active := state.ActiveConnectionKind == "amneziawg"
		if managed, ok := s.backend.(backend.ManagedBackend); ok && active {
			if err := managed.SelectDirect(ctx); err != nil {
				return fmt.Errorf("select direct route: %w", err)
			}
		}
		if s.awgController != nil {
			if err := s.awgController.Disconnect(ctx); err != nil {
				return err
			}
		}
		if !active {
			return nil
		}
		state.ActiveConnectionKind = ""
		state.ActiveSubscriptionID = ""
		state.ActiveNodeID = ""
		state.ActiveNodeName = ""
		state.Mode = domain.SelectionModeDisconnected
		state.Connected = false
		state.OperationalMode = domain.OperationalModeDirect
		state.ActiveTransport = domain.TransportModeDirect
		state.SelectedOutboundTag = "fastlane-direct"
		state.CurrentOperation = nil
		state.LastSwitchAt = s.currentTime().UTC()
		state.LastSwitchReason = "AmneziaWG disconnected"
		if err := s.saveState(state); err != nil {
			return err
		}
		settings, err := s.store.LoadSettings()
		if err != nil {
			return fmt.Errorf("load settings: %w", err)
		}
		settings.AutoMode = false
		settings.Mode = domain.SelectionModeDisconnected
		return s.store.SaveSettings(settings)
	})
}

func (s *Service) RemoveAWG(ctx context.Context) error {
	return runStoreWriteLocked(s, func() error {
		state, err := s.store.LoadState()
		if err != nil {
			return err
		}
		tag := ""
		if state.ActiveConnectionKind == "amneziawg" {
			tag = state.SelectedOutboundTag
			if managed, ok := s.backend.(backend.ManagedBackend); ok {
				if err := managed.SelectDirect(ctx); err != nil {
					return err
				}
			}
		}
		if s.awgController != nil {
			if err := s.awgController.Remove(ctx); err != nil {
				return err
			}
		}
		if tag != "" {
			if managed, ok := s.backend.(backend.ManagedBackend); ok {
				_ = managed.RemoveOutbound(ctx, tag)
			}
		}
		if s.awgStore != nil {
			if err := s.awgStore.RemoveAWGProfile(); err != nil {
				return err
			}
		}
		state.AWGProfileName = ""
		state.AWGLastProbe = nil
		if state.ActiveConnectionKind == "amneziawg" {
			state.ActiveConnectionKind = ""
			state.ActiveSubscriptionID = ""
			state.ActiveNodeID = ""
			state.ActiveNodeName = ""
			state.Mode = domain.SelectionModeDisconnected
			state.Connected = false
			state.OperationalMode = domain.OperationalModeDirect
			state.ActiveTransport = domain.TransportModeDirect
			state.SelectedOutboundTag = "fastlane-direct"
		}
		if err := s.saveState(state); err != nil {
			return err
		}
		if tag == "" {
			return nil
		}
		settings, err := s.store.LoadSettings()
		if err != nil {
			return fmt.Errorf("load settings: %w", err)
		}
		settings.AutoMode = false
		settings.Mode = domain.SelectionModeDisconnected
		return s.store.SaveSettings(settings)
	})
}

func (s *Service) loadAWGProfile() (amneziawg.Profile, error) {
	if s.awgStore == nil {
		return amneziawg.Profile{}, fmt.Errorf("AmneziaWG profile storage is not configured")
	}
	raw, err := s.awgStore.LoadAWGProfile()
	if err != nil {
		return amneziawg.Profile{}, fmt.Errorf("load AmneziaWG profile: %w", err)
	}
	return amneziawg.Parse(raw)
}
