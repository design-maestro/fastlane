package probe

import (
	"fmt"
	"sort"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
)

// ScoreConfig configures node ranking.
type ScoreConfig struct {
	HealthyBonus                  float64
	UnhealthyPenalty              float64
	FreshLatencyWeight            float64
	AverageLatencyWeight          float64
	LatencyVariationPenaltyWeight float64
	FailureRatePenalty            time.Duration
	ConsecutiveFailurePenalty     time.Duration
	InstabilityUnitPenalty        time.Duration
	MaxLatencyBaseline            time.Duration
}

// DefaultScoreConfig returns the default scoring configuration.
func DefaultScoreConfig() ScoreConfig {
	return ScoreConfig{
		HealthyBonus:                  10_000,
		UnhealthyPenalty:              10_000,
		FreshLatencyWeight:            0.7,
		AverageLatencyWeight:          0.3,
		LatencyVariationPenaltyWeight: 0.5,
		FailureRatePenalty:            500 * time.Millisecond,
		ConsecutiveFailurePenalty:     500 * time.Millisecond,
		InstabilityUnitPenalty:        100 * time.Millisecond,
		MaxLatencyBaseline:            2 * time.Second,
	}
}

// CalculateScore converts health telemetry into a comparable score.
func CalculateScore(health domain.NodeHealth, cfg ScoreConfig) domain.ScoreResult {
	score := 0.0
	reason := "healthy"
	latency := effectiveSelectionLatency(health, cfg)
	if latency <= 0 {
		latency = cfg.MaxLatencyBaseline
	}

	if health.Healthy {
		score += cfg.HealthyBonus
		score -= latency.Seconds() * 1000
	} else {
		reason = "unhealthy"
		score -= cfg.UnhealthyPenalty
		score -= latency.Seconds() * 1000
	}

	return domain.ScoreResult{
		NodeID:  health.NodeID,
		Healthy: health.Healthy,
		Score:   score,
		Reason:  reason,
	}
}

func selectionLatency(health domain.NodeHealth) time.Duration {
	if latency := health.LastLatency.Duration(); latency > 0 {
		return latency
	}
	return health.AverageLatency.Duration()
}

func effectiveSelectionLatency(health domain.NodeHealth, cfg ScoreConfig) time.Duration {
	fresh := health.LastLatency.Duration()
	average := health.AverageLatency.Duration()
	latency := weightedLatency(fresh, average, cfg.FreshLatencyWeight, cfg.AverageLatencyWeight)
	if variation := health.LatencyVariation.Duration(); variation > 0 && cfg.LatencyVariationPenaltyWeight > 0 {
		latency += time.Duration(float64(variation) * cfg.LatencyVariationPenaltyWeight)
	}

	attempts := health.SuccessCount + health.FailureCount
	if attempts > 0 && cfg.FailureRatePenalty > 0 {
		failureRate := float64(health.FailureCount) / float64(attempts)
		recoveryFactor := 1 + float64(min(health.ConsecutiveSuccesses, 20))/5
		latency += time.Duration(failureRate * float64(cfg.FailureRatePenalty) / recoveryFactor)
	}
	if health.ConsecutiveFailures > 0 && cfg.ConsecutiveFailurePenalty > 0 {
		latency += time.Duration(health.ConsecutiveFailures) * cfg.ConsecutiveFailurePenalty
	}
	if health.InstabilityPenalty > 0 && cfg.InstabilityUnitPenalty > 0 {
		latency += time.Duration(health.InstabilityPenalty) * cfg.InstabilityUnitPenalty
	}
	return latency
}

func weightedLatency(fresh, average time.Duration, freshWeight, averageWeight float64) time.Duration {
	if fresh <= 0 {
		return average
	}
	if average <= 0 {
		return fresh
	}
	totalWeight := freshWeight + averageWeight
	if totalWeight <= 0 {
		return fresh
	}
	return time.Duration((float64(fresh)*freshWeight + float64(average)*averageWeight) / totalWeight)
}

// SelectBestNode chooses the healthiest measured node with the lowest effective
// latency after current, rolling-average, variation, and reliability penalties.
func SelectBestNode(nodes []domain.Node, health map[string]domain.NodeHealth, cfg ScoreConfig) (domain.Node, domain.ScoreResult, error) {
	if len(nodes) == 0 {
		return domain.Node{}, domain.ScoreResult{}, fmt.Errorf("no nodes to select from")
	}

	type candidate struct {
		node   domain.Node
		result domain.ScoreResult
	}

	candidates := make([]candidate, 0, len(nodes))
	for _, node := range nodes {
		h := health[node.ID]
		h.NodeID = node.ID
		result := CalculateScore(h, cfg)
		candidates = append(candidates, candidate{node: node, result: result})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].result.Healthy != candidates[j].result.Healthy {
			return candidates[i].result.Healthy
		}
		leftLatency := effectiveSelectionLatency(health[candidates[i].node.ID], cfg)
		rightLatency := effectiveSelectionLatency(health[candidates[j].node.ID], cfg)
		if leftLatency > 0 && rightLatency <= 0 {
			return true
		}
		if leftLatency <= 0 && rightLatency > 0 {
			return false
		}
		if leftLatency > 0 && rightLatency > 0 && leftLatency != rightLatency {
			return leftLatency < rightLatency
		}
		return candidates[i].result.Score > candidates[j].result.Score
	})

	best := candidates[0]
	best.result.Selected = true
	return best.node, best.result, nil
}
