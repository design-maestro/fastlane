package speedtest

import (
	"context"
	"net/http"
	"time"
)

// Both endpoints share one round budget. One reachable target is sufficient;
// a failed public check service must not disqualify a working VPN candidate.
func measureURLTestEndpoints(ctx context.Context, client *http.Client, primary, fallback string, timeout time.Duration) (time.Duration, error) {
	if fallback == "" || fallback == primary {
		return measureSingleLatencyWithTimeout(ctx, client, primary, timeout)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		latency time.Duration
		err     error
	}
	results := make(chan result, 2)
	for _, endpoint := range []string{primary, fallback} {
		go func(endpoint string) {
			latency, err := measureSingleLatencyWithTimeout(ctx, client, endpoint, timeout)
			results <- result{latency, err}
		}(endpoint)
	}
	var lastErr error
	for range 2 {
		res := <-results
		if res.err == nil {
			return res.latency, nil
		}
		lastErr = res.err
	}
	return 0, lastErr
}
