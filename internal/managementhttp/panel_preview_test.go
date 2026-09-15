package managementhttp

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/app"
	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/store"
)

// A store-only playground must never mark a VLESS connection successful merely
// because app.Service allows a nil backend in unit tests.
type previewService struct{ *app.Service }

func (previewService) ConnectManual(context.Context, string, string) error {
	return panelJobError("vpn_runtime_unavailable")
}
func (previewService) ConnectAuto(context.Context, string) (domain.Node, error) {
	return domain.Node{}, panelJobError("vpn_runtime_unavailable")
}
func (previewService) RunAutoHealthCheck(context.Context) error {
	return panelJobError("vpn_runtime_unavailable")
}

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
	h := mustHandler(t, context.Background(), previewService{s}, HandlerConfig{LoopbackOnly: true})
	if _, err := s.ImportAWGProfile("AWG — демонстрационный файл", []byte(panelAWGFixture())); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		_, _ = w.Write(bytes.Replace(recorder.Body.Bytes(), []byte(`<main id="main" class="fl-shell">`), []byte(`<main id="main" class="fl-shell"><p class="preview-note">Локальный прототип · данные демонстрационные · сеть роутера не изменяется</p>`), 1))
	}))
	if address := os.Getenv("FASTLANE_PANEL_PREVIEW_ADDR"); address != "" {
		host, _, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			t.Fatal("preview address must be loopback")
		}
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		_ = server.Listener.Close()
		server.Listener = listener
	}
	server.Start()
	defer server.Close()
	t.Logf("Standalone panel preview: %s (available for 12 hours)", server.URL)
	<-time.After(12 * time.Hour)
}
