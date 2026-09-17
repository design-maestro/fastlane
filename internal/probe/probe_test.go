package probe_test

import (
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/probe"
)

func TestCalculateScore(t *testing.T) {
	t.Parallel()

	cfg := probe.DefaultScoreConfig()

	tests := []struct {
		name   string
		health domain.NodeHealth
		wantOK bool
	}{
		{
			name: "healthy node gets positive score",
			health: domain.NodeHealth{
				NodeID:               "a",
				Healthy:              true,
				LastLatency:          domain.NewDuration(120 * time.Millisecond),
				AverageLatency:       domain.NewDuration(100 * time.Millisecond),
				ConsecutiveSuccesses: 5,
				FailureCount:         1,
			},
			wantOK: true,
		},
		{
			name: "unhealthy node is penalized",
			health: domain.NodeHealth{
				NodeID:              "b",
				Healthy:             false,
				LastLatency:         domain.NewDuration(50 * time.Millisecond),
				ConsecutiveFailures: 4,
				FailureCount:        10,
			},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := probe.CalculateScore(tt.health, cfg)
			if tt.wantOK && (!result.Healthy || result.Score <= 0) {
				t.Fatalf("expected positive healthy score, got %+v", result)
			}

			if !tt.wantOK && (result.Healthy || result.Score >= 0) {
				t.Fatalf("expected unhealthy negative score, got %+v", result)
			}
		})
	}
}

func TestShouldSwitch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 19, 15, 0, 0, 0, time.UTC)
	policy := probe.DefaultSwitchPolicy()

	current := domain.NodeHealth{
		NodeID:               "current",
		Healthy:              true,
		AverageLatency:       domain.NewDuration(180 * time.Millisecond),
		ConsecutiveFailures:  0,
		ConsecutiveSuccesses: 3,
	}

	better := domain.NodeHealth{
		NodeID:               "better",
		Healthy:              true,
		AverageLatency:       domain.NewDuration(80 * time.Millisecond),
		ConsecutiveSuccesses: 4,
	}

	should, _ := probe.ShouldSwitch(current, better, now, now.Add(-2*time.Hour), policy)
	if !should {
		t.Fatal("expected switch to better node")
	}

	should, _ = probe.ShouldSwitch(current, better, now, now.Add(-1*time.Minute), policy)
	if should {
		t.Fatal("expected cooldown to prevent switch")
	}

	failing := current
	failing.Healthy = false
	failing.ConsecutiveFailures = policy.FailureThreshold

	should, reason := probe.ShouldSwitch(failing, better, now, now.Add(-1*time.Minute), policy)
	if !should {
		t.Fatal("expected unhealthy current node to trigger switch")
	}

	if reason == "" {
		t.Fatal("expected switch reason")
	}
}

func TestShouldSwitchDoesNotOptimizeInsideCooldownEvenAboveCeiling(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 10, 16, 0, 0, 0, time.UTC)
	policy := probe.DefaultSwitchPolicy()
	current := domain.NodeHealth{
		NodeID:         "current",
		Healthy:        true,
		LastLatency:    domain.NewDuration(321 * time.Millisecond),
		AverageLatency: domain.NewDuration(40 * time.Millisecond),
	}
	candidate := domain.NodeHealth{
		NodeID:               "candidate",
		Healthy:              true,
		LastLatency:          domain.NewDuration(26 * time.Millisecond),
		AverageLatency:       domain.NewDuration(120 * time.Millisecond),
		ConsecutiveSuccesses: 2,
	}

	should, reason := probe.ShouldSwitch(current, candidate, now, now.Add(-time.Minute), policy)
	if should || reason != "cooldown active" {
		t.Fatalf("expected cooldown to block latency optimization, should=%t reason=%q", should, reason)
	}
}

func TestShouldSwitchRequiresFourWinsAndThirtyFivePercentImprovement(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	policy := probe.DefaultSwitchPolicy()
	current := domain.NodeHealth{NodeID: "current", Healthy: true, AverageLatency: domain.NewDuration(300 * time.Millisecond)}
	candidate := domain.NodeHealth{NodeID: "candidate", Healthy: true, AverageLatency: domain.NewDuration(180 * time.Millisecond), ConsecutiveSuccesses: 3}
	if should, reason := probe.ShouldSwitch(current, candidate, now, now.Add(-time.Hour), policy); should || reason != "latency improvement is not confirmed" {
		t.Fatalf("single win was accepted: should=%t reason=%q", should, reason)
	}
	candidate.ConsecutiveSuccesses = 4
	candidate.AverageLatency = domain.NewDuration(210 * time.Millisecond) // 90 ms, but less than 35%.
	if should, reason := probe.ShouldSwitch(current, candidate, now, now.Add(-time.Hour), policy); should || reason != "relative improvement below threshold" {
		t.Fatalf("sub-35%% improvement was accepted: should=%t reason=%q", should, reason)
	}
	candidate.AverageLatency = domain.NewDuration(195 * time.Millisecond)
	if should, reason := probe.ShouldSwitch(current, candidate, now, now.Add(-time.Hour), policy); !should || reason == "" {
		t.Fatalf("confirmed 35%% / 105ms improvement was rejected: should=%t reason=%q", should, reason)
	}
}

