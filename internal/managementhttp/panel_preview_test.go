package managementhttp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/app"
	"github.com/design-maestro/fastlane/internal/store"
)

// Local, opt-in UI playground. No Xray, firewall, DNS or AWG controllers are
// installed, and all data lives in a disposable store, never in /etc/fastlane.
func TestStandalonePanelPreview(t *testing.T) {
	if os.Getenv("FASTLANE_PANEL_PREVIEW") != "1" {
		t.Skip("set FASTLANE_PANEL_PREVIEW=1")
	}
	fs := store.NewFileStore(t.TempDir())
	s := app.NewService(app.Dependencies{Store: fs, AWGStore: fs})
	_, err := s.AddSubscription(context.Background(), app.AddSubscriptionRequest{
		Name: "Демонстрационный источник",
		Raw:  "vless://11111111-1111-4111-8111-111111111111@vpn.example:443?security=tls#Demo%20server",
	})
	if err != nil {
		t.Fatal(err)
	}
	h := mustHandler(t, context.Background(), s, HandlerConfig{LoopbackOnly: true})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" || r.Method != http.MethodGet {
			h.ServeHTTP(w, r)
			return
		}
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, r)
		for key, values := range recorder.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(bytes.Replace(recorder.Body.Bytes(), []byte(`<main id="main">`), []byte(`<main id="main"><p>Локальный прототип · данные демонстрационные · сеть роутера не изменяется</p>`), 1))
	}))
	defer server.Close()
	t.Logf("Standalone panel preview: %s (available for 60 minutes)", server.URL)
	<-time.After(time.Hour)
}
