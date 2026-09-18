package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/design-maestro/fastlane/internal/amneziawg"
	"github.com/design-maestro/fastlane/internal/backend"
	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/probe"
)

const (
	awgSubscriptionID = "server-list"
	legacyAWGNodeID   = "profile"
	awgNodeID         = legacyAWGNodeID
)

// AWGStatus is a secret-free projection for CLI and LuCI.
type AWGStatus struct {
	ID            string                    `json:"id"`
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
		if err := s.migrateLegacyAWGProfileLocked(); err != nil {
			return AWGStatus{}, err
		}
		id := profile.StableID()
		name = strings.TrimSpace(name)
		if name == "" {
			name = profile.Peer.Endpoint
		}
		if _, err := s.awgStore.SaveAWGProfile(amneziawg.ProfileMetadata{ID: id, Name: name}, raw); err != nil {
			return AWGStatus{}, fmt.Errorf("save AmneziaWG profile: %w", err)
		}
		return s.awgStatusLocked(context.Background(), id)
	})
}

func (s *Service) GetAWGStatus(ctx context.Context) (AWGStatus, error) {
	return runStoreWriteLockedResult(s, func() (AWGStatus, error) {
		if err := s.migrateLegacyAWGProfileLocked(); err != nil {
			return AWGStatus{}, err
		}
		id, err := s.resolveAWGProfileIDLocked("")
		if errors.Is(err, os.ErrNotExist) {
			return AWGStatus{State: "absent", Protocol: "AmneziaWG"}, nil
		}
		if err != nil {
			return AWGStatus{}, err
		}
		return s.awgStatusLocked(ctx, id)
	})
}

func (s *Service) GetAWGStatusByID(ctx context.Context, id string) (AWGStatus, error) {
	return runStoreWriteLockedResult(s, func() (AWGStatus, error) {
		if err := s.migrateLegacyAWGProfileLocked(); err != nil {
			return AWGStatus{}, err
		}
		resolved, err := s.resolveAWGProfileIDLocked(id)
		if err != nil {
			return AWGStatus{}, err
		}
		return s.awgStatusLocked(ctx, resolved)
	})
}

func (s *Service) ListAWGStatuses(ctx context.Context) ([]AWGStatus, error) {
	return runStoreWriteLockedResult(s, func() ([]AWGStatus, error) {
		if s.awgStore == nil {
			return []AWGStatus{}, nil
		}
		if err := s.migrateLegacyAWGProfileLocked(); err != nil {
			return nil, err
		}
		profiles, err := s.awgStore.ListAWGProfiles()
		if err != nil {
			return nil, err
		}
		statuses := make([]AWGStatus, 0, len(profiles))
		for _, metadata := range profiles {
			status, statusErr := s.awgStatusLocked(ctx, metadata.ID)
			if statusErr != nil {
				return nil, statusErr
			}
			statuses = append(statuses, status)
		}
		sort.SliceStable(statuses, func(i, j int) bool { return strings.ToLower(statuses[i].Name) < strings.ToLower(statuses[j].Name) })
		return statuses, nil
	})
}

// subscriptionsWithAWGProfiles projects imported AWG profiles into the
// existing Server List subscription for shared filtering, health ranking and
// automatic selection. The projection contains no key material and is never
// persisted as a subscription payload.
func (s *Service) subscriptionsWithAWGProfiles(subscriptions []domain.Subscription) ([]domain.Subscription, error) {
	cloned := make([]domain.Subscription, len(subscriptions))
	for index := range subscriptions {
		cloned[index] = subscriptions[index]
		cloned[index].Nodes = append([]domain.Node(nil), subscriptions[index].Nodes...)
	}
	if s.awgStore == nil {
		return cloned, nil
	}
	profiles, err := s.awgStore.ListAWGProfiles()
	if err != nil {
		return nil, fmt.Errorf("list AmneziaWG profiles: %w", err)
	}
	if len(profiles) == 0 {
		return cloned, nil
	}
	serverList := -1
	for index := range cloned {
		if cloned[index].ID == awgSubscriptionID {
			serverList = index
			break
		}
	}
	if serverList < 0 {
		cloned = append(cloned, domain.Subscription{
			ID: awgSubscriptionID, SourceType: domain.SourceTypeRaw,
			ProviderName: "Server List", DisplayName: "Server List", ParserStatus: "ok",
		})
		serverList = len(cloned) - 1
	}
	for _, metadata := range profiles {
		raw, loadErr := s.awgStore.LoadAWGProfile(metadata.ID)
		if loadErr != nil {
			continue
		}
		profile, parseErr := amneziawg.Parse(raw)
		if parseErr != nil {
			continue
		}
		cloned[serverList].Nodes = append(cloned[serverList].Nodes, domain.Node{
			ID: metadata.ID, SubscriptionID: awgSubscriptionID,
			Name:         firstNonEmpty(strings.TrimSpace(metadata.Name), profile.Peer.Endpoint),
			ProviderName: "Server List", Protocol: domain.ProtocolAmneziaWG,
			Address: profile.Peer.Endpoint,
		})
	}
	return cloned, nil
}