func TestShouldSwitchRejectsFastCandidateWithInstabilityHistory(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	current := domain.NodeHealth{
		NodeID:               "steady",
		Healthy:              true,
		AverageLatency:       domain.NewDuration(150 * time.Millisecond),
		ConsecutiveSuccesses: 20,
		SuccessCount:         100,
	}
	candidate := domain.NodeHealth{
		NodeID:               "fast-but-unstable",
		Healthy:              true,
		LastLatency:          domain.NewDuration(50 * time.Millisecond),
		AverageLatency:       domain.NewDuration(55 * time.Millisecond),
		LatencyVariation:     domain.NewDuration(50 * time.Millisecond),
		SuccessCount:         9,
		FailureCount:         1,
		ConsecutiveSuccesses: 4,
		InstabilityPenalty:   3,
	}

	should, reason := probe.ShouldSwitch(current, candidate, now, now.Add(-time.Hour), probe.DefaultSwitchPolicy())
	if should || reason != "relative improvement below threshold" {
		t.Fatalf("expected unstable fast candidate to be rejected, should=%t reason=%q", should, reason)
	}
}

func TestShouldSwitchPrefersStableCandidateOverFasterFlakyCurrent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 15, 20, 0, 0, 0, time.UTC)
	current := domain.NodeHealth{
		NodeID:               "fast-flaky",
		Healthy:              true,
		LastLatency:          domain.NewDuration(45 * time.Millisecond),
		AverageLatency:       domain.NewDuration(50 * time.Millisecond),
		LatencyVariation:     domain.NewDuration(80 * time.Millisecond),
		SuccessCount:         10,
		FailureCount:         5,
		ConsecutiveSuccesses: 3,
		InstabilityPenalty:   6,
	}
	candidate := domain.NodeHealth{
		NodeID:               "steady",
		Healthy:              true,
		LastLatency:          domain.NewDuration(90 * time.Millisecond),
		AverageLatency:       domain.NewDuration(90 * time.Millisecond),
		LatencyVariation:     domain.NewDuration(5 * time.Millisecond),
		SuccessCount:         100,
		ConsecutiveSuccesses: 20,
	}

	should, reason := probe.ShouldSwitch(current, candidate, now, now.Add(-time.Hour), probe.DefaultSwitchPolicy())
	if !should || reason == "" {
		t.Fatalf("expected stable candidate to replace faster flaky current, should=%t reason=%q", should, reason)
	}
}

func TestShouldSwitchProfilePolicyKeepsGamesAndStreamingConnections(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	current := domain.NodeHealth{NodeID: "current", Healthy: true, AverageLatency: domain.NewDuration(250 * time.Millisecond)}
	candidate := domain.NodeHealth{NodeID: "candidate", Healthy: true, AverageLatency: domain.NewDuration(100 * time.Millisecond), ConsecutiveSuccesses: 10}
	games := probe.DefaultSwitchPolicy()
	games.AllowOptimization = false
	if should, reason := probe.ShouldSwitch(current, candidate, now, now.Add(-time.Hour), games); should || reason != "optimization disabled" {
		t.Fatalf("games profile optimized a healthy route: should=%t reason=%q", should, reason)
	}
	streaming := probe.DefaultSwitchPolicy()
	streaming.CurrentLatencyCeiling = 300 * time.Millisecond
	if should, reason := probe.ShouldSwitch(current, candidate, now, now.Add(-time.Hour), streaming); should || reason != "current node is within profile latency ceiling" {
		t.Fatalf("streaming profile optimized below its ceiling: should=%t reason=%q", should, reason)
	}
	current.Healthy = false
	if should, _ := probe.ShouldSwitch(current, candidate, now, now, games); !should {
		t.Fatal("games profile did not fail over after confirmed connection loss")
	}
}
