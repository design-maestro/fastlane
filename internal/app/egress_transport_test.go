package app

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
)

// Each health pass owns a fresh transport so it observes the selected route,
// not a keep-alive connection opened through a previous outbound. That also
// makes the pass responsible for closing its now-unused connection pool.
func TestBackendEgressProbeClosesOwnedIdleConnections(t *testing.T) {
	var closed atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			closed.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	service := &Service{httpClient: &http.Client{Transport: transport}, store: &urlContractStore{settings: domain.Settings{URLTestURL: "http://probe.invalid/generate_204", URLTestFallbackURL: "http://probe.invalid/generate_204"}}}
	const passes = 8
	for range passes {
		if err := service.defaultBackendEgressProbe(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(time.Second)
	for closed.Load() < passes && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := closed.Load(); got != passes {
		t.Fatalf("completed probes closed %d/%d owned connections", got, passes)
	}
}

func TestBackendEgressProbeAcceptsEitherEndpointAndRejectsBothFailures(t *testing.T) {
	for _, healthy := range []string{"first.invalid", "second.invalid", ""} {
		t.Run(healthy, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host == healthy {
					w.WriteHeader(http.StatusNoContent)
				} else {
					w.WriteHeader(http.StatusServiceUnavailable)
				}
			}))
			defer server.Close()
			transport := http.DefaultTransport.(*http.Transport).Clone()
			defer transport.CloseIdleConnections()
			transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}
			service := &Service{httpClient: &http.Client{Transport: transport}, store: &urlContractStore{settings: domain.Settings{URLTestURL: "http://first.invalid/check", URLTestFallbackURL: "http://second.invalid/check"}}}
			err := service.defaultBackendEgressProbe(context.Background())
			if (err == nil) != (healthy != "") {
				t.Fatalf("healthy=%q error=%v", healthy, err)
			}
		})
	}
}
