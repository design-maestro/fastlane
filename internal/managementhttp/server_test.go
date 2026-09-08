package managementhttp

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestValidateExposure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		address  string
		token    string
		loopback bool
		wantErr  bool
	}{
		{name: "loopback without token", address: "127.0.0.1:9080", loopback: true},
		{name: "ipv6 loopback without token", address: "[::1]:9080", loopback: true},
		{name: "lan requires token", address: "192.168.1.1:9080", wantErr: true},
		{name: "wildcard requires token", address: ":9080", wantErr: true},
		{name: "lan with strong token", address: "192.168.1.1:9080", token: "0123456789abcdef0123456789abcdef"},
		{name: "weak token rejected", address: "127.0.0.1:9080", token: "too-short", wantErr: true},
		{name: "hostname rejected", address: "router.lan:9080", token: "0123456789abcdef0123456789abcdef", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			loopback, err := validateExposure(test.address, test.token)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected validation error")
				}
				return
			}
			if err != nil {
				t.Fatalf("validate exposure: %v", err)
			}
			if loopback != test.loopback {
				t.Fatalf("loopback = %t, want %t", loopback, test.loopback)
			}
		})
	}
}

func TestServerStopsWithContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	server, err := NewServer(ctx, Config{ListenAddr: "127.0.0.1:0"}, &fakeService{})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()

	response, err := http.Get("http://" + server.Addr().String() + "/api/v1/state")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run server: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop after context cancellation")
	}
}
