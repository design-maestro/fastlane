package speedtest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestURLTestEndpointsEitherSuccessAndBothFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			w.WriteHeader(204)
		} else {
			w.WriteHeader(503)
		}
	}))
	defer server.Close()
	for _, pair := range [][2]string{{"/ok", "/bad"}, {"/bad", "/ok"}, {"/bad", "/bad"}} {
		_, err := measureURLTestEndpoints(context.Background(), server.Client(), server.URL+pair[0], server.URL+pair[1], time.Second)
		if (err == nil) != (pair[0] == "/ok" || pair[1] == "/ok") {
			t.Fatalf("pair=%v error=%v", pair, err)
		}
	}
}
