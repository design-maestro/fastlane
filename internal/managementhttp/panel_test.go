package managementhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/app"
	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/store"
)

func TestPanelAssetsDoNotExposeStateWithoutLogin(t *testing.T) {
	h := mustHandler(t, context.Background(), &fakeService{}, HandlerConfig{AccessToken: testAccessToken})
	for _, path := range []string{"/", "/panel.js", "/panel.css", "/legacy.css", "/legacy-icons.js", "/legacy-brand.png"} {
		r := request(h, "GET", path, "", "", "")
		if r.Code != 200 || strings.Contains(r.Body.String(), testAccessToken) {
			t.Fatalf("asset %s: %d", path, r.Code)
		}
		if !strings.Contains(r.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Fatal("missing CSP")
		}
	}
	for _, path := range []string{"/api/v1/state", "/api/v1/awg"} {
		if r := request(h, "GET", path, "", "", ""); r.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", path, r.Code)
		}
	}
	if r := request(h, "GET", "/../handler.go", "", "", ""); r.Code == 200 {
		t.Fatal("served source code")
	}
}

func TestPanelDesignMatchesLuCI(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required for frontend asset drift check")
	}
	cmd := exec.Command("node", "scripts/sync-panel-design.cjs", "--check")
	cmd.Dir = "../.."
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("shared design drift: %v\n%s", err, output)
	}
}

func TestPanelSessionsExpireAndLogoutRevokesOnlyCurrentSession(t *testing.T) {
	h := mustHandler(t, context.Background(), &fakeService{}, HandlerConfig{AccessToken: testAccessToken}).(*Handler)
	login := func() *http.Cookie {
		r := request(h, "POST", "/api/v1/session", `{"token":"`+testAccessToken+`"}`, "", "")
		return r.Result().Cookies()[0]
	}
	a, b := login(), login()
	if a.Value == b.Value {
		t.Fatal("sessions reused")
	}
	withCookie := func(method, path string, cookie *http.Cookie) int {
		r := httptest.NewRequest(method, "http://router.local"+path, nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if withCookie("DELETE", "/api/v1/session", a) != 200 || withCookie("GET", "/api/v1/state", a) != 401 {
		t.Fatal("logout did not revoke session")
	}
	if withCookie("GET", "/api/v1/state", b) != 200 {
		t.Fatal("logout revoked other session")
	}
	h.now = func() time.Time { return time.Now().Add(13 * time.Hour) }
	if withCookie("GET", "/api/v1/state", b) != 401 {
		t.Fatal("session did not expire server-side")
	}
}

func TestPanelJobsApplyThroughRealService(t *testing.T) {
	fs := store.NewFileStore(t.TempDir())
	s := app.NewService(app.Dependencies{Store: fs, AWGStore: fs})
	h := mustHandler(t, context.Background(), s, HandlerConfig{AccessToken: testAccessToken}).(*Handler)
	post := func(path, body string) {
		t.Helper()
		r := request(h, "POST", path, body, testAccessToken, "")
		if r.Code != 202 {
			t.Fatalf("%s: %d %s", path, r.Code, r.Body.String())
		}
		deadline := time.Now().Add(time.Second)
		for h.jobs.snapshot().Running && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if !h.jobs.snapshot().Succeeded {
			t.Fatalf("job %s failed", path)
		}
	}
	post("/api/v1/jobs/settings", `{"refresh-interval":"2h"}`)
	post("/api/v1/hide-keywords", `{"keywords":["Россия","LTE"]}`)
	settings, err := fs.LoadSettings()
	if err != nil || settings.RefreshInterval.Duration() != 2*time.Hour || len(settings.AutoHideKeywords) != 2 {
		t.Fatalf("settings not persisted: %v", err)
	}
	post("/api/v1/routing", `{"mode":"bypass","bypass":["example.com"],"excluded":[],"proxy":[]}`)
	settings, _ = fs.LoadSettings()
	if len(settings.Firewall.Split.Bypass.Domains) != 1 {
		t.Fatal("routing not persisted")
	}
	settings.DNS.Bootstrap = []string{"192.0.2.53"}
	settings.DNS.DirectDomains = []string{"domain:lan", "full:router.lan"}
	if err := fs.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	post("/api/v1/dns", `{"mode":"split","transport":"doh","servers":["https://dns.example/dns-query"]}`)
	settings, _ = fs.LoadSettings()
	if settings.DNS.Transport != domain.DNSTransportDoH || len(settings.DNS.Servers) != 1 || settings.DNS.Bootstrap[0] != "192.0.2.53" || len(settings.DNS.DirectDomains) != 2 {
		t.Fatal("DNS patch lost existing bootstrap or local domain rules")
	}
	if r := request(h, "POST", "/api/v1/dns", `{"mode":"split","transport":"dot"}`, testAccessToken, ""); r.Code != 400 {
		t.Fatal("unsupported DNS transport accepted")
	}
	if r := request(h, "POST", "/api/v1/awg", `{"config":"[Interface]\nPostUp = echo forbidden"}`, testAccessToken, ""); r.Code != 202 {
		t.Fatal(r.Body.String())
	}
	deadline := time.Now().Add(time.Second)
	for h.jobs.snapshot().Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if h.jobs.snapshot().Succeeded {
		t.Fatal("unsafe AWG accepted")
	}
	if h.jobs.snapshot().Error != "unsafe_awg_directive" {
		t.Fatal("AWG validation did not return a safe, actionable error code")
	}
	profiles, err := fs.ListAWGProfiles()
	if err != nil || len(profiles) != 0 {
		t.Fatalf("unsafe AWG persisted: %+v, %v", profiles, err)
	}
}