// probeAWGProfilesForAuto refreshes eligible AWG observations before the
// normal Xray pool is ranked. While one AWG profile is active, only that
// profile is touched because the current prototype owns one netifd interface.
func (s *Service) probeAWGProfilesForAuto(ctx context.Context, scope string) {
	if s.awgStore == nil || (scope != "" && scope != autoScopeAll && scope != awgSubscriptionID) {
		return
	}
	settings, err := s.store.LoadSettings()
	if err != nil {
		return
	}
	state, err := s.store.LoadState()
	if err != nil {
		return
	}
	subscriptions, err := s.subscriptionsWithAWGProfiles(nil)
	if err != nil || len(subscriptions) == 0 {
		return
	}
	var nodes []domain.Node
	for _, subscription := range subscriptions {
		if subscription.ID == awgSubscriptionID {
			nodes = subscription.Nodes
			break
		}
	}
	for _, node := range nodes {
		if node.Protocol != domain.ProtocolAmneziaWG || domain.IsNodeExcludedFromAuto(settings, awgSubscriptionID, node) {
			continue
		}
		_, isolated := s.awgController.(amneziawg.IsolatedProbeController)
		if !isolated && state.ActiveConnectionKind == "amneziawg" && state.ActiveAWGProfileID != "" && state.ActiveAWGProfileID != node.ID {
			continue
		}
		if _, checkErr := s.CheckAWGProfile(ctx, node.ID); checkErr != nil && ctx.Err() != nil {
			return
		}
	}
}

