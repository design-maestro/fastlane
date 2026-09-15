package managementhttp

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/app"
	"github.com/design-maestro/fastlane/internal/speedtest"
	"github.com/design-maestro/fastlane/internal/store"
)

func TestVPNPanelHideRestoreUsesExistingSettings(t *testing.T) {
	fs := store.NewFileStore(t.TempDir())
	s := app.NewService(app.Dependencies{Store: fs, AWGStore: fs})
	sub, err := s.AddSubscription(context.Background(), app.AddSubscriptionRequest{Raw: "vless://11111111-1111-4111-8111-111111111111@vpn.example:443?security=tls#Test"})
	if err != nil {
		t.Fatal(err)
	}
	h := mustHandler(t, context.Background(), s, HandlerConfig{AccessToken: testAccessToken}).(*Handler)
	for _, hidden := range []string{"true", "false"} {
		body := `{"subscription_id":"` + sub.ID + `","node_id":"` + sub.Nodes[0].ID + `","hidden":` + hidden + `}`
		if r := request(h, "POST", "/api/v1/vpn/hidden", body, "", ""); r.Code != 401 {
			t.Fatal("unprotected mutation")
		}
		if r := request(h, "POST", "/api/v1/vpn/hidden", body, testAccessToken, ""); r.Code != 202 {
			t.Fatal(r.Body.String())
		}
		deadline := time.Now().Add(time.Second)
		for h.jobs.snapshot().Running && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if !h.jobs.snapshot().Succeeded {
			t.Fatal(h.jobs.snapshot())
		}
		settings, err := fs.LoadSettings()
		if err != nil {
			t.Fatal(err)
		}
		if (len(settings.AutoExcludedNodes) == 1) != (hidden == "true") {
			t.Fatal("existing settings not changed")
		}
	}
	if r := request(h, "GET", "/api/v1/vpn/probes", "", testAccessToken, ""); r.Code != 200 {
		t.Fatal(r.Code)
	}
	if r := request(h, "POST", "/api/v1/vpn/exec", `{"subscription_id":"x"}`, testAccessToken, ""); r.Code != 404 {
		t.Fatal("arbitrary operation accepted")
	}
}

func TestCheckCancellationIsSequenceAndKindBound(t *testing.T) {
	var jobs jobTracker
	job, ok := jobs.start(context.Background(), "health-check", func(ctx context.Context) error { <-ctx.Done(); return nil })
	if !ok {
		t.Fatal("start")
	}
	if jobs.cancelCheck(job.Sequence + 1) {
		t.Fatal("cancelled wrong check")
	}
	if !jobs.cancelCheck(job.Sequence) {
		t.Fatal("not cancelled")
	}
	deadline := time.Now().Add(time.Second)
	for jobs.snapshot().Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if jobs.snapshot().Running || jobs.snapshot().Succeeded || !jobs.snapshot().Cancelled {
		t.Fatal(jobs.snapshot())
	}
	done := make(chan struct{})
	job, _ = jobs.start(context.Background(), "settings", func(context.Context) error { <-done; return nil })
	if jobs.cancelCheck(job.Sequence) {
		t.Fatal("cancelled write")
	}
	close(done)
}

type failingPanelProbeService struct{ *app.Service }

func (failingPanelProbeService) InspectURLTest(context.Context, string, string) (speedtest.URLTestResult, error) {
	return speedtest.URLTestResult{}, errors.New("failed check")
}
func TestFailedPanelProbeKeepsNegativeObservation(t *testing.T) {
	fs := store.NewFileStore(t.TempDir())
	s := app.NewService(app.Dependencies{Store: fs})
	sub, err := s.AddSubscription(context.Background(), app.AddSubscriptionRequest{Raw: "vless://11111111-1111-4111-8111-111111111111@vpn.example:443?security=tls#Test"})
	if err != nil {
		t.Fatal(err)
	}
	h := mustHandler(t, context.Background(), failingPanelProbeService{s}, HandlerConfig{AccessToken: testAccessToken}).(*Handler)
	key := sub.ID + "/" + sub.Nodes[0].ID
	h.probes.values = map[string]panelProbeResult{key: {Success: true, URLTestResult: speedtest.URLTestResult{CheckedAt: time.Now().Add(-time.Hour), LatencyMS: 10}}}
	body := `{"subscription_id":"` + sub.ID + `","node_id":"` + sub.Nodes[0].ID + `"}`
	if r := request(h, "POST", "/api/v1/vpn/check", body, testAccessToken, ""); r.Code != 202 {
		t.Fatal(r.Code)
	}
	deadline := time.Now().Add(time.Second)
	for h.jobs.snapshot().Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	h.probes.Lock()
	defer h.probes.Unlock()
	observed, exists := h.probes.values[key]
	if !exists || observed.Success || time.Since(observed.CheckedAt) > time.Minute {
		t.Fatal("failed check restored stale success")
	}
}

func TestMaintenanceWaitDoesNotAcquireHealthMutex(t *testing.T) {
	calls := make(chan struct{}, 1)
	finished := make(chan struct{})
	h := mustHandler(t, context.Background(), &fakeService{}, HandlerConfig{RunExclusive: func(ctx context.Context, run func(context.Context) error) error { calls <- struct{}{}; return run(ctx) }}).(*Handler)
	h.startMaintenanceJob(httptest.NewRecorder(), "routing-geo", func(context.Context) error { <-finished; return nil })
	select {
	case <-calls:
		t.Fatal("download holds active health mutex")
	case <-time.After(20 * time.Millisecond):
	}
	if !h.jobs.snapshot().Running {
		t.Fatal("maintenance lost job ownership")
	}
	close(finished)
}
