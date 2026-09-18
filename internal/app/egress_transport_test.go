package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
)

// Each Xray health pass owns a fresh transport so it observes the selected
// outbound and closes its now-unused proxy connection pool. AWG has a separate
// bounded reusable transport covered below.
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

func TestEgressDiagnosticsIdentifyStagesWithoutLeakingURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/status") {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err := probeEgressEndpoints(ctx, server.Client(), []string{server.URL + "/status?token=secret-one", server.URL + "/secret-path?token=secret-two"})
	if err == nil {
		t.Fatal("both failures accepted")
	}
	message := err.Error()
	for _, expected := range []string{"endpoint_1 phase=response_headers result=http_503", "endpoint_2 phase="} {
		if !strings.Contains(message, expected) {
			t.Fatalf("missing %q in %q", expected, message)
		}
	}
	for _, secret := range []string{"secret", server.URL, "token="} {
		if strings.Contains(message, secret) {
			t.Fatalf("diagnostic leaked %q", secret)
		}
	}
}

func TestEgressAcceptsResponseHeadersWithoutWaitingForSlowBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := probeEgressEndpoints(ctx, server.Client(), []string{server.URL}); err != nil {
		t.Fatalf("received HTTP 200 was misclassified as failure: %v", err)
	}
}

func TestEgressDiagnosticsDistinguishProxyTunnelFromTLS(t *testing.T) {
	for _, tlsStarted := range []bool{false, true} {
		t.Run(fmt.Sprint(tlsStarted), func(t *testing.T) {
			release := make(chan struct{})
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tlsStarted {
					conn, buffered, err := w.(http.Hijacker).Hijack()
					if err != nil {
						return
					}
					defer conn.Close()
					_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
					_ = buffered.Flush()
				}
				<-release
			}))
			defer proxy.Close()
			defer close(release)
			proxyURL, _ := url.Parse(proxy.URL)
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = http.ProxyURL(proxyURL)
			defer transport.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			err := probeEgressEndpoints(ctx, &http.Client{Transport: transport}, []string{"https://secret.invalid/private?token=secret"})
			phase := "proxy_connect"
			if tlsStarted {
				phase = "tls"
			}
			if err == nil || !strings.Contains(err.Error(), "phase="+phase) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("expected safe %s diagnostic, got %v", phase, err)
			}
		})
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

func TestAWGEgressProbeForcesFreshConnectionEveryFiveMinutes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := server.Client()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T", client.Transport)
	}
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	service := &Service{
		store:              &urlContractStore{settings: domain.Settings{URLTestURL: server.URL, URLTestFallbackURL: server.URL}},
		now:                func() time.Time { return now },
		awgEgressDevice:    "test-device",
		awgEgressTransport: transport,
		awgEgressClient:    client,
		awgEgressFreshAt:   now.Add(-awgEgressFreshConnectionInterval),
	}
	if err := service.defaultAWGEgressProbe(context.Background(), "test-device"); err != nil {
		t.Fatalf("fresh AWG probe: %v", err)
	}
	if !service.awgEgressFreshAt.Equal(now) {
		t.Fatalf("fresh connection timestamp = %s, want %s", service.awgEgressFreshAt, now)
	}
	now = now.Add(4 * time.Minute)
	if err := service.defaultAWGEgressProbe(context.Background(), "test-device"); err != nil {
		t.Fatalf("pooled AWG probe: %v", err)
	}
	if !service.awgEgressFreshAt.Equal(now.Add(-4 * time.Minute)) {
		t.Fatalf("healthy pool was rotated early: %s", service.awgEgressFreshAt)
	}
}

type egressTestConnectionIDKey struct{}

func TestAWGEgressProbeRetriesStalledKeepAliveOnFreshConnection(t *testing.T) {
	var connectionCount atomic.Int32
	var stallExisting atomic.Bool
	var existingConnections atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connectionID, _ := r.Context().Value(egressTestConnectionIDKey{}).(int32)
		if stallExisting.Load() && connectionID <= existingConnections.Load() {
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Config.ConnContext = func(ctx context.Context, _ net.Conn) context.Context {
		return context.WithValue(ctx, egressTestConnectionIDKey{}, connectionCount.Add(1))
	}
	server.Start()
	defer server.Close()

	client := server.Client()
	transport := client.Transport.(*http.Transport)
	if err := probeEgressEndpoints(context.Background(), client, []string{server.URL}); err != nil {
		t.Fatalf("prime keep-alive connection: %v", err)
	}
	existingConnections.Store(connectionCount.Load())
	stallExisting.Store(true)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	retried, err := probeAWGEgressEndpoints(ctx, client, transport, []string{server.URL}, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("fresh retry did not recover stalled keep-alive: %v", err)
	}
	if !retried {
		t.Fatal("stalled keep-alive did not trigger a fresh retry")
	}
	if connectionCount.Load() <= existingConnections.Load() {
		t.Fatalf("fresh retry reused stalled connection: connections=%d", connectionCount.Load())
	}
}
