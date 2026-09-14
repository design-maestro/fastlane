package probe

import (
	"fmt"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
)

const defaultHealthyLatencyCeiling = 300 * time.Millisecond

// SwitchPolicy controls anti-flap behavior.
type SwitchPolicy struct {
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
		Cooldown:              5 * time.Minute,
		LatencyImprovement:    50 * time.Millisecond,
		RelativeImprovement:   0.20,
		RequiredLatencyWins:   2,
		FailureThreshold:      2,
		HealthyLatencyCeiling: defaultHealthyLatencyCeiling,
	}
}

// ShouldSwitch decides whether the selector should move to the candidate node.
func ShouldSwitch(current, candidate domain.NodeHealth, now, lastSwitch time.Time, policy SwitchPolicy) (bool, string) {
	if candidate.NodeID == "" || !candidate.Healthy {
		return false, ""
	}

	if current.NodeID == "" {
		return true, "no current node"
	}

	if !current.Healthy || current.ConsecutiveFailures >= policy.FailureThreshold {
		return true, "current node unhealthy"
	}

	currentLatency := selectionLatency(current)
	candidateLatency := selectionLatency(candidate)
	if now.Sub(lastSwitch) < policy.Cooldown {
		return false, "cooldown active"
	}

	if policy.RequiredLatencyWins > 0 && candidate.ConsecutiveSuccesses < policy.RequiredLatencyWins {
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
		return true, fmt.Sprintf("latency improved by %s", currentLatency-candidateLatency)
	}

	return false, "current node acceptable"
}
