package managementhttp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/design-maestro/fastlane/internal/app"
	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/store"
)

func TestRoutingGeoForegroundUpdateReadsStatusAfterExit(t *testing.T) {
	var actions []string
	status, err := runRoutingGeoUpdate(context.Background(), func(ctx context.Context, action string) (routingGeoStatus, error) {
		actions = append(actions, action)
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 15*time.Minute {
			t.Fatal("update has no bounded work deadline")
		}
		if action == "update" {
			return routingGeoStatus{Ready: true, Updating: true, LastResult: "ok"}, nil
		}
		return routingGeoStatus{Ready: true, LastResult: "ok"}, nil
	})
	if err != nil || !status.Ready || status.Updating || !reflect.DeepEqual(actions, []string{"update", "status"}) {
		t.Fatalf("foreground/status sequence: %v %+v %v", actions, status, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	actions = nil
	_, err = runRoutingGeoUpdate(ctx, func(context.Context, string) (routingGeoStatus, error) {
		actions = append(actions, "update")
		cancel()
		return routingGeoStatus{Ready: true}, nil
	})
	if err != panelJobError("geo_update_timeout") || len(actions) != 1 {
		t.Fatalf("cancelled update read status/claimed success: %v %v", actions, err)
	}
	if _, err := runRoutingGeoHelper(context.Background(), "start"); err != panelJobError("invalid_request") {
		t.Fatalf("detached action still allowed: %v", err)
	}
}

func TestRoutingGeoForegroundCancellationWaitsForCleanupUnderHealthLock(t *testing.T) {
	dir := t.TempDir()
	// Only owned synthetic scripts and temporary markers. The child stands in
	// for wget/Xray; the parent models the actual helper's TERM -> EXIT cleanup.
	parent := `#!/bin/sh
set -eu
task_dir="$1"
cleanup() {
  trap '' TERM
  printf 'started' > "$task_dir/cleanup-started"
  sleep 0.3
  printf 'done' > "$task_dir/cleaned"
}
trap cleanup EXIT
trap 'exit 143' TERM
test -z "${FASTLANE_GEODATA_BASE_URL-}"
/bin/sh "$task_dir/child.sh" "$task_dir" &
child=$!
wait "$child"
printf 'unexpected' > "$task_dir/committed"
`
	child := `#!/bin/sh
set -eu
task_dir="$1"
trap 'printf stopped > "$task_dir/child-stopped"; exit 143' TERM
printf '%s' "$$" > "$task_dir/child-pid"
printf 'ready' > "$task_dir/ready"
while :; do sleep 30; done
`
	for name, script := range map[string]string{"parent.sh": parent, "child.sh": child} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("FASTLANE_GEODATA_BASE_URL", "https://synthetic-override.invalid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, _, _ := newRoutingTest(t)
	h.baseContext = ctx
	var healthMu sync.Mutex
	h.runExclusive = func(ctx context.Context, run func(context.Context) error) error {
		healthMu.Lock()
		defer healthMu.Unlock()
		return run(ctx)
	}
	command := exec.CommandContext(ctx, "/bin/sh", filepath.Join(dir, "parent.sh"), dir)
	h.startJob(httptest.NewRecorder(), "routing-geo", func(context.Context) error { _, err := runRoutingGeoProcess(ctx, command); return err })
	awaitFile := func(name string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("missing marker %s, job=%+v", name, h.jobs.snapshot())
	}
	awaitFile("ready")
	cancel()
	awaitFile("cleanup-started")
	if healthMu.TryLock() {
		healthMu.Unlock()
		t.Error("health mutex released while rollback/cleanup still runs")
	}
	if !h.jobs.snapshot().Running {
		t.Error("cancelled job completed before cleanup")
	}
	awaitFile("cleaned")
	awaitFile("child-stopped")
	deadline := time.Now().Add(3 * time.Second)
	for h.jobs.snapshot().Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	job := h.jobs.snapshot()
	// The root coordinator marks daemon-context cancellation explicitly. The
	// foreground runner itself returns the safe geo_update_timeout code.
	if job.Running || job.Succeeded || !job.Cancelled || job.Error != "operation_cancelled" {
		t.Fatalf("cancelled foreground job: %+v", job)
	}
	if !healthMu.TryLock() {
		t.Fatal("health remains locked after cleanup")
	}
	healthMu.Unlock()
	if command.ProcessState == nil || !command.ProcessState.Exited() {
		t.Fatal("foreground helper not reaped")
	}
	if _, err := os.Stat(filepath.Join(dir, "committed")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("helper continued after cancellation")
	}
	pidBytes, err := os.ReadFile(filepath.Join(dir, "child-pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		t.Fatal(err)
	}
	// The child handled TERM. Verify it is no longer a running group member;
	// allow the OS a brief interval to reap a reparented process.
	deadline = time.Now().Add(time.Second)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("owned child survived cancellation: %v", err)
	}
}

func TestRoutingGeoForegroundErrorsAndOutputAreBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := runRoutingGeoProcess(ctx, exec.CommandContext(ctx, "/bin/sh", "-c", "printf synthetic-private-error >&2; exit 1"))
	if err != panelJobError("geo_update_failed") {
		t.Fatalf("raw process error exposed: %v", err)
	}
	var output routingGeoOutput
	if n, err := output.Write(make([]byte, 128<<10)); err != nil || n != 128<<10 || len(output.data) != 64<<10 || !output.overflow {
		t.Fatal("unbounded output")
	}
	if n, err := output.Write([]byte("extra")); n != 5 || err != nil || len(output.data) != 64<<10 {
		t.Fatal("overflow writer must drain without killing cleanup")
	}
}

// Model a detached Geo worker: Start accepts, Status observes its eventual
// database commit/reload, and only then may country settings be patched.
type routingExclusiveGeoService struct {
	*routingTestService
	check       func(string)
	started     chan struct{}
	release     chan struct{}
	startErr    error
	patchCalls  int
	statusCalls int
}

func (s *routingExclusiveGeoService) RoutingGeoStart(ctx context.Context) (routingGeoStatus, error) {
	s.check("helper start")
	close(s.started)
	select {
	case <-s.release:
	case <-ctx.Done():
		return routingGeoStatus{}, ctx.Err()
	}
	if s.startErr != nil {
		return routingGeoStatus{}, s.startErr
	}
	return routingGeoStatus{Updating: true, LastResult: "updating"}, nil
}

func (s *routingExclusiveGeoService) RoutingGeoStatus(context.Context) (routingGeoStatus, error) {
	s.check("helper status/commit/reload")
	s.statusCalls++
	select {
	case <-s.started:
		return routingGeoStatus{Ready: true, LastResult: "ok"}, nil
	default:
		return routingGeoStatus{Ready: false, LastResult: "never"}, nil
	}
}

func (s *routingExclusiveGeoService) PatchSettings(values map[string]string) (domain.Settings, error) {
	s.check("country commit")
	s.patchCalls++
	return s.routingTestService.PatchSettings(values)
}

func TestRoutingGeoMaintenanceOwnsHealthLockUntilCompletion(t *testing.T) {
	for _, route := range []string{"geo/update", "country"} {
		for _, fail := range []bool{false, true} {
			t.Run(route+"/failure="+strconv.FormatBool(fail), func(t *testing.T) {
				h, base, fs := newRoutingTest(t)
				var healthMu sync.Mutex
				var exclusive atomic.Bool
				var entries atomic.Int32
				s := &routingExclusiveGeoService{routingTestService: base, started: make(chan struct{}), release: make(chan struct{})}
				s.check = func(phase string) {
					if !exclusive.Load() {
						t.Errorf("%s ran outside health lock", phase)
					}
				}
				if fail {
					s.startErr = panelJobError("geo_update_failed")
				}
				h.service = s
				h.runExclusive = func(ctx context.Context, run func(context.Context) error) error {
					// Detect accidental nested acquisition without hanging the test.
					if !healthMu.TryLock() {
						return errors.New("nested health lock")
					}
					defer healthMu.Unlock()
					exclusive.Store(true)
					defer exclusive.Store(false)
					entries.Add(1)
					return run(ctx)
				}
				body := `{}`
				if route == "country" {
					body = `{"country":"DE","enabled":true}`
				}
				w := routingRequest(h, "POST", route, body)
				if w.Code != 202 {
					t.Fatal(w.Body.String())
				}
				select {
				case <-s.started:
				case <-time.After(time.Second):
					t.Fatal("helper did not start")
				}
				if healthMu.TryLock() {
					healthMu.Unlock()
					t.Error("health can overlap helper download/reload")
				}
				if job := h.jobs.snapshot(); !job.Running || job.Succeeded {
					t.Errorf("helper acceptance treated as completion: %+v", job)
				}
				close(s.release)
				deadline := time.Now().Add(5 * time.Second)
				for h.jobs.snapshot().Running && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				job := h.jobs.snapshot()
				if job.Running || job.Succeeded == fail || entries.Load() != 1 {
					t.Fatalf("job=%+v exclusive entries=%d", job, entries.Load())
				}
				if fail && job.Error != "geo_update_failed" {
					t.Fatalf("lost safe helper error: %+v", job)
				}
				if !healthMu.TryLock() {
					t.Fatal("health lock not released")
				}
				healthMu.Unlock()
				settings, _ := fs.LoadSettings()
				wantCommit := route == "country" && !fail
				if settings.CountryRouting.Enabled != wantCommit || (s.patchCalls == 1) != wantCommit {
					t.Fatalf("country committed prematurely: %+v, calls=%d", settings.CountryRouting, s.patchCalls)
				}
				if !fail && s.statusCalls == 0 {
					t.Fatal("helper completion was not polled")
				}
			})
		}
	}
}

type routingTestService struct {
	*app.Service
	geo      routingGeoStatus
	geoErr   error
	groupErr error
	listErr  error
	patchErr error
	block    <-chan struct{}
}

func (s *routingTestService) RoutingGeoStatus(context.Context) (routingGeoStatus, error) {
	return s.geo, s.geoErr
}
func (s *routingTestService) RoutingGeoStart(context.Context) (routingGeoStatus, error) {
	return s.geo, s.geoErr
}
func (s *routingTestService) ListFirewallTargetServices() ([]domain.FirewallTargetService, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.Service.ListFirewallTargetServices()
}
func (s *routingTestService) PatchSettings(values map[string]string) (domain.Settings, error) {
	if s.block != nil {
		<-s.block
	}
	if s.patchErr != nil {
		return domain.Settings{}, s.patchErr
	}
	return s.Service.PatchSettings(values)
}
func (s *routingTestService) ConfigureFirewallBypass(ctx context.Context, targets, sources []string, enabled bool, port int) (domain.FirewallSettings, error) {
	if s.groupErr != nil {
		return domain.FirewallSettings{}, s.groupErr
	}
	return s.Service.ConfigureFirewallBypass(ctx, targets, sources, enabled, port)
}

func newRoutingTest(t *testing.T) (*Handler, *routingTestService, *store.FileStore) {
	t.Helper()
	fs := store.NewFileStore(t.TempDir())
	s := &routingTestService{Service: app.NewService(app.Dependencies{Store: fs}), geo: routingGeoStatus{Ready: true, LastResult: "ok"}}
	h := mustHandler(t, context.Background(), s, HandlerConfig{AccessToken: testAccessToken}).(*Handler)
	return h, s, fs
}
func routingRequest(h *Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://router.local/api/v1/routing/"+path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+testAccessToken)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	if !h.routingPanelAPI(w, r) {
		http.NotFound(w, r)
	}
	return w
}
func routingJob(t *testing.T, h *Handler, path, body, wantError string) {
	t.Helper()
	w := routingRequest(h, "POST", path, body)
	if w.Code != 202 {
		t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	for h.jobs.snapshot().Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	job := h.jobs.snapshot()
	if job.Running || job.Error != wantError || job.Succeeded != (wantError == "") {
		t.Fatalf("%s: job=%+v want error=%q", path, job, wantError)
	}
}

func TestRoutingCountryPersistence(t *testing.T) {
	h, _, fs := newRoutingTest(t)
	routingJob(t, h, "country", `{"country":"DE"}`, "")
	settings, err := fs.LoadSettings()
	if err != nil || settings.CountryRouting.CountryCode != "DE" || settings.CountryRouting.Enabled {
		t.Fatalf("country: %+v %v", settings.CountryRouting, err)
	}
	routingJob(t, h, "country", `{"enabled":true}`, "")
	settings, _ = fs.LoadSettings()
	if !settings.CountryRouting.Enabled || !settings.Firewall.Enabled || settings.Firewall.Split.DefaultAction != domain.FirewallDefaultActionProxy {
		t.Fatal("country not enabled with managed VPN default")
	}
	routingJob(t, h, "country", `{"country":"NZ"}`, "")
	routingJob(t, h, "country", `{"enabled":false}`, "")
	// A new service must see the same persisted rule, not panel-local state.
	reloaded := app.NewService(app.Dependencies{Store: fs})
	state, err := reloaded.Status()
	if err != nil || state.Settings.CountryRouting.Enabled || state.Settings.CountryRouting.CountryCode != "NZ" || !state.Settings.Firewall.Enabled {
		t.Fatalf("country toggle lost other routing: %+v %v", state.Settings.CountryRouting, err)
	}
}

func TestRoutingGroupsCRUDPreservesSelectors(t *testing.T) {
	h, s, fs := newRoutingTest(t)
	_, err := s.Service.ConfigureFirewallBypass(context.Background(), []string{"existing.example", "198.51.100.0/24"}, []string{"192.0.2.44"}, true, 12345)
	if err != nil {
		t.Fatal(err)
	}
	routingJob(t, h, "groups", `{"name":"robot","domains":["example.com"],"cidrs":["192.0.2.10","192.0.2.0/24"]}`, "")
	state, _ := fs.LoadSettings()
	if len(state.Firewall.Split.Bypass.Services) != 1 || state.Firewall.Split.Bypass.Services[0] != "robot" {
		t.Fatal("new group must activate")
	}
	before := state.Firewall.Split
	routingJob(t, h, "groups/edit", `{"name":"robot","domains":["changed.example"],"cidrs":["203.0.113.0/24"]}`, "")
	saved, err := s.GetFirewallTargetService("robot")
	if err != nil || !reflect.DeepEqual(saved.Domains, []string{"changed.example"}) {
		t.Fatalf("edit not persisted: %+v %v", saved, err)
	}
	routingJob(t, h, "groups/toggle", `{"name":"robot","enabled":false}`, "")
	state, _ = fs.LoadSettings()
	if len(state.Firewall.Split.Bypass.Services) != 0 {
		t.Fatal("group still enabled")
	}
	if !reflect.DeepEqual(state.Firewall.Split.Bypass.Domains, before.Bypass.Domains) || !reflect.DeepEqual(state.Firewall.Split.Bypass.CIDRs, before.Bypass.CIDRs) || !reflect.DeepEqual(state.Firewall.Split.ExcludedSources, before.ExcludedSources) || state.Firewall.TransparentPort != 12345 {
		t.Fatal("toggle lost unrelated bypass settings")
	}
	routingJob(t, h, "groups/toggle", `{"name":"robot","enabled":true}`, "")
	routingJob(t, h, "groups/delete", `{"name":"robot"}`, "")
	if _, err := s.GetFirewallTargetService("robot"); err == nil {
		t.Fatal("group deletion not persisted")
	}
	state, _ = fs.LoadSettings()
	if len(state.Firewall.Split.Bypass.Services) != 0 || !reflect.DeepEqual(state.Firewall.Split.Bypass.Domains, before.Bypass.Domains) {
		t.Fatal("delete lost unrelated settings")
	}
}

func TestRoutingGroupValidationAndPartialFailure(t *testing.T) {
	h, s, fs := newRoutingTest(t)
	routingJob(t, h, "groups", `{"name":"robot","domains":["example.com"]}`, "")
	before, _ := fs.LoadSettings()
	for _, tc := range []struct{ path, body, code string }{
		{"groups", `{"name":"robot","domains":["evil.example"]}`, "routing_group_exists"},
		{"groups/edit", `{"name":"youtube","domains":["evil.example"]}`, "routing_group_not_editable"},
		{"groups/edit", `{"name":"missing","domains":["evil.example"]}`, "routing_group_not_editable"},
		{"groups/edit", `{"name":"robot","domains":["$(touch /tmp/forbidden)"]}`, "invalid_routing_group"},
		{"groups/toggle", `{"name":"youtube","enabled":true}`, "routing_group_not_editable"},
		{"groups/delete", `{"name":"youtube"}`, "routing_group_not_editable"},
	} {
		routingJob(t, h, tc.path, tc.body, tc.code)
	}
	after, _ := fs.LoadSettings()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("invalid group operation mutated settings")
	}
	// Existing composite members survive the UI's domain/IP-only editor.
	if _, err := s.SetFirewallTargetService(context.Background(), "composite", []string{"robot", "composite.example"}); err != nil {
		t.Fatal(err)
	}
	routingJob(t, h, "groups/edit", `{"name":"composite","domains":["new.example"]}`, "")
	group, _ := s.GetFirewallTargetService("composite")
	if len(group.Services) != 1 || group.Services[0] != "robot" {
		t.Fatal("composite membership lost")
	}
	s.groupErr = errors.New("synthetic-private-error")
	routingJob(t, h, "groups", `{"name":"partial","domains":["partial.example"]}`, "routing_group_saved_activation_failed")
	if _, err := s.GetFirewallTargetService("partial"); err != nil {
		t.Fatal("partial persistence should be accurately reported")
	}
}

func TestRoutingGeoAndErrorsFailClosed(t *testing.T) {
	h, s, fs := newRoutingTest(t)
	routingJob(t, h, "country", `{"country":"RU"}`, "")
	before, _ := fs.LoadSettings()
	s.geoErr = panelJobError("geo_runtime_unavailable")
	routingJob(t, h, "country", `{"enabled":true}`, "geo_runtime_unavailable")
	routingJob(t, h, "geo/update", `{}`, "geo_runtime_unavailable")
	after, _ := fs.LoadSettings()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("missing Geo runtime changed country settings")
	}
	s.geoErr = nil
	s.geo = routingGeoStatus{Ready: false, LastResult: "error"}
	routingJob(t, h, "country", `{"enabled":true}`, "geo_update_failed")
	s.geo = routingGeoStatus{Ready: true, LastResult: "ok"}
	s.patchErr = errors.New("synthetic-private-error")
	routingJob(t, h, "country", `{"enabled":true}`, "operation_failed")
	s.listErr = errors.New("synthetic-private-error")
	w := routingRequest(h, "GET", "state", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "routing_groups_unavailable") || strings.Contains(w.Body.String(), "synthetic-private-error") {
		t.Fatal("state failed to isolate/sanitize groups error")
	}
	if _, err := runRoutingGeoHelper(context.Background(), "status; arbitrary"); err == nil {
		t.Fatal("external action not allowlisted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.geo = routingGeoStatus{Updating: true, LastResult: "updating"}
	if err := awaitRoutingGeo(ctx, s, time.Millisecond); err == nil {
		t.Fatal("cancelled Geo wait succeeded")
	}
}

func TestRoutingRequestSecurityAndCoordinator(t *testing.T) {
	h, s, fs := newRoutingTest(t)
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "country", "", 405}, {"POST", "unknown", `{}`, 404},
		{"POST", "country", `{"enabled":"true"}`, 400}, {"POST", "country", `{"country":"ZZ"}`, 400},
		{"POST", "country", `{"country":"RU;id"}`, 400}, {"POST", "country", `{"country":"RU","shell":"id"}`, 400},
		{"POST", "country", `{} {}`, 400}, {"POST", "country", `null`, 400},
		{"POST", "groups", `{"name":"--help","domains":["example.com"]}`, 400},
		{"POST", "groups/toggle", `{"name":"robot"}`, 400}, {"POST", "groups/delete", `{"name":"robot","enabled":true}`, 400},
		{"POST", "geo/update", `{"url":"https://evil.example"}`, 400}, {"GET", "geo?command=id", "", 400},
		{"POST", "groups", `{"name":"x","domains":["` + strings.Repeat("x", maxRequestBodyBytes) + `"]}`, 400},
	} {
		w := routingRequest(h, tc.method, tc.path, tc.body)
		if w.Code != tc.status {
			t.Fatalf("%s %s: got %d want %d", tc.method, tc.path, w.Code, tc.status)
		}
	}
	for _, tc := range []struct {
		token, origin, site string
		code                int
	}{
		{"", "", "", 401}, {testAccessToken, "http://evil.example", "", 403}, {testAccessToken, "", "cross-site", 403},
	} {
		r := httptest.NewRequest("POST", "http://router.local/api/v1/routing/country", strings.NewReader(`{"country":"RU"}`))
		r.Header.Set("Authorization", "Bearer "+tc.token)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		h.routingPanelAPI(w, r)
		if w.Code != tc.code {
			t.Fatalf("security: %d", w.Code)
		}
	}
	settings, _ := fs.LoadSettings()
	if settings.CountryRouting.CountryCode != "" {
		t.Fatal("rejected requests changed settings")
	}
	block := make(chan struct{})
	s.block = block
	w := routingRequest(h, "POST", "country", `{"country":"RU"}`)
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	if other := routingRequest(h, "POST", "country", `{"country":"DE"}`); other.Code != 409 {
		t.Fatal("coordinator did not reject concurrent write")
	}
	close(block)
	deadline := time.Now().Add(time.Second)
	for h.jobs.snapshot().Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !h.jobs.snapshot().Succeeded {
		t.Fatal("accepted write did not complete")
	}
}

