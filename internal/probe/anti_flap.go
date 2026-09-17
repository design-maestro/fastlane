package probe

import (
	"fmt"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
)

const defaultHealthyLatencyCeiling = 300 * time.Millisecond

// SwitchPolicy controls anti-flap behavior.
type SwitchPolicy struct {
	AllowOptimization     bool
	CurrentLatencyCeiling time.Duration
	Cooldown              time.Duration
	LatencyImprovement    time.Duration
	RelativeImprovement   float64
	RequiredLatencyWins   int
	FailureThreshold      int
	HealthyLatencyCeiling time.Duration
}

// DefaultSwitchPolicy returns conservative switching defaults.
func DefaultSwitchPolicy() SwitchPolicy {
	return SwitchPolicy{
		AllowOptimization:     true,
		Cooldown:              20 * time.Minute,
		LatencyImprovement:    70 * time.Millisecond,
		RelativeImprovement:   0.35,
		RequiredLatencyWins:   4,
		FailureThreshold:      2,
		HealthyLatencyCeiling: defaultHealthyLatencyCeiling,
	}
}

// ShouldSwitch decides whether the selector should move to the candidate node.
func ShouldSwitch(current, candidate domain.NodeHealth, now, lastSwitch time.Time, policy SwitchPolicy) (bool, string) {
	return ShouldSwitchWithWins(current, candidate, now, lastSwitch, policy, candidate.ConsecutiveSuccesses)
}

// ShouldSwitchWithWins uses consecutive measurements where this exact candidate
// beat the current route. It keeps probe success history separate from proof of
// a switching advantage.
func ShouldSwitchWithWins(current, candidate domain.NodeHealth, now, lastSwitch time.Time, policy SwitchPolicy, confirmedWins int) (bool, string) {
	if candidate.NodeID == "" || !candidate.Healthy {
		return false, ""
	}

	if current.NodeID == "" {
		return true, "no current node"
	}

	if !current.Healthy || current.ConsecutiveFailures >= policy.FailureThreshold {
		return true, "current node unhealthy"
	}
	if !policy.AllowOptimization {
		return false, "optimization disabled"
	}
	if policy.CurrentLatencyCeiling > 0 && current.AverageLatency.Duration() <= policy.CurrentLatencyCeiling {
		return false, "current node is within profile latency ceiling"
	}

	// Compare the same latency-equivalent quality cost used by candidate
	// ranking. This keeps latency relevant while allowing recent failures,
	// jitter, and rolling history to outweigh a deceptively fast probe.
	scoreConfig := DefaultScoreConfig()
	currentLatency := effectiveSelectionLatency(current, scoreConfig)
	candidateLatency := effectiveSelectionLatency(candidate, scoreConfig)
	if now.Sub(lastSwitch) < policy.Cooldown {
		return false, "cooldown active"
	}

	if policy.RequiredLatencyWins > 0 && confirmedWins < policy.RequiredLatencyWins {
		return false, "latency improvement is not confirmed"
	}

	if currentLatency > 0 && candidateLatency > 0 && policy.RelativeImprovement > 0 {
		relative := float64(currentLatency-candidateLatency) / float64(currentLatency)
		if relative < policy.RelativeImprovement {
			return false, "relative improvement below threshold"
		}
	}

	if currentLatency > 0 &&
		currentLatency <= policy.HealthyLatencyCeiling &&
		currentLatency-candidateLatency < policy.LatencyImprovement {
		return false, "improvement below threshold"
	}

	if candidateLatency > 0 && currentLatency-candidateLatency >= policy.LatencyImprovement {
		return true, fmt.Sprintf("stability-adjusted quality improved by %s", currentLatency-candidateLatency)
	}

	return false, "current node acceptable"
}

// IsOptimizationCandidate reports whether a healthy candidate is materially
// better before cooldown and confirmation checks are applied.
func IsOptimizationCandidate(current, candidate domain.NodeHealth, policy SwitchPolicy) bool {
	if !policy.AllowOptimization || !current.Healthy || !candidate.Healthy || current.NodeID == candidate.NodeID {
		return false
	}
	if policy.CurrentLatencyCeiling > 0 && current.AverageLatency.Duration() <= policy.CurrentLatencyCeiling {
		return false
	}
	config := DefaultScoreConfig()
	currentLatency, candidateLatency := effectiveSelectionLatency(current, config), effectiveSelectionLatency(candidate, config)
	if currentLatency <= 0 || candidateLatency <= 0 || currentLatency-candidateLatency < policy.LatencyImprovement {
		return false
	}
	if policy.RelativeImprovement > 0 && float64(currentLatency-candidateLatency)/float64(currentLatency) < policy.RelativeImprovement {
		return false
	}
	return true
}
