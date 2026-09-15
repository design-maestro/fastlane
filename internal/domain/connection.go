package domain

import "time"

// OperationalMode describes how traffic is currently leaving the router.
// It is intentionally separate from SelectionMode, which only controls how a
// VPN node is selected.
type OperationalMode string

const (
	// OperationalModeVPN means traffic uses the selected VPN outbound.
	OperationalModeVPN OperationalMode = "vpn"
	// OperationalModeDirect means the VPN is unavailable and traffic is direct.
	OperationalModeDirect OperationalMode = "direct"
	// OperationalModeRecovering means Fast Lane is moving between runtime configs.
	OperationalModeRecovering OperationalMode = "recovering"
)

// RuntimeOperation describes the bounded in-flight runtime transition.
type RuntimeOperation struct {
	Kind      string    `json:"kind"`
	From      string    `json:"from,omitempty"`
	To        string    `json:"to,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

// RuntimeOutboundState tracks live handlers retained across route switches.
type RuntimeOutboundState struct {
	Tag             string    `json:"tag"`
	SubscriptionID  string    `json:"subscription_id,omitempty"`
	NodeID          string    `json:"node_id,omitempty"`
	Role            string    `json:"role"`
	VerifiedAt      time.Time `json:"verified_at,omitempty"`
	Score           float64   `json:"score,omitempty"`
	Samples         int       `json:"samples,omitempty"`
	SelectionReason string    `json:"selection_reason,omitempty"`
	PromotionWins   int       `json:"promotion_wins,omitempty"`
	RetireAfter     time.Time `json:"retire_after,omitempty"`
	RemoveBy        time.Time `json:"remove_by,omitempty"`
}

type CandidateBackoffState struct {
	Tag                 string    `json:"tag"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	RetryAfter          time.Time `json:"retry_after"`
}

// AWGProbeState contains connectivity metadata only; imported keys never enter
// runtime state, status or diagnostics.
type AWGProbeState struct {
	Success   bool      `json:"success"`
	CheckedAt time.Time `json:"checked_at"`
	LatencyMS float64   `json:"latency_ms,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// ActiveConnection describes the currently applied runtime selection.
type ActiveConnection struct {
	SubscriptionID string        `json:"subscription_id"`
	NodeID         string        `json:"node_id"`
	ConnectedAt    time.Time     `json:"connected_at"`
	Mode           SelectionMode `json:"mode"`
}

// ZapretTestRestoreState keeps the runtime selection that should be restored
// when a manual Zapret test ends.
type ZapretTestRestoreState struct {
	ActiveSubscriptionID string        `json:"active_subscription_id,omitempty"`
	ActiveNodeID         string        `json:"active_node_id,omitempty"`
	Mode                 SelectionMode `json:"mode,omitempty"`
	Connected            bool          `json:"connected"`
	ActiveTransport      TransportMode `json:"active_transport,omitempty"`
}

// ZapretTestState persists whether Fast Lane is in a user-forced Zapret test
// mode and which runtime selection should be restored afterwards.
type ZapretTestState struct {
	Active  bool                   `json:"active"`
	Restore ZapretTestRestoreState `json:"restore,omitempty"`
}

// RuntimeState persists the operational state across restarts.
type RuntimeState struct {
	SchemaVersion              int                              `json:"schema_version"`
	OperationalMode            OperationalMode                  `json:"operational_mode"`
	SelectedOutboundTag        string                           `json:"selected_outbound_tag,omitempty"`
	RuntimeConfigGeneration    uint64                           `json:"runtime_config_generation"`
	RuntimeConfigVersion       string                           `json:"runtime_config_version,omitempty"`
	CurrentOperation           *RuntimeOperation                `json:"current_operation,omitempty"`
	RuntimeOutbounds           []RuntimeOutboundState           `json:"runtime_outbounds,omitempty"`
	CandidateBackoff           map[string]CandidateBackoffState `json:"candidate_backoff,omitempty"`
	AutoScope                  string                           `json:"auto_scope,omitempty"`
	ActiveSubscriptionID       string                           `json:"active_subscription_id"`
	ActiveNodeID               string                           `json:"active_node_id"`
	ActiveNodeName             string                           `json:"active_node_name,omitempty"`
	Mode                       SelectionMode                    `json:"mode"`
	Connected                  bool                             `json:"connected"`
	ActiveTransport            TransportMode                    `json:"active_transport"`
	LastRefreshAt              map[string]time.Time             `json:"last_refresh_at"`
	Health                     map[string]NodeHealth            `json:"health"`
	LastSwitchAt               time.Time                        `json:"last_switch_at"`
	LastSwitchReason           string                           `json:"last_switch_reason,omitempty"`
	LastTransportSwitchAt      time.Time                        `json:"last_transport_switch_at"`
	LastSuccessAt              time.Time                        `json:"last_success_at"`
	LastFailureReason          string                           `json:"last_failure_reason"`
	LastTransportFailureReason string                           `json:"last_transport_failure_reason"`
	ZapretTest                 ZapretTestState                  `json:"zapret_test,omitempty"`
	ActiveConnectionKind       string                           `json:"active_connection_kind,omitempty"`
	AWGProfileName             string                           `json:"awg_profile_name,omitempty"`
	AWGLastProbe               *AWGProbeState                   `json:"awg_last_probe,omitempty"`
}

// DefaultRuntimeState returns an empty persisted state.
func DefaultRuntimeState() RuntimeState {
	return RuntimeState{
		SchemaVersion:    4,
		OperationalMode:  OperationalModeDirect,
		Mode:             SelectionModeDisconnected,
		ActiveTransport:  TransportModeDirect,
		LastRefreshAt:    make(map[string]time.Time),
		Health:           make(map[string]NodeHealth),
		CandidateBackoff: make(map[string]CandidateBackoffState),
	}
}

// NormalizeOperationalMode coerces unknown values to the fail-open direct mode.
func NormalizeOperationalMode(mode OperationalMode) OperationalMode {
	switch mode {
	case OperationalModeVPN, OperationalModeRecovering:
		return mode
	default:
		return OperationalModeDirect
	}
}

// LegacyOperationalMode derives the additive operational status from the
// pre-schema-4 connection fields.
func LegacyOperationalMode(connected bool, transport TransportMode) OperationalMode {
	if connected && NormalizeTransportMode(transport) == TransportModeProxy {
		return OperationalModeVPN
	}

	return OperationalModeDirect
}