func TestRoutingHappPreviewNeverPartiallyApplies(t *testing.T) {
	h, _, fs := newRoutingTest(t)
	before, _ := fs.LoadSettings()
	profile := `{"Name":"Тест <img src=x>","DirectSites":["example.com"],"ProxyIp":["192.0.2.0/24"],"BlockSites":["blocked.example"],"UnknownFutureRules":["keep visible"]}`
	link := "happ://routing/onadd/" + base64.RawURLEncoding.EncodeToString([]byte(profile))
	body, _ := json.Marshal(map[string]string{"link": link})
	w := routingRequest(h, "POST", "happ/preview", string(body))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "UnknownFutureRules") {
		t.Fatal("valid profile not preserved in preview")
	}
	w = routingRequest(h, "POST", "happ/apply", string(body))
	if w.Code != 422 || !strings.Contains(w.Body.String(), "happ_atomic_apply_unavailable") {
		t.Fatal("partial HAPP import allowed")
	}
	for _, bad := range []string{"null", "[]", `{"DirectSites":"example.com"}`, `{"ProxyIp":[42]}`, `{"Name":[]}`} {
		if _, err := parseRoutingHapp("happ://routing/onadd/" + base64.StdEncoding.EncodeToString([]byte(bad))); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	if _, err := parseRoutingHapp("https://evil.example"); err == nil {
		t.Fatal("accepted non-HAPP URI")
	}
	after, _ := fs.LoadSettings()
	if !reflect.DeepEqual(before, after) || h.jobs.snapshot().Sequence != 0 {
		t.Fatal("preview or rejected apply mutated state")
	}
}

