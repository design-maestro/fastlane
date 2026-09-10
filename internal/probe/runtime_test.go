package probe_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/probe"
)

func TestTCPCheckerSuccess(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	accepted := make(chan struct{}, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
			accepted <- struct{}{}
		}
	}()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener addr type: %T", listener.Addr())
	}

	checker := probe.TCPChecker{Timeout: time.Second}
	result := checker.Check(context.Background(), domain.Node{
		ID:      "node-1",
		Address: "127.0.0.1",
		Port:    addr.Port,
	})

	if !result.Healthy {
		t.Fatalf("expected healthy result, got %+v", result)
	}
	if result.Latency <= 0 {
		t.Fatalf("expected positive latency, got %s", result.Latency)
	}

	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("expected checker connection to be accepted")
	}
}

func TestTCPCheckerFailureUsesTimeoutLatency(t *testing.T) {
	t.Parallel()

	timeout := 25 * time.Millisecond
	checker := probe.TCPChecker{Timeout: timeout}
	result := checker.Check(context.Background(), domain.Node{
		ID:      "node-1",
		Address: "127.0.0.1",
		Port:    0,
	})

	if result.Healthy {
		t.Fatalf("expected unhealthy result, got %+v", result)
	}
	if result.Err == nil {
		t.Fatal("expected probe error")
	}
	if result.Latency != timeout {
		t.Fatalf("expected timeout latency %s, got %s", timeout, result.Latency)
	}
}

func TestUpdateHealthSuccessClearsFailuresAndTracksAverage(t *testing.T) {
	t.Parallel()

	previous := domain.NodeHealth{
		Healthy:              false,
		FailureCount:         2,
		ConsecutiveFailures:  2,
		ConsecutiveSuccesses: 0,
		LastFailureReason:    "dial tcp timeout",
		AverageLatency:       domain.NewDuration(200 * time.Millisecond),
		InstabilityPenalty:   2,
	}

	updated := probe.UpdateHealth(previous, true, 100*time.Millisecond, time.Date(2026, 3, 25, 8, 0, 0, 0, time.UTC), "", probe.DefaultSwitchPolicy().FailureThreshold)
	if !updated.Healthy {
		t.Fatal("expected node to become healthy")
	}
	if updated.ConsecutiveFailures != 0 {
		t.Fatalf("expected failures to reset, got %d", updated.ConsecutiveFailures)
	}
	if updated.ConsecutiveSuccesses != 1 {
		t.Fatalf("expected success counter to increment, got %d", updated.ConsecutiveSuccesses)
	}
	if updated.LastFailureReason != "" {
		t.Fatalf("expected failure reason to clear, got %q", updated.LastFailureReason)
	}
	if updated.AverageLatency.Duration() != 180*time.Millisecond {
		t.Fatalf("unexpected average latency: %s", updated.AverageLatency.Duration())
	}
	if updated.LatencyVariation.Duration() != 100*time.Millisecond {
		t.Fatalf("unexpected latency variation: %s", updated.LatencyVariation.Duration())
	}
	if updated.InstabilityPenalty != 1 {
		t.Fatalf("expected instability penalty to decay, got %d", updated.InstabilityPenalty)
	}
}

func TestUpdateHealthFailurePreservesHealthyNodeUntilThreshold(t *testing.T) {
	t.Parallel()

	previous := domain.NodeHealth{
		Healthy:              true,
		SuccessCount:         3,
		ConsecutiveSuccesses: 3,
		AverageLatency:       domain.NewDuration(90 * time.Millisecond),
	}

	updated := probe.UpdateHealth(previous, false, 250*time.Millisecond, time.Date(2026, 3, 25, 8, 5, 0, 0, time.UTC), "connection refused", probe.DefaultSwitchPolicy().FailureThreshold)
	if !updated.Healthy {
		t.Fatal("expected node to stay healthy before reaching threshold")
	}
	if updated.ConsecutiveSuccesses != 0 {
		t.Fatalf("expected successes to reset, got %d", updated.ConsecutiveSuccesses)
	}
	if updated.ConsecutiveFailures != 1 {
		t.Fatalf("expected one consecutive failure, got %d", updated.ConsecutiveFailures)
	}
	if updated.InstabilityPenalty != 3 {
		t.Fatalf("expected recent failure penalty, got %d", updated.InstabilityPenalty)
	}
	if updated.LastFailureReason != "connection refused" {
		t.Fatalf("unexpected failure reason: %q", updated.LastFailureReason)
	}
	if updated.AverageLatency.Duration() != 90*time.Millisecond {
		t.Fatalf("expected average latency to be preserved, got %s", updated.AverageLatency.Duration())
	}
}

