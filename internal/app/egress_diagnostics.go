package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"time"
)

// Only fixed phase names and endpoint ordinals enter diagnostics. URL paths,
// query strings, proxy credentials and raw transport errors must never leak.
type egressEndpointTrace struct {
	mu     sync.Mutex
	phase  string
	result string
}

func (t *egressEndpointTrace) setPhase(phase string) {
	t.mu.Lock()
	t.phase = phase
	t.mu.Unlock()
}

func (t *egressEndpointTrace) finish(result string) {
	t.mu.Lock()
	t.result = result
	t.mu.Unlock()
}

func (t *egressEndpointTrace) summary(index int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := t.result
	if result == "" {
		result = "deadline"
	}
	return fmt.Sprintf("endpoint_%d phase=%s result=%s", index+1, t.phase, result)
}

func (t *egressEndpointTrace) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSStart:     func(httptrace.DNSStartInfo) { t.setPhase("dns") },
		ConnectStart: func(string, string) { t.setPhase("tcp") },
		ConnectDone: func(_, _ string, err error) {
			if err == nil {
				t.setPhase("proxy_connect")
			}
		},
		TLSHandshakeStart: func() { t.setPhase("tls") },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			if err == nil {
				t.setPhase("request")
			}
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				t.setPhase("response_headers")
			}
		},
	}
}

func probeEgressEndpoints(ctx context.Context, client *http.Client, endpoints []string) error {
	if len(endpoints) == 0 {
		return errors.New("no egress probe endpoints configured")
	}
	started := time.Now()
	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan bool, len(endpoints))
	traces := make([]*egressEndpointTrace, len(endpoints))
	for i, endpoint := range endpoints {
		trace := &egressEndpointTrace{phase: "request_setup"}
		traces[i] = trace
		go func() {
			requestCtx := httptrace.WithClientTrace(probeCtx, trace.clientTrace())
			req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
			if err != nil {
				trace.finish("invalid_request")
				results <- false
				return
			}
			response, err := client.Do(req)
			if err != nil {
				kind := "transport_error"
				var networkErr net.Error
				if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkErr) && networkErr.Timeout()) {
					kind = "timeout"
				} else if errors.Is(err, context.Canceled) {
					kind = "cancelled"
				}
				trace.finish(kind)
				results <- false
				return
			}
			// Receipt of HTTPS response headers already proves egress. Waiting
			// for an arbitrary page body can turn a valid 200 into a false timeout.
			trace.setPhase("response_headers")
			trace.finish(fmt.Sprintf("http_%d", response.StatusCode))
			ok := response.StatusCode >= 200 && response.StatusCode < 500
			results <- ok
			_ = response.Body.Close()
		}()
	}
	diagnostic := func(cause error) error {
		parts := make([]string, len(traces))
		for i, trace := range traces {
			parts[i] = trace.summary(i)
		}
		return fmt.Errorf("egress failed after %dms: %s: %w", time.Since(started).Milliseconds(), strings.Join(parts, "; "), cause)
	}
	for range endpoints {
		select {
		case ok := <-results:
			if ok {
				return nil
			}
		case <-ctx.Done():
			return diagnostic(ctx.Err())
		}
	}
	return diagnostic(errors.New("all control requests failed"))
}
