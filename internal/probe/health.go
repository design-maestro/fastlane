package probe

import (
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
)

const (
	instabilityPenaltyPerFailure = 3
	maxInstabilityPenalty        = 20
)

// UpdateHealth folds a probe result into the rolling health state.
func UpdateHealth(previous domain.NodeHealth, success bool, latency time.Duration, checkedAt time.Time, failureReason string, failureThreshold int) domain.NodeHealth {
	updated := previous
	updated.LastCheckedAt = checkedAt
	updated.LastLatency = domain.NewDuration(latency)
	if failureThreshold < 1 {
		failureThreshold = 1
	}

	if success {
		updated.Healthy = true
		updated.SuccessCount++
		updated.ConsecutiveSuccesses++
		updated.ConsecutiveFailures = 0
		if updated.InstabilityPenalty > 0 {
			updated.InstabilityPenalty--
		}
		updated.LastFailureReason = ""
		previousAverage := updated.AverageLatency.Duration()
		if previousAverage == 0 {
			updated.AverageLatency = domain.NewDuration(latency)
		} else {
			deviation := latency - previousAverage
			if deviation < 0 {
				deviation = -deviation
			}
			variation := updated.LatencyVariation.Duration()
			if variation == 0 {
				variation = deviation
			} else {
				variation = (variation*4 + deviation) / 5
			}
			updated.LatencyVariation = domain.NewDuration(variation)
			updated.AverageLatency = domain.NewDuration((previousAverage*4 + latency) / 5)
		}
		return updated
	}

	updated.Healthy = false
	updated.FailureCount++
	updated.ConsecutiveFailures++
	updated.ConsecutiveSuccesses = 0
	updated.InstabilityPenalty = min(updated.InstabilityPenalty+instabilityPenaltyPerFailure, maxInstabilityPenalty)
	updated.LastFailureReason = failureReason
	if updated.AverageLatency.Duration() == 0 {
		updated.AverageLatency = domain.NewDuration(latency)
	}
	if updated.SuccessCount > 0 && updated.ConsecutiveFailures < failureThreshold {
		updated.Healthy = true
		return updated
	}

	return updated
}