func TestUpdateHealthFailureMarksNodeUnhealthyAtThreshold(t *testing.T) {
	t.Parallel()

	threshold := probe.DefaultSwitchPolicy().FailureThreshold
	previous := domain.NodeHealth{
		Healthy:              true,
		SuccessCount:         2,
		FailureCount:         2,
		ConsecutiveFailures:  threshold - 1,
		ConsecutiveSuccesses: 0,
		AverageLatency:       domain.NewDuration(90 * time.Millisecond),
	}

	updated := probe.UpdateHealth(previous, false, 250*time.Millisecond, time.Date(2026, 3, 25, 8, 10, 0, 0, time.UTC), "connection refused", threshold)
	if updated.Healthy {
		t.Fatal("expected node to become unhealthy after threshold breach")
	}
	if updated.ConsecutiveFailures != threshold {
		t.Fatalf("unexpected consecutive failures: got %d want %d", updated.ConsecutiveFailures, threshold)
	}
}

func TestUpdateHealthFailureKeepsNewNodeUnhealthyWithoutSuccessHistory(t *testing.T) {
	t.Parallel()

	updated := probe.UpdateHealth(domain.NodeHealth{}, false, 250*time.Millisecond, time.Date(2026, 3, 25, 8, 15, 0, 0, time.UTC), "connection refused", probe.DefaultSwitchPolicy().FailureThreshold)
	if updated.Healthy {
		t.Fatal("expected node without success history to remain unhealthy")
	}
}

func TestSelectBestNodeKeepsStableOrderOnTie(t *testing.T) {
	t.Parallel()

	nodes := []domain.Node{
		{ID: "node-1", Name: "One"},
		{ID: "node-2", Name: "Two"},
	}

	best, result, err := probe.SelectBestNode(nodes, map[string]domain.NodeHealth{}, probe.DefaultScoreConfig())
	if err != nil {
		t.Fatalf("select best node: %v", err)
	}
	if best.ID != "node-1" {
		t.Fatalf("expected stable tie-break to keep first node, got %s", best.ID)
	}
	if !result.Selected {
		t.Fatal("expected best result to be marked selected")
	}
}

func TestSelectBestNodeFailsOnEmptyInput(t *testing.T) {
	t.Parallel()

	if _, _, err := probe.SelectBestNode(nil, nil, probe.DefaultScoreConfig()); err == nil {
		t.Fatal("expected empty node list to fail")
	}
}

func TestSelectBestNodePrefersFreshLatencyOverHistory(t *testing.T) {
	t.Parallel()

	nodes := []domain.Node{
		{ID: "historically-stable", Name: "Historically stable"},
		{ID: "fresh-fast", Name: "Fresh fast"},
	}
	health := map[string]domain.NodeHealth{
		"historically-stable": {
			NodeID:               "historically-stable",
			Healthy:              true,
			LastLatency:          domain.NewDuration(321 * time.Millisecond),
			AverageLatency:       domain.NewDuration(40 * time.Millisecond),
			SuccessCount:         9_000,
			ConsecutiveSuccesses: 9_000,
		},
		"fresh-fast": {
			NodeID:               "fresh-fast",
			Healthy:              true,
			LastLatency:          domain.NewDuration(26 * time.Millisecond),
			AverageLatency:       domain.NewDuration(120 * time.Millisecond),
			SuccessCount:         1,
			ConsecutiveSuccesses: 1,
		},
	}

	best, _, err := probe.SelectBestNode(nodes, health, probe.DefaultScoreConfig())
	if err != nil {
		t.Fatalf("select best node: %v", err)
	}
	if best.ID != "fresh-fast" {
		t.Fatalf("expected fresh 26ms node, got %s", best.ID)
	}
}

