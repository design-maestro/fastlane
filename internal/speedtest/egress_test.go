package speedtest

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type egressRoundTripFunc func(*http.Request) (*http.Response, error)

func (f egressRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestProbeEgressIdentityParsesPublicAddressAndCountry(t *testing.T) {
	client := &http.Client{Transport: egressRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != egressTraceURL {
			t.Fatalf("URL = %q", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("fl=123\nip=203.0.113.9\nloc=NL\n")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}
	ip, country := ProbeEgressIdentity(context.Background(), client, time.Second)
	if ip != "203.0.113.9" || country != "NL" {
		t.Fatalf("identity = %q/%q", ip, country)
	}
}

func TestProbeEgressIdentityIgnoresInvalidMetadata(t *testing.T) {
	client := &http.Client{Transport: egressRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ip=not-an-ip\nloc=Russia\n")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}
	ip, country := ProbeEgressIdentity(context.Background(), client, time.Second)
	if ip != "" || country != "" {
		t.Fatalf("invalid identity was accepted: %q/%q", ip, country)
	}
}