func (s *Service) awgStatusLocked(ctx context.Context, id string) (AWGStatus, error) {
	result := AWGStatus{ID: id, State: "absent", Protocol: "AmneziaWG"}
	if s.awgStore == nil {
		result.Message = "AmneziaWG profile storage is not configured"
		return result, nil
	}
	metadata, err := s.awgProfileMetadataLocked(id)
	if err != nil {
		return result, err
	}
	raw, err := s.awgStore.LoadAWGProfile(id)
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
	if profile.Version == amneziawg.VersionLegacy {
		result.Protocol = "AmneziaWG Legacy"
	} else if profile.Version == amneziawg.Version31 {
		result.Protocol = "AmneziaWG 3.1"
	} else {
		result.Protocol = "AmneziaWG 2.0"
	}
	state, stateErr := s.store.LoadState()
	if stateErr != nil {
		return result, fmt.Errorf("load state: %w", stateErr)
	}
	result.Name = firstNonEmpty(strings.TrimSpace(metadata.Name), profile.Peer.Endpoint)
	if probe, ok := state.AWGProfileProbes[id]; ok {
		probeCopy := probe
		result.LastProbe = &probeCopy
	} else if state.ActiveAWGProfileID == id {
		result.LastProbe = state.AWGLastProbe
	}
	result.Active = state.ActiveConnectionKind == "amneziawg" && state.ActiveAWGProfileID == id && state.Connected && state.OperationalMode == domain.OperationalModeVPN
	result.State = "imported"
	if state.ActiveConnectionKind == "amneziawg" && state.ActiveAWGProfileID == id && state.OperationalMode == domain.OperationalModeDirect {
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
	if iface, ifaceErr := s.awgController.Status(ctx); state.PreparedAWGProfileID == id && ifaceErr == nil {
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
	return s.CheckAWGProfile(ctx, "")
}

func (s *Service) CheckAWGProfile(ctx context.Context, id string) (AWGStatus, error) {
	return runStoreWriteLockedResult(s, func() (AWGStatus, error) {
		if err := s.migrateLegacyAWGProfileLocked(); err != nil {
			return AWGStatus{}, err
		}
		resolved, err := s.resolveAWGProfileIDLocked(id)
		if err != nil {
			return AWGStatus{}, err
		}
		_, _, err = s.checkAWGLocked(ctx, resolved, true)
		status, statusErr := s.awgStatusLocked(ctx, resolved)
		if err != nil {
			return status, err
		}
		return status, statusErr
	})
}

func (s *Service) checkAWGLocked(ctx context.Context, id string, prepare bool) (string, amneziawg.InterfaceStatus, error) {
	return s.verifyAWGLocked(ctx, id, prepare, true)
}

// A candidate probe owns a temporary tunnel. Connecting must instead verify
// the permanent tunnel that will remain alive after this function returns.
func (s *Service) verifyAWGLocked(ctx context.Context, id string, prepare, isolatedProbe bool) (string, amneziawg.InterfaceStatus, error) {
	profile, err := s.loadAWGProfile(id)
	if err != nil {
		return "", amneziawg.InterfaceStatus{}, err
	}
	if s.awgController == nil {
		return "", amneziawg.InterfaceStatus{}, fmt.Errorf("AmneziaWG controller is not configured")
	}
	stateBefore, err := s.store.LoadState()
	if err != nil {
		return "", amneziawg.InterfaceStatus{}, fmt.Errorf("load state: %w", err)
	}
	_, supportsIsolation := s.awgController.(amneziawg.IsolatedProbeController)
	if stateBefore.ActiveConnectionKind == "amneziawg" && stateBefore.Connected && stateBefore.ActiveAWGProfileID != "" && stateBefore.ActiveAWGProfileID != id && !(isolatedProbe && supportsIsolation) {
		return "", amneziawg.InterfaceStatus{}, fmt.Errorf("disconnect the active AmneziaWG profile before checking another profile")
	}
	activeProfile := stateBefore.ActiveConnectionKind == "amneziawg" && stateBefore.Connected && stateBefore.ActiveAWGProfileID == id
	isIsolatedProbe := false
	var isolatedCleanup func(context.Context) error
	var iface amneziawg.InterfaceStatus
	if prepare && !activeProfile && isolatedProbe {
		if isolated, ok := s.awgController.(amneziawg.IsolatedProbeController); ok {
			iface, isolatedCleanup, err = isolated.PrepareIsolatedProbe(ctx, profile)
			if isolatedCleanup != nil {
				defer func() {
					cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					defer cancel()
					if cleanupErr := isolatedCleanup(cleanupCtx); cleanupErr != nil {
						s.logWarn("remove isolated AmneziaWG probe", "error", cleanupErr.Error())
					}
				}()
			}
			if err != nil {
				return "", iface, err
			}
			isIsolatedProbe = true
		}
	}
	if prepare && !isIsolatedProbe {
		prepared, statusErr := s.awgController.Status(ctx)
		if activeProfile && (statusErr != nil || !prepared.Up || prepared.Device == "" || prepared.Address == "") {
			return "", prepared, fmt.Errorf("active AmneziaWG profile is unavailable; refusing to replace it during a check")
		}
		if stateBefore.PreparedAWGProfileID != id && !activeProfile {
			if err := s.awgController.Remove(ctx); err != nil {
				return "", amneziawg.InterfaceStatus{}, fmt.Errorf("remove prepared AmneziaWG profile: %w", err)
			}
			statusErr = errors.New("different profile was prepared")
		}
		if statusErr != nil || !prepared.Up || prepared.Device == "" || prepared.Address == "" {
			if err := s.awgController.Prepare(ctx, profile); err != nil {
				return "", amneziawg.InterfaceStatus{}, err
			}
			stateBefore.PreparedAWGProfileID = id
			if err := s.saveState(stateBefore); err != nil {
				return "", amneziawg.InterfaceStatus{}, err
			}
		}
	}
	if !isIsolatedProbe {
		iface, err = s.awgController.Status(ctx)
		if err != nil || !iface.Up || iface.Device == "" || iface.Address == "" {
			iface, err = s.awgController.Connect(ctx)
		}
		if err != nil {
			return "", iface, err
		}
		if routes, ok := s.awgController.(interface {
			EnsurePolicyRoutes(context.Context, amneziawg.InterfaceStatus) error
		}); ok {
			if err := routes.EnsurePolicyRoutes(ctx, iface); err != nil {
				return "", iface, err
			}
		}
	}
	managed, err := s.ensureAWGManagedRuntime(ctx)
	if err != nil {
		return "", iface, err
	}
	mark := amneziawg.RouteMark
	if isIsolatedProbe {
		// The isolated interface is selected by its output-device rule; using
		// the active AWG mark here would send this candidate through the live
		// tunnel instead.
		mark = 0
	}
	tag, err := managed.PrepareInterfaceOutbound(ctx, iface.Device, iface.Address, mark)
	latency := time.Duration(0)
	egressIP := ""
	countryCode := ""
	if err == nil {
		if s.managedOutboundProbe != nil {
			started := time.Now()
			err = s.managedOutboundProbe(ctx, managed, 0, tag)
			latency = time.Since(started)
			if err == nil && latency <= 0 {
				latency = time.Nanosecond
			}
		} else {
			observation, probeErr := s.probeManagedOutboundObservation(ctx, managed, 0, tag, true)
			latency, egressIP, countryCode, err = observation.latency, observation.egressIP, observation.countryCode, probeErr
		}
	}
	checkedAt := s.currentTime().UTC()
	probeState := &domain.AWGProbeState{Success: err == nil, CheckedAt: checkedAt, LatencyMS: float64(latency) / float64(time.Millisecond), EgressIP: egressIP, CountryCode: countryCode}
	if err != nil {
		probeState.Error = err.Error()
		probeState.LatencyMS = 0
	}
	state, stateErr := s.store.LoadState()
	if stateErr == nil {
		if state.AWGProfileProbes == nil {
			state.AWGProfileProbes = make(map[string]domain.AWGProbeState)
		}
		if probeState.CountryCode == "" {
			previousProbe := state.AWGProfileProbes[id]
			probeState.EgressIP = previousProbe.EgressIP
			probeState.CountryCode = previousProbe.CountryCode
		}
		state.AWGProfileProbes[id] = *probeState
		state.AWGLastProbe = probeState
		if state.Health == nil {
			state.Health = make(map[string]domain.NodeHealth)
		}
		failureThreshold := probe.DefaultSwitchPolicy().FailureThreshold
		if settings, loadSettingsErr := s.store.LoadSettings(); loadSettingsErr == nil {
			failureThreshold = switchPolicyFromSettings(settings).FailureThreshold
		}
		previousHealth := state.Health[id]
		previousHealth.NodeID = id
		updatedHealth := probe.UpdateHealth(previousHealth, err == nil, latency, checkedAt, probeState.Error, failureThreshold)
		if err == nil && countryCode != "" {
			updatedHealth.EgressIP = egressIP
			updatedHealth.CountryCode = countryCode
		}
		state.Health[id] = updatedHealth
		reportAutoHealthProgress(ctx, state.Health[id])
		if saveErr := s.saveState(state); saveErr != nil && err == nil {
			err = saveErr
		}
	}
	return tag, iface, err
}

func (s *Service) ConnectAWG(ctx context.Context) error {
	return s.ConnectAWGProfile(ctx, "")
}

func (s *Service) ConnectAWGProfile(ctx context.Context, id string) error {
	return runStoreWriteLocked(s, func() error {
		if err := s.migrateLegacyAWGProfileLocked(); err != nil {
			return err
		}
		resolved, err := s.resolveAWGProfileIDLocked(id)
		if err != nil {
			return err
		}
		return s.connectAWGWithModeLocked(ctx, resolved, domain.SelectionModeManual, "")
	})
}

func (s *Service) connectAWGLocked(ctx context.Context, id string) error {
	mode := domain.SelectionModeManual
	autoScope := ""
	if current, err := s.store.LoadState(); err == nil && current.Mode == domain.SelectionModeAuto {
		mode = domain.SelectionModeAuto
		autoScope = current.AutoScope
	}
	return s.connectAWGWithModeLocked(ctx, id, mode, autoScope)
}

func (s *Service) connectAWGWithModeLocked(ctx context.Context, id string, mode domain.SelectionMode, autoScope string) (resultErr error) {
	var tag string
	stateBefore, stateErr := s.store.LoadState()
	if stateErr != nil {
		return stateErr
	}
	if stateErr == nil && stateBefore.ActiveConnectionKind == "amneziawg" && stateBefore.Connected && stateBefore.ActiveAWGProfileID != "" && stateBefore.ActiveAWGProfileID != id {
		if _, isolated := s.awgController.(amneziawg.IsolatedProbeController); isolated {
			if _, _, err := s.checkAWGLocked(ctx, id, true); err != nil {
				return fmt.Errorf("verify AmneziaWG candidate: %w", err)
			}
		}
		original := stateBefore
		defer func() {
			if resultErr == nil {
				return
			}
			recoveryCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if restoreErr := s.restoreAWGConnectionLocked(recoveryCtx, original); restoreErr != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("restore previous AmneziaWG connection: %w", restoreErr))
			}
		}()
		if err := s.disconnectAWGLocked(ctx); err != nil {
			return fmt.Errorf("disconnect active AmneziaWG profile: %w", err)
		}
		stateBefore, stateErr = s.store.LoadState()
	}
	probe, hasProbe := stateBefore.AWGProfileProbes[id]
	if stateErr == nil && hasProbe && stateBefore.PreparedAWGProfileID == id && awgProbeIsFresh(&probe, s.currentTime().UTC(), time.Minute) && s.awgController != nil {
		if iface, statusErr := s.awgController.Status(ctx); statusErr == nil && iface.Up && iface.Device != "" && iface.Address != "" {
			if routes, ok := s.awgController.(interface {
				EnsurePolicyRoutes(context.Context, amneziawg.InterfaceStatus) error
			}); ok {
				if err := routes.EnsurePolicyRoutes(ctx, iface); err != nil {
					return err
				}
			}
			if managed, runtimeErr := s.ensureAWGManagedRuntime(ctx); runtimeErr == nil {
				tag, _ = managed.PrepareInterfaceOutbound(ctx, iface.Device, iface.Address, amneziawg.RouteMark)
			}
		}
	}
	if tag == "" {
		var err error
		tag, _, err = s.verifyAWGLocked(ctx, id, true, false)
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
	if reason, egressErr := s.ensureBackendEgress(ctx, settings, awgSubscriptionID, id, domain.SelectionModeManual); egressErr != nil {
		_ = managed.SelectOutbound(ctx, previousTag)
		state.CurrentOperation = nil
		_ = s.saveState(state)
		return fmt.Errorf("%s: %w", reason, egressErr)
	}
	state = intent
	state.ActiveConnectionKind = "amneziawg"
	state.ActiveAWGProfileID = id
	state.ActiveSubscriptionID = awgSubscriptionID
	state.ActiveNodeID = id
	if metadata, metadataErr := s.awgProfileMetadataLocked(id); metadataErr == nil {
		state.ActiveNodeName = firstNonEmpty(strings.TrimSpace(metadata.Name), "AmneziaWG")
		state.AWGProfileName = state.ActiveNodeName
	} else {
		state.ActiveNodeName = "AmneziaWG"
	}
	state.Mode = mode
	if mode == domain.SelectionModeAuto {
		state.AutoScope = autoScope
	} else {
		state.AutoScope = ""
	}
	state.Connected = true
	state.OperationalMode = domain.OperationalModeVPN
	state.ActiveTransport = domain.TransportModeProxy
	state.SelectedOutboundTag = tag
	state.RuntimeConfigVersion = tag
	state.CurrentOperation = nil
	state.RuntimeOutbounds = updateRuntimeOutbound(state.RuntimeOutbounds, domain.RuntimeOutboundState{Tag: tag, SubscriptionID: awgSubscriptionID, NodeID: id, Role: "active", VerifiedAt: now})
	if previousTag != "fastlane-direct" && previousTag != tag {
		state.RuntimeOutbounds = updateRuntimeOutbound(state.RuntimeOutbounds, domain.RuntimeOutboundState{Tag: previousTag, Role: "draining", RetireAfter: now.Add(5 * time.Minute), RemoveBy: now.Add(30 * time.Minute)})
	}
	state.LastSuccessAt = now
	state.LastSwitchAt = now
	if mode == domain.SelectionModeAuto {
		state.LastSwitchReason = "automatic switch to AmneziaWG"
	} else {
		state.LastSwitchReason = "manual switch to AmneziaWG"
	}
	state.LastFailureReason = ""
	state.LastTransportFailureReason = ""
	if err := s.saveState(state); err != nil {
		_ = managed.SelectOutbound(ctx, previousTag)
		previousState.CurrentOperation = nil
		_ = s.saveState(previousState)
		return fmt.Errorf("save AmneziaWG state: %w", err)
	}
	settings.AutoMode = mode == domain.SelectionModeAuto
	settings.Mode = mode
	_ = s.store.SaveSettings(settings)
	return nil
}

func (s *Service) restoreAWGConnectionLocked(ctx context.Context, original domain.RuntimeState) error {
	profile, err := s.loadAWGProfile(original.ActiveAWGProfileID)
	if err != nil {
		return err
	}
	if err := s.awgController.Remove(ctx); err != nil {
		return err
	}
	if err := s.awgController.Prepare(ctx, profile); err != nil {
		return err
	}
	iface, err := s.awgController.Connect(ctx)
	if err != nil {
		return err
	}
	managed, err := s.ensureAWGManagedRuntime(ctx)
	if err != nil {
		return err
	}
	tag, err := managed.PrepareInterfaceOutbound(ctx, iface.Device, iface.Address, amneziawg.RouteMark)
	if err != nil {
		return err
	}
	if err := managed.SelectOutbound(ctx, tag); err != nil {
		return err
	}
	settings, err := s.store.LoadSettings()
	if err != nil {
		return err
	}
	if reason, err := s.ensureBackendEgress(ctx, settings, awgSubscriptionID, original.ActiveAWGProfileID, original.Mode); err != nil {
		return fmt.Errorf("%s: %w", reason, err)
	}
	original.SelectedOutboundTag = tag
	original.PreparedAWGProfileID = original.ActiveAWGProfileID
	if err := s.saveState(original); err != nil {
		return err
	}
	settings.Mode = original.Mode
	settings.AutoMode = original.Mode == domain.SelectionModeAuto
	return s.store.SaveSettings(settings)
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
	if reason, err := s.ensureBackendRunning(ctx, awgSubscriptionID, legacyAWGNodeID, domain.SelectionModeManual); err != nil {
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
	return runStoreWriteLocked(s, func() error { return s.disconnectAWGLocked(ctx) })
}

func (s *Service) disconnectAWGLocked(ctx context.Context) error {
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
	state.ActiveAWGProfileID = ""
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
}

func (s *Service) RemoveAWG(ctx context.Context) error {
	return s.RemoveAWGProfile(ctx, "")
}

func (s *Service) RemoveAWGProfile(ctx context.Context, id string) error {
	return runStoreWriteLocked(s, func() error {
		if err := s.migrateLegacyAWGProfileLocked(); err != nil {
			return err
		}
		resolved, err := s.resolveAWGProfileIDLocked(id)
		if err != nil {
			return err
		}
		state, err := s.store.LoadState()
		if err != nil {
			return err
		}
		tag := ""
		active := state.ActiveConnectionKind == "amneziawg" && state.ActiveAWGProfileID == resolved
		if active {
			tag = state.SelectedOutboundTag
			if managed, ok := s.backend.(backend.ManagedBackend); ok {
				if err := managed.SelectDirect(ctx); err != nil {
					return err
				}
			}
		}
		if s.awgController != nil && (active || state.PreparedAWGProfileID == resolved) {
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
			if err := s.awgStore.RemoveAWGProfile(resolved); err != nil {
				return err
			}
		}
		delete(state.AWGProfileProbes, resolved)
		if state.PreparedAWGProfileID == resolved {
			state.PreparedAWGProfileID = ""
		}
		if active {
			state.AWGProfileName = ""
			state.AWGLastProbe = nil
			state.ActiveConnectionKind = ""
			state.ActiveAWGProfileID = ""
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
		settings, err := s.store.LoadSettings()
		if err != nil {
			return fmt.Errorf("load settings: %w", err)
		}
		exclusion := domain.AutoExcludedNodeKey("server-list", resolved)
		filtered := settings.AutoExcludedNodes[:0]
		for _, value := range settings.AutoExcludedNodes {
			if value != exclusion {
				filtered = append(filtered, value)
			}
		}
		settings.AutoExcludedNodes = domain.NormalizeAutoExcludedNodes(filtered)
		if active {
			settings.AutoMode = false
			settings.Mode = domain.SelectionModeDisconnected
		}
		return s.store.SaveSettings(settings)
	})
}

func (s *Service) loadAWGProfile(id string) (amneziawg.Profile, error) {
	if s.awgStore == nil {
		return amneziawg.Profile{}, fmt.Errorf("AmneziaWG profile storage is not configured")
	}
	raw, err := s.awgStore.LoadAWGProfile(id)
	if err != nil {
		return amneziawg.Profile{}, fmt.Errorf("load AmneziaWG profile: %w", err)
	}
	return amneziawg.Parse(raw)
}

func (s *Service) awgProfileMetadataLocked(id string) (amneziawg.ProfileMetadata, error) {
	profiles, err := s.awgStore.ListAWGProfiles()
	if err != nil {
		return amneziawg.ProfileMetadata{}, err
	}
	for _, profile := range profiles {
		if profile.ID == id {
			return profile, nil
		}
	}
	return amneziawg.ProfileMetadata{}, fmt.Errorf("AmneziaWG profile %q not found", id)
}

func (s *Service) resolveAWGProfileIDLocked(value string) (string, error) {
	profiles, err := s.awgStore.ListAWGProfiles()
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		state, stateErr := s.store.LoadState()
		if stateErr == nil && state.ActiveConnectionKind == "amneziawg" && state.ActiveAWGProfileID != "" {
			value = state.ActiveAWGProfileID
		} else if len(profiles) == 1 {
			return profiles[0].ID, nil
		} else if len(profiles) == 0 {
			return "", os.ErrNotExist
		} else {
			return "", fmt.Errorf("multiple AmneziaWG profiles are imported; specify --id")
		}
	}
	match := ""
	for _, profile := range profiles {
		if profile.ID == value {
			return profile.ID, nil
		}
		if strings.HasPrefix(profile.ID, value) {
			if match != "" {
				return "", fmt.Errorf("AmneziaWG profile ID prefix %q is ambiguous", value)
			}
			match = profile.ID
		}
	}
	if match != "" {
		return match, nil
	}
	return "", fmt.Errorf("AmneziaWG profile %q not found", value)
}

func (s *Service) migrateLegacyAWGProfileLocked() error {
	if s.awgStore == nil {
		return nil
	}
	raw, err := s.awgStore.LoadLegacyAWGProfile()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load legacy AmneziaWG profile: %w", err)
	}
	profile, err := amneziawg.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse legacy AmneziaWG profile: %w", err)
	}
	state, err := s.store.LoadState()
	if err != nil {
		return fmt.Errorf("load state for AmneziaWG migration: %w", err)
	}
	id := profile.StableID()
	name := firstNonEmpty(strings.TrimSpace(state.AWGProfileName), profile.Peer.Endpoint)
	if _, err := s.awgStore.SaveAWGProfile(amneziawg.ProfileMetadata{ID: id, Name: name}, raw); err != nil {
		return fmt.Errorf("migrate legacy AmneziaWG profile: %w", err)
	}
	if state.AWGProfileProbes == nil {
		state.AWGProfileProbes = make(map[string]domain.AWGProbeState)
	}
	if state.AWGLastProbe != nil {
		state.AWGProfileProbes[id] = *state.AWGLastProbe
	}
	if state.ActiveConnectionKind == "amneziawg" {
		state.ActiveAWGProfileID = id
		state.ActiveSubscriptionID = awgSubscriptionID
		if state.ActiveNodeID == "" || state.ActiveNodeID == legacyAWGNodeID {
			state.ActiveNodeID = id
		}
	}
	if state.PreparedAWGProfileID == "" {
		state.PreparedAWGProfileID = id
	}
	if err := s.saveState(state); err != nil {
		return fmt.Errorf("save migrated AmneziaWG state: %w", err)
	}
	if err := s.awgStore.RemoveLegacyAWGProfile(); err != nil {
		return fmt.Errorf("remove legacy AmneziaWG profile: %w", err)
	}
	return nil
}