func TestSelectBestNodeDoesNotKeepDegradedLatencyBecauseOfReliabilityPenalties(t *testing.T) {
	t.Parallel()

	nodes := []domain.Node{
		{ID: "stable-but-degraded", Name: "Stable but degraded"},
		{ID: "recently-unstable-fast", Name: "Recently unstable fast"},
	}
	health := map[string]domain.NodeHealth{
		"stable-but-degraded": {
			NodeID:               "stable-but-degraded",
			Healthy:              true,
			LastLatency:          domain.NewDuration(347 * time.Millisecond),
			AverageLatency:       domain.NewDuration(40 * time.Millisecond),
			SuccessCount:         1_000,
			ConsecutiveSuccesses: 1_000,
		},
		"recently-unstable-fast": {
			NodeID:               "recently-unstable-fast",
			Healthy:              true,
			LastLatency:          domain.NewDuration(39 * time.Millisecond),
			AverageLatency:       domain.NewDuration(60 * time.Millisecond),
			SuccessCount:         10,
			FailureCount:         10,
			ConsecutiveSuccesses: 1,
			InstabilityPenalty:   20,
		},
	}

	best, _, err := probe.SelectBestNode(nodes, health, probe.DefaultScoreConfig())
	if err != nil {
		t.Fatalf("select best node: %v", err)
	}
	if best.ID != "recently-unstable-fast" {
		t.Fatalf("expected a healthy node below the latency ceiling, got %s", best.ID)
	}
}

func TestSelectBestNodePenalizesUnstableFastNode(t *testing.T) {
	t.Parallel()

	nodes := []domain.Node{
		{ID: "stable", Name: "Stable"},
		{ID: "flaky", Name: "Flaky"},
	}
	health := map[string]domain.NodeHealth{
		"stable": {
			NodeID:               "stable",
			Healthy:              true,
			LastLatency:          domain.NewDuration(75 * time.Millisecond),
			AverageLatency:       domain.NewDuration(70 * time.Millisecond),
			SuccessCount:         95,
			FailureCount:         5,
			ConsecutiveSuccesses: 12,
		},
		"flaky": {
			NodeID:               "flaky",
			Healthy:              true,
			LastLatency:          domain.NewDuration(45 * time.Millisecond),
			AverageLatency:       domain.NewDuration(50 * time.Millisecond),
			SuccessCount:         5,
			FailureCount:         5,
			ConsecutiveSuccesses: 1,
		},
	}

	best, _, err := probe.SelectBestNode(nodes, health, probe.DefaultScoreConfig())
	if err != nil {
		t.Fatalf("select best node: %v", err)
	}
	if best.ID != "stable" {
		t.Fatalf("expected stable node to outrank flaky low-latency node, got %s", best.ID)
	}
}

func TestSelectBestNodePenalizesLatencyVariation(t *testing.T) {
	t.Parallel()

	nodes := []domain.Node{{ID: "steady"}, {ID: "jumpy"}}
	health := map[string]domain.NodeHealth{
		"steady": {
			NodeID:           "steady",
			Healthy:          true,
			LastLatency:      domain.NewDuration(70 * time.Millisecond),
			AverageLatency:   domain.NewDuration(70 * time.Millisecond),
			LatencyVariation: domain.NewDuration(5 * time.Millisecond),
			SuccessCount:     20,
		},
		"jumpy": {
			NodeID:           "jumpy",
			Healthy:          true,
			LastLatency:      domain.NewDuration(60 * time.Millisecond),
			AverageLatency:   domain.NewDuration(60 * time.Millisecond),
			LatencyVariation: domain.NewDuration(100 * time.Millisecond),
			SuccessCount:     20,
		},
	}

	best, _, err := probe.SelectBestNode(nodes, health, probe.DefaultScoreConfig())
	if err != nil {
		t.Fatalf("select best node: %v", err)
	}
	if best.ID != "steady" {
		t.Fatalf("expected steady node to outrank jumpy node, got %s", best.ID)
	}
}