func TestRoutingGeneratedSource(t *testing.T) {
	cmd := exec.Command("node", "scripts/sync-routing-panel.cjs", "--check")
	cmd.Dir = "../.."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("routing source drift: %v %s", err, out)
	}
	data, err := panelFiles.ReadFile("web/routing-panel.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"eval(", "new Function(", "innerHTML", "fs.exec(", "window.confirm("} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("unsafe/legacy runtime: %s", forbidden)
		}
	}
}

// Local browser harness uses the real service with a disposable settings store.
// No router, Xray, secrets or persistent browser profile is involved.
func TestRoutingPanelBrowser(t *testing.T) {
	if os.Getenv("FASTLANE_ROUTING_BROWSER") != "1" {
		t.Skip("set FASTLANE_ROUTING_BROWSER=1")
	}
	h, _, _ := newRoutingTest(t)
	fixture := `<!doctype html><html lang="ru"><head><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/legacy.css"><link rel="stylesheet" href="/panel.css"><link rel="stylesheet" href="/routing-panel.css"><script defer src="/legacy-countries.js"></script><script defer src="/routing-panel.js"></script><script defer src="/fixture.js"></script></head><body class="fastlane-root"><main class="fl-shell"><div id="notice"></div><section id="routing"></section></main><dialog id="confirm" class="fl-dialog"><form method="dialog"><h4>Подтвердить действие</h4><div class="fl-dialog-form"><p id="confirm-text"></p></div><div class="fl-dialog-actions"><button value="cancel" class="fl-dialog-button">Отмена</button><button value="confirm" class="fl-dialog-button">Подтвердить</button></div></form></dialog></body></html>`
	fixtureJS := `let snapshot;window.notices=[];const api=async(path,method='GET',body)=>{const response=await fetch('/api/v1/'+path,{method,headers:{Authorization:'Bearer ` + testAccessToken + `','Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)});const value=await response.json();if(!response.ok)throw Error(value.error);return value;};window.fixtureAPI=api;window.fixturePanel=FastLaneRouting.mount(document.querySelector('#routing'),{api,operation:()=>{throw Error('acceptance is not completion');},notice:(message,error)=>{notices.push({message,error});document.querySelector('#notice').textContent=message;},getSnapshot:()=>snapshot,confirmAction:(message,action)=>{const d=document.querySelector('#confirm');document.querySelector('#confirm-text').textContent=message;d.addEventListener('close',()=>{if(d.returnValue==='confirm')action();},{once:true});d.showModal();}});setInterval(async()=>{snapshot=await api('state');FastLaneRouting.refresh(snapshot);},500);`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'")
		if h.routingPanelAPI(w, r) {
			return
		}
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(fixture))
			return
		}
		if r.URL.Path == "/fixture.js" {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write([]byte(fixtureJS))
			return
		}
		if r.URL.Path == "/routing-panel.js" || r.URL.Path == "/routing-panel.css" || r.URL.Path == "/legacy-countries.js" {
			mime := "text/javascript; charset=utf-8"
			if strings.HasSuffix(r.URL.Path, ".css") {
				mime = "text/css; charset=utf-8"
			}
			w.Header().Set("Content-Type", mime)
			data, err := panelFiles.ReadFile("web" + r.URL.Path)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(data)
			return
		}
		h.ServeHTTP(w, r)
	}))
	defer server.Close()
	alloc, cancel := chromedp.NewExecAllocator(context.Background(), chromedp.DefaultExecAllocatorOptions[:]...)
	defer cancel()
	ctx, cancel := chromedp.NewContext(alloc)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	check := func(expression string) {
		t.Helper()
		var valid bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &valid)); err != nil || !valid {
			var details string
			_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({count:document.querySelectorAll('.flr-select option').length,title:document.querySelector('.flr-head h1')?.textContent,notice:document.querySelector('#notice')?.textContent,preview:document.querySelector('.flr-preview')?.textContent})`, &details))
			t.Fatalf("browser assertion %s: %v %s", expression, err, details)
		}
	}
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1440, 1000), chromedp.Navigate(server.URL), chromedp.WaitVisible(".flr-bypass"), chromedp.WaitEnabled(".flr-bypass-head button")); err != nil {
		t.Fatal(err)
	}
	check(`document.querySelectorAll('.flr-select option').length===250 && document.querySelector('.flr-head h1').textContent==='Маршруты'`)
	if err := chromedp.Run(ctx, chromedp.Click(".flr-bypass-head button"), chromedp.WaitVisible("dialog.fastlane-routing-modal"), chromedp.SendKeys("dialog.fastlane-routing-modal input", "robot"), chromedp.SendKeys("dialog.fastlane-routing-modal textarea", "example.com"), chromedp.Sleep(1100*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	check(`document.querySelector('dialog.fastlane-routing-modal input').value==='robot' && document.querySelector('dialog.fastlane-routing-modal textarea').value==='example.com'`)
	// Force a real settings change/reload while the unsaved dialog is open.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`void fixtureAPI('routing/country','POST',{country:'DE'})`, nil), chromedp.Sleep(1200*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	check(`document.querySelector('.flr-select').value==='DE' && document.querySelector('dialog.fastlane-routing-modal input').value==='robot' && document.querySelector('dialog.fastlane-routing-modal textarea').value==='example.com'`)
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844)); err != nil {
		t.Fatal(err)
	}
	check(`(()=>{const rect=document.querySelector('dialog.fastlane-routing-modal').getBoundingClientRect();return rect.left>=0&&rect.right<=innerWidth&&rect.top>=0&&rect.bottom<=innerHeight&&document.documentElement.scrollWidth<=innerWidth;})()`)
	if err := chromedp.Run(ctx, chromedp.Click("dialog.fastlane-routing-modal .fl-dialog-primary"), chromedp.WaitVisible(".flr-rule-active"), chromedp.WaitNotPresent("dialog.fastlane-routing-modal")); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`.flr-rule-actions .flr-button:not(.flr-button-danger)`), chromedp.WaitVisible("dialog.fastlane-routing-modal"), chromedp.SetValue("dialog.fastlane-routing-modal textarea", "changed.example"), chromedp.Click("dialog.fastlane-routing-modal .fl-dialog-primary"), chromedp.WaitNotPresent("dialog.fastlane-routing-modal")); err != nil {
		t.Fatal(err)
	}
	check(`document.querySelector('.flr-rule-values').textContent.includes('changed.example')`)
	if err := chromedp.Run(ctx, chromedp.Click(".flr-rule-switch"), chromedp.WaitNotPresent(".flr-rule-active"), chromedp.WaitEnabled(".flr-bypass-head button")); err != nil {
		t.Fatal(err)
	}
	check(`document.querySelector('.flr-rule-title h3').textContent==='Robot'`)
	if err := chromedp.Run(ctx, chromedp.Reload(), chromedp.WaitVisible(".flr-rule"), chromedp.WaitEnabled(".flr-bypass-head button")); err != nil {
		t.Fatal(err)
	}
	check(`!document.querySelector('.flr-rule-active') && document.querySelector('.flr-rule-values').textContent.includes('changed.example')`)
	if err := chromedp.Run(ctx, chromedp.Click(".flr-advanced summary"), chromedp.SendKeys(".flr-import input", "happ://routing/onadd/"+base64.RawURLEncoding.EncodeToString([]byte(`{"Name":"<img src=x onerror=alert(1)>","DirectSites":["example.com"],"BlockSites":["blocked.example"]}`))), chromedp.Click(".flr-import button"), chromedp.WaitVisible(".flr-preview")); err != nil {
		t.Fatal(err)
	}
	check(`!document.querySelector('.flr-preview img') && document.querySelector('.flr-preview').textContent.includes('Частично применять')`)
	for _, width := range []int64{1440, 390, 320} {
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(width, 900)); err != nil {
			t.Fatal(err)
		}
		check(`document.documentElement.scrollWidth<=innerWidth`)
		if dir := os.Getenv("FASTLANE_PANEL_SCREENSHOTS"); dir != "" {
			var shot []byte
			if err := chromedp.Run(ctx, chromedp.FullScreenshot(&shot, 90)); err != nil {
				t.Fatal(err)
			}
			name := "routing-desktop.png"
			if width < 1000 {
				name = "routing-mobile.png"
			}
			if err := os.WriteFile(filepath.Join(dir, name), shot, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := chromedp.Run(ctx, chromedp.Click(".flr-rule .flr-button-danger"), chromedp.WaitVisible("#confirm"), chromedp.Click(`#confirm button[value="cancel"]`), chromedp.WaitNotPresent("#confirm[open]")); err != nil {
		t.Fatal(err)
	}
	check(`document.querySelectorAll('.flr-rule').length===1`)
	if err := chromedp.Run(ctx, chromedp.Click(".flr-rule .flr-button-danger"), chromedp.WaitVisible("#confirm"), chromedp.Click(`#confirm button[value="confirm"]`), chromedp.WaitNotPresent(".flr-rule")); err != nil {
		t.Fatal(err)
	}
}