func TestSelectBestNodeKeepsRecentlyFailedNodeDemotedAfterOneRecovery(t *testing.T) {
	t.Parallel()

	failed := domain.NodeHealth{
		NodeID:         "recently-failed",
		Healthy:        true,
		LastLatency:    domain.NewDuration(20 * time.Millisecond),
		AverageLatency: domain.NewDuration(20 * time.Millisecond),
		SuccessCount:   100,
	}
	failed = probe.UpdateHealth(failed, false, 3*time.Second, time.Now().UTC(), "timeout", 1)
	failed = probe.UpdateHealth(failed, true, 20*time.Millisecond, time.Now().UTC(), "", 1)
	steady := domain.NodeHealth{
		NodeID:               "steady",
		Healthy:              true,
		LastLatency:          domain.NewDuration(70 * time.Millisecond),
		AverageLatency:       domain.NewDuration(70 * time.Millisecond),
		SuccessCount:         100,
		ConsecutiveSuccesses: 10,
	}

	best, _, err := probe.SelectBestNode(
		[]domain.Node{{ID: failed.NodeID}, {ID: steady.NodeID}},
		map[string]domain.NodeHealth{failed.NodeID: failed, steady.NodeID: steady},
		probe.DefaultScoreConfig(),
	)
	if err != nil {
		t.Fatalf("select best node: %v", err)
	}
	if best.ID != steady.NodeID {
		t.Fatalf("expected recent failure to prevent an immediate bounce, got %s", best.ID)
	}
}

func TestSelectBestNodeAlwaysPrefersHealthyCandidate(t *testing.T) {
	t.Parallel()

	nodes := []domain.Node{{ID: "failed-fast"}, {ID: "healthy-slow"}}
	health := map[string]domain.NodeHealth{
		"failed-fast": {
			NodeID:              "failed-fast",
			Healthy:             false,
			LastLatency:         domain.NewDuration(20 * time.Millisecond),
			ConsecutiveFailures: 1,
		},
		"healthy-slow": {
			NodeID:       "healthy-slow",
			Healthy:      true,
			LastLatency:  domain.NewDuration(15 * time.Second),
			SuccessCount: 1,
		},
	}

	best, _, err := probe.SelectBestNode(nodes, health, probe.DefaultScoreConfig())
	if err != nil {
		t.Fatalf("select best node: %v", err)
	}
	if best.ID != "healthy-slow" {
		t.Fatalf("expected healthy candidate to outrank failed node, got %s", best.ID)
	}
}

func TestShouldSwitchRejectsUnhealthyCandidate(t *testing.T) {
	t.Parallel()

	current := domain.NodeHealth{
		NodeID:         "current",
		Healthy:        true,
		AverageLatency: domain.NewDuration(90 * time.Millisecond),
	}
	candidate := domain.NodeHealth{
		NodeID:         "candidate",
		Healthy:        false,
		AverageLatency: domain.NewDuration(20 * time.Millisecond),
	}

	should, reason := probe.ShouldSwitch(current, candidate, time.Now().UTC(), time.Now().Add(-10*time.Minute), probe.DefaultSwitchPolicy())
	if should {
		t.Fatalf("expected unhealthy candidate to be rejected, got reason %q", reason)
	}
}

func TestTCPCheckerHysteriaFallback(t *testing.T) {
	t.Parallel()

	checker := probe.TCPChecker{Timeout: time.Second}
	result := checker.Check(context.Background(), domain.Node{
		ID:       "node-hy2",
		Address:  "127.0.0.1",
		Protocol: domain.ProtocolHysteria2,
	})

	if !result.Healthy {
		t.Fatalf("expected healthy result from command-not-found fallback or successful local ping, got: %+v", result)
	}
}

func BenchmarkCalculateScore(b *testing.B) {
	health := domain.NodeHealth{
		NodeID:               "node-1",
		Healthy:              true,
		LastLatency:          domain.NewDuration(100 * time.Millisecond),
		AverageLatency:       domain.NewDuration(80 * time.Millisecond),
		ConsecutiveSuccesses: 5,
		SuccessCount:         12,
		FailureCount:         1,
	}
	cfg := probe.DefaultScoreConfig()

	for i := 0; i < b.N; i++ {
		result := probe.CalculateScore(health, cfg)
		if result.NodeID == "" {
			b.Fatal(fmt.Errorf("unexpected empty node id"))
		}
	}
}
