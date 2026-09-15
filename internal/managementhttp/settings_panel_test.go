package managementhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/design-maestro/fastlane/internal/app"
	"github.com/design-maestro/fastlane/internal/backend"
	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/store"
	"github.com/design-maestro/fastlane/internal/update"
)

type settingsPanelTestService struct {
	*app.Service
	host SettingsPanelHost
}

func (s settingsPanelTestService) SettingsPanelHost() SettingsPanelHost { return s.host }

func newSettingsPanelTest(t *testing.T, host SettingsPanelHost) (*Handler, *store.FileStore) {
	t.Helper()
	fs := store.NewFileStore(t.TempDir())
	s := settingsPanelTestService{app.NewService(app.Dependencies{Store: fs}), host}
	h := mustHandler(t, context.Background(), s, HandlerConfig{AccessToken: testAccessToken}).(*Handler)
	return h, fs
}

// Exercise the independently authenticated dispatcher even before root merges
// its ServeHTTP integration. The browser harness also exercises the outer shell.
func settingsPanelRequest(h *Handler, method, path, body, token, origin string) *httptest.ResponseRecorder {
	return request(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.settingsPanelAPI(w, r) {
			h.ServeHTTP(w, r)
		}
	}), method, path, body, token, origin)
}

func waitSettingsPanelJob(t *testing.T, h *Handler) jobSnapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for h.jobs.snapshot().Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	job := h.jobs.snapshot()
	if job.Running {
		t.Fatal("job never completed")
	}
	return job
}

func TestSettingsPanelPersistenceAndAtomicErrors(t *testing.T) {
	h, fs := newSettingsPanelTest(t, nil)
	post := func(path, method, body string) jobSnapshot {
		t.Helper()
		w := settingsPanelRequest(h, method, path, body, testAccessToken, "")
		if w.Code != 202 {
			t.Fatalf("admission %d: %s", w.Code, w.Body.String())
		}
		return waitSettingsPanelJob(t, h)
	}
	job := post("/api/v1/settings-panel", "PATCH", `{"refresh_interval":"2h3m4s","health_check_interval":"0m0s","url_test_timeout":"7s","switch_cooldown":"1m30s","latency_threshold":"125ms","strict_egress_check":false,"url_test_url":"https://example.com/204"}`)
	if !job.Succeeded {
		t.Fatal(job)
	}
	saved, err := fs.LoadSettings()
	if err != nil || saved.RefreshInterval.Duration() != 2*time.Hour+3*time.Minute+4*time.Second || saved.StrictEgressCheck || saved.LatencyThreshold.Duration() != 125*time.Millisecond || saved.HealthCheckInterval.Duration() != 0 {
		t.Fatalf("not persisted: %+v %v", saved, err)
	}
	job = post("/api/v1/settings-panel", "PATCH", `{"refresh_interval":"9h","url_test_url":"http://user:DO_NOT_EXPOSE@example.com"}`)
	if job.Succeeded || strings.Contains(job.Error, "DO_NOT_EXPOSE") {
		t.Fatal("failed validation reported success or leaked payload")
	}
	after, _ := fs.LoadSettings()
	if after.RefreshInterval != saved.RefreshInterval {
		t.Fatal("partial patch persisted")
	}
	job = post("/api/v1/settings-panel/hide-keywords", "POST", `{"keywords":[" LTE ","lte","Россия"]}`)
	if !job.Succeeded {
		t.Fatal(job)
	}
	after, _ = fs.LoadSettings()
	if len(after.AutoHideKeywords) != 2 || after.AutoHideKeywords[0] != "LTE" {
		t.Fatalf("keyword canonicalization: %v", after.AutoHideKeywords)
	}
	job = post("/api/v1/settings-panel/hide-keywords", "POST", `{"keywords":[]}`)
	if !job.Succeeded {
		t.Fatal(job)
	}
	after, _ = fs.LoadSettings()
	if len(after.AutoHideKeywords) != 0 {
		t.Fatal("keyword deletion not persisted")
	}
	after.DNS.Bootstrap = []string{"192.0.2.53"}
	after.DNS.DirectDomains = []string{"domain:lan"}
	if err := fs.SaveSettings(after); err != nil {
		t.Fatal(err)
	}
	job = post("/api/v1/settings-panel/dns", "POST", `{"mode":"split","transport":"doh","servers":["https://dns.example/dns-query"]}`)
	if !job.Succeeded {
		t.Fatal(job)
	}
	after, _ = fs.LoadSettings()
	if after.DNS.Bootstrap[0] != "192.0.2.53" || after.DNS.DirectDomains[0] != "domain:lan" {
		t.Fatal("DNS lost omitted advanced settings")
	}
	job = post("/api/v1/settings-panel/dns", "POST", `{"mode":"split","transport":"doh","servers":["https://dns.example/dns-query"],"bootstrap":[],"direct_domains":[]}`)
	if !job.Succeeded {
		t.Fatal(job)
	}
	after, _ = fs.LoadSettings()
	if len(after.DNS.Bootstrap) != 0 || len(after.DNS.DirectDomains) != 0 {
		t.Fatal("explicit DNS clearing ignored")
	}
}

func TestSettingsPanelSecurityAndUnsupportedRuntime(t *testing.T) {
	h, _ := newSettingsPanelTest(t, nil)
	for _, path := range []string{"/api/v1/settings-panel", "/api/v1/settings-panel/update", "/api/v1/diagnostics-panel"} {
		if w := settingsPanelRequest(h, "GET", path, "", "", ""); w.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", path, w.Code)
		}
		if w := settingsPanelRequest(h, "GET", path, "", testAccessToken, "https://evil.example"); w.Code != 403 {
			t.Fatal("cross-origin request accepted")
		}
	}
	for _, test := range []struct {
		method, path, body string
		code               int
	}{
		{"PATCH", "/api/v1/settings-panel", `{"unknown":"secret"}`, 400},
		{"PATCH", "/api/v1/settings-panel", `{"strict_egress_check":"true"}`, 400},
		{"PATCH", "/api/v1/settings-panel", `{} {}`, 400},
		{"PATCH", "/api/v1/settings-panel", `null`, 400},
		{"POST", "/api/v1/settings-panel/hide-keywords", `{"keywords":["one,two"]}`, 400},
		{"POST", "/api/v1/settings-panel/hide-keywords", `{}`, 400},
		{"POST", "/api/v1/settings-panel/dns", `{"mode":"anything","transport":"doh","servers":[]}`, 400},
		{"POST", "/api/v1/settings-panel/dns", `{"mode":"split","transport":"dot","servers":[]}`, 400},
		{"POST", "/api/v1/settings-panel/update/check", `{}`, 501},
		{"POST", "/api/v1/settings-panel/update/install", `{"release_id":42,"confirm":true}`, 501},
		{"POST", "/api/v1/settings-panel/uninstall", `{"confirm":true}`, 501},
		{"DELETE", "/api/v1/settings-panel", "", 405},
		{"POST", "/api/v1/diagnostics-panel", `{}`, 405},
	} {
		w := settingsPanelRequest(h, test.method, test.path, test.body, testAccessToken, "")
		if w.Code != test.code {
			t.Fatalf("%s %s: %d %s", test.method, test.path, w.Code, w.Body.String())
		}
	}
	if h.jobs.snapshot().Sequence != 0 {
		t.Fatal("invalid/unsupported requests started jobs")
	}
	w := settingsPanelRequest(h, "GET", "/api/v1/diagnostics-panel", "", testAccessToken, "")
	var result diagnosticsPanelResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"runtime", "dns", "ipv6", "files"} {
		if result.Unavailable[key] == "" {
			t.Fatalf("missing unavailable %s", key)
		}
	}
	if result.Runtime.Running || result.DNS.Active {
		t.Fatal("absent runtime reported healthy")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private response cacheable")
	}
}

type settingsPanelFakeHost struct {
	mu      sync.Mutex
	state   update.State
	paths   map[string]string
	started chan struct{}
	release chan struct{}
	err     error
	runs    int
}

func (f *settingsPanelFakeHost) Capabilities() SettingsPanelHostCapabilities {
	return SettingsPanelHostCapabilities{true, true, true}
}
func (f *settingsPanelFakeHost) Paths() map[string]string { return f.paths }
func (f *settingsPanelFakeHost) UpdateStatus(context.Context) (update.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, nil
}
func (f *settingsPanelFakeHost) Uninstall(context.Context) error {
	return errors.New("test uninstall error DO_NOT_EXPOSE")
}
func (f *settingsPanelFakeHost) RunUpdate(ctx context.Context, operation string, id int64) error {
	f.mu.Lock()
	f.runs++
	f.mu.Unlock()
	if f.started != nil {
		close(f.started)
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.err
}

func TestSettingsPanelHostCoordinatorAndMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("DO_NOT_EXPOSE_FILE_CONTENT"), 0600); err != nil {
		t.Fatal(err)
	}
	host := &settingsPanelFakeHost{state: update.State{Status: "available", Current: "1.0.0", Token: "DO_NOT_EXPOSE_TOKEN", PID: 1234, Candidate: &update.Candidate{ID: 42, Version: "1.1.0", Page: "https://github.com/design-maestro/fastlane/releases/tag/v1.1.0"}}, paths: map[string]string{"settings_file": path}, started: make(chan struct{}), release: make(chan struct{})}
	h, _ := newSettingsPanelTest(t, host)
	w := settingsPanelRequest(h, "GET", "/api/v1/settings-panel/update", "", testAccessToken, "")
	if strings.Contains(w.Body.String(), "DO_NOT_EXPOSE") || strings.Contains(w.Body.String(), `"pid"`) || !strings.Contains(w.Body.String(), `"id":42`) {
		t.Fatalf("unsafe/incomplete update: %s", w.Body.String())
	}
	w = settingsPanelRequest(h, "GET", "/api/v1/diagnostics-panel", "", testAccessToken, "")
	if strings.Contains(w.Body.String(), "DO_NOT_EXPOSE") || !strings.Contains(w.Body.String(), `"exists":true`) || strings.Contains(w.Body.String(), "file_inspection_adapter_unavailable") {
		t.Fatalf("metadata: %s", w.Body.String())
	}
	if w = settingsPanelRequest(h, "POST", "/api/v1/settings-panel/update/install", `{"release_id":42}`, testAccessToken, ""); w.Code != 400 {
		t.Fatal("install without explicit confirmation")
	}
	w = settingsPanelRequest(h, "POST", "/api/v1/settings-panel/update/install", `{"release_id":42,"confirm":true}`, testAccessToken, "")
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	select {
	case <-host.started:
	case <-time.After(time.Second):
		t.Fatal("helper never started")
	}
	if job := h.jobs.snapshot(); !job.Running || job.Succeeded {
		t.Fatal("admission claimed completion")
	}
	w = settingsPanelRequest(h, "PATCH", "/api/v1/settings-panel", `{"refresh_interval":"1h"}`, testAccessToken, "")
	if w.Code != 409 {
		t.Fatal("settings overlapped update coordinator")
	}
	close(host.release)
	if job := waitSettingsPanelJob(t, h); !job.Succeeded {
		t.Fatal(job)
	}
	w = settingsPanelRequest(h, "POST", "/api/v1/settings-panel/update/install", `{"release_id":43,"confirm":true}`, testAccessToken, "")
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	if job := waitSettingsPanelJob(t, h); job.Succeeded || job.Error != "release_changed" {
		t.Fatal("stale release accepted", job)
	}
	host.mu.Lock()
	runs := host.runs
	host.mu.Unlock()
	if runs != 1 {
		t.Fatal("stale release executed helper")
	}
	w = settingsPanelRequest(h, "POST", "/api/v1/settings-panel/uninstall", `{"confirm":true}`, testAccessToken, "")
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	if job := waitSettingsPanelJob(t, h); job.Succeeded || strings.Contains(job.Error, "DO_NOT_EXPOSE") {
		t.Fatal("failed uninstall misreported", job)
	}
}

type settingsPanelRuntimeError struct{ settingsPanelTestService }

func (settingsPanelRuntimeError) RuntimeStatus(context.Context) (backend.RuntimeStatus, error) {
	return backend.RuntimeStatus{Running: true}, errors.New("DO_NOT_EXPOSE_RUNTIME")
}
func (settingsPanelRuntimeError) DNSStatus(context.Context) (domain.DNSRuntimeStatus, error) {
	return domain.DNSRuntimeStatus{Active: true}, errors.New("DO_NOT_EXPOSE_DNS")
}
func (settingsPanelRuntimeError) IPv6Status(context.Context) (domain.IPv6Status, error) {
	return domain.IPv6Status{}, errors.New("DO_NOT_EXPOSE_IPV6")
}

func TestSettingsPanelRuntimeErrorsAreNotHealthy(t *testing.T) {
	h, _ := newSettingsPanelTest(t, nil)
	h.service = settingsPanelRuntimeError{h.service.(settingsPanelTestService)}
	w := settingsPanelRequest(h, "GET", "/api/v1/diagnostics-panel", "", testAccessToken, "")
	if strings.Contains(w.Body.String(), "DO_NOT_EXPOSE") || strings.Contains(w.Body.String(), `"running":true`) || !strings.Contains(w.Body.String(), "runtime_status_failed") {
		t.Fatalf("runtime error contract: %s", w.Body.String())
	}
}

func TestSettingsPanelAssets(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node required")
	}
	for _, args := range [][]string{{"scripts/sync-settings-panel.cjs", "--check"}, {"--check", "internal/managementhttp/web/settings-panel.js"}, {"--check", "internal/managementhttp/web/diagnostics-panel.js"}} {
		cmd := exec.Command("node", args...)
		cmd.Dir = "../.."
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	for _, name := range []string{"settings-panel.js", "diagnostics-panel.js"} {
		data, err := panelFiles.ReadFile("web/" + name)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"eval(", "new Function(", "innerHTML", "E('style'"} {
			if strings.Contains(string(data), bad) {
				t.Fatalf("%s contains forbidden %s", name, bad)
			}
		}
	}
}

func TestSettingsPanelDetachedUninstallChild(t *testing.T) {
	if os.Getenv("FASTLANE_TEST_UNINSTALL_CHILD") != "1" {
		return
	}
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(os.Getenv("FASTLANE_TEST_UNINSTALL_RECEIPT"), []byte("finished"), 0600); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestSettingsPanelUninstallSurvivesParentCancellation(t *testing.T) {
	receipt := filepath.Join(t.TempDir(), "test-completion")
	command := exec.Command(os.Args[0], "-test.run=^TestSettingsPanelDetachedUninstallChild$")
	command.Env = append(os.Environ(), "FASTLANE_TEST_UNINSTALL_CHILD=1", "FASTLANE_TEST_UNINSTALL_RECEIPT="+receipt)
	ctx, cancel := context.WithCancel(context.Background())
	err := startPanelUninstall(ctx, command)
	cancel()
	if err != panelJobError("uninstall_completion_unconfirmed") {
		t.Fatalf("spawn claimed completion: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(receipt); err == nil && string(data) == "finished" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("daemon context cancellation killed detached cleanup")
}

func TestSettingsPanelUpdateCommandContract(t *testing.T) {
	for _, test := range []struct {
		operation, status string
		id, candidate     int64
		wantError         bool
	}{
		{"check", "available", 0, 42, false}, {"check", "current", 0, 42, false},
		{"check", "error", 0, 0, true}, {"install", "updated", 42, 42, false},
		{"install", "installing", 42, 42, true}, {"install", "updated", 42, 43, true},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		err := runPanelUpdate(ctx, test.operation, test.id, func(_ context.Context, args ...string) (update.State, error) {
			calls++
			want := test.operation
			if test.operation == "install" {
				want += " --release 42"
			}
			if strings.Join(args, " ") != want {
				t.Fatalf("wrong installed CLI flags: %v", args)
			}
			if test.status == "installing" {
				cancel()
			}
			return update.State{Status: test.status, Candidate: &update.Candidate{ID: test.candidate}}, nil
		})
		cancel()
		if calls != 1 || (err != nil) != test.wantError {
			t.Fatalf("%+v: calls=%d err=%v", test, calls, err)
		}
	}
	host := openWrtPanelHost{rootDir: filepath.Join(t.TempDir(), "configured-root"), configPath: filepath.Join(t.TempDir(), "configured-xray.json")}
	paths := host.Paths()
	if paths["fastlane_root"] != host.rootDir || paths["xray_config"] != host.configPath {
		t.Fatal("configured diagnostics paths ignored")
	}
	if !strings.Contains(strings.Join(host.environment(), "\n"), "FASTLANE_ROOT="+host.rootDir) {
		t.Fatal("helper used another root")
	}
}

// Local isolated browser fixture. No router, helper, real configuration or
// secrets are accessed. Serve the exact module assets under the production CSP.
func TestSettingsPanelBrowser(t *testing.T) {
	if os.Getenv("FASTLANE_SETTINGS_BROWSER") != "1" {
		t.Skip("set FASTLANE_SETTINGS_BROWSER=1")
	}
	h, _ := newSettingsPanelTest(t, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/module-test":
			w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'")
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/legacy.css"><link rel="stylesheet" href="/settings-panel.css"><link rel="stylesheet" href="/diagnostics-panel.css"><script defer src="/settings-panel.js"></script><script defer src="/diagnostics-panel.js"></script><script defer src="/module-boot.js"></script></head><body class="fastlane-root"><main class="fl-shell"><div id="settings"></div><div id="diagnostics"></div><p id="notice"></p></main></body></html>`))
		case "/module-boot.js":
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write([]byte(`(async()=>{let snapshot;const api=async(path,method='GET',body)=>{const r=await fetch('/api/v1/'+path,{method,headers:{'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)});const value=await r.json();if(!r.ok)throw Error(value.error);return value;};await api('session','POST',{token:'` + testAccessToken + `'});snapshot=await api('state');const shared={api,getSnapshot:()=>snapshot,notice:(text)=>document.querySelector('#notice').textContent=text,confirmAction:(message,action)=>{window.testConfirmation={message,action};},operation:async(path,method='POST',body)=>{try{await api(path,method,body);snapshot=await api('state');return true;}catch(_){return false;}}};window.settingsTest=FastLaneSettings.mount(document.querySelector('#settings'),shared);window.diagnosticsTest=FastLaneDiagnostics.mount(document.querySelector('#diagnostics'),shared);await Promise.all([settingsTest.ready,diagnosticsTest.ready]);window.testReady=true;setInterval(async()=>{snapshot=await api('state');settingsTest.refresh(snapshot);diagnosticsTest.refresh(snapshot);},100);})()`))
		default:
			if strings.HasSuffix(r.URL.Path, "-panel.js") || strings.HasSuffix(r.URL.Path, "-panel.css") {
				data, err := panelFiles.ReadFile("web" + r.URL.Path)
				if err != nil {
					http.NotFound(w, r)
					return
				}
				if strings.HasSuffix(r.URL.Path, ".js") {
					w.Header().Set("Content-Type", "text/javascript")
				} else {
					w.Header().Set("Content-Type", "text/css")
				}
				_, _ = w.Write(data)
				return
			}
			if !h.settingsPanelAPI(w, r) {
				h.ServeHTTP(w, r)
			}
		}
	}))
	defer server.Close()
	opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("no-sandbox", true))
	alloc, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()
	ctx, cancel := chromedp.NewContext(alloc)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var ok bool
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1440, 1000), chromedp.Navigate(server.URL+"/module-test"), chromedp.WaitVisible(".fls-keyword-control"), chromedp.WaitVisible(".fld-overview")); err != nil {
		t.Fatal(err)
	}
	check := func(js string) {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &ok)); err != nil || !ok {
			t.Fatalf("browser assertion: %s: %v", js, err)
		}
	}
	check(`document.querySelectorAll('[data-duration-unit]').length===9 && document.querySelectorAll('.fld-row').length===5 && document.querySelectorAll('.fld-tech-row').length===6 && document.querySelector('.fls-update button').disabled`)
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(()=>{const i=document.querySelector('[data-setting-key="url_test_url"]');i.value='https://example.com/unsaved';i.dispatchEvent(new Event('input',{bubbles:true}));i.focus();})()`, nil), chromedp.Sleep(350*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	check(`document.activeElement.value==='https://example.com/unsaved'`)
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(()=>{const i=document.querySelector('.fls-keyword-control input');i.value='LTE';i.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true}));})()`, nil), chromedp.WaitVisible(".fls-chip"), chromedp.Sleep(350*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	check(`document.querySelector('[data-setting-key="url_test_url"]').value==='https://example.com/unsaved' && document.querySelectorAll('.fls-chip').length===1`)
	if err := chromedp.Run(ctx, chromedp.Click(".fls-head .fls-primary"), chromedp.Sleep(350*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	check(`document.querySelector('#notice').textContent.includes('saved') || document.querySelector('#notice').textContent.includes('сохранены')`)
	if err := chromedp.Run(ctx, chromedp.Click(".fld-advanced summary"), chromedp.Sleep(350*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	check(`document.querySelector('.fld-advanced').open`)
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.lifecycleDone=false;window.lifecycleError='';(async()=>{
  const base=await (await fetch('/api/v1/settings-panel')).json();
  for(const outcome of ['updated','wrong-version','error','uninstall']) {
    let snapshot={status:{settings:base.settings},job:{sequence:7,running:false}};
    let state={status:'available',current_version:'1.0.0',candidate:{id:42,version:'1.1.0'}};
    let confirmed,notice='';
    const target=document.createElement('div');document.body.append(target);
    const shared={getSnapshot:()=>snapshot,notice:text=>{notice=text;},confirmAction:(_,action)=>{confirmed=action;},
      api:async path=>path==='settings-panel/update'?state:{...base,update:state,unavailable:{}},
      operation:async()=>{snapshot={...snapshot,job:{sequence:0,running:false}};state={...state,status:outcome==='wrong-version'?'updated':outcome,candidate:{id:42,version:outcome==='wrong-version'?'9.9.9':'1.1.0'}};return true;}};
    const controller=FastLaneSettings.mount(target,shared);await controller.ready;
    target.querySelector(outcome==='uninstall'?'.fls-danger':'.fls-update .fls-primary').click();
    if(!confirmed)throw Error('No confirmation');
    const action=confirmed();await new Promise(resolve=>setTimeout(resolve,0));
    await controller.refresh(snapshot);
    await Promise.race([action,new Promise((_,reject)=>setTimeout(()=>reject(Error('stuck '+outcome)),1000))]);
    if(target.querySelector('.fls-head .fls-primary')?.disabled)throw Error('saving stuck '+outcome);
    if(outcome!=='updated' && !notice)throw Error('unconfirmed result not reported');
    target.remove();
  }
  window.lifecycleDone=true;
})().catch(error=>{window.lifecycleError=error.message;window.lifecycleDone=true;});`, nil), chromedp.Poll(`window.lifecycleDone`, nil)); err != nil {
		t.Fatal(err)
	}
	var lifecycleError string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.lifecycleError`, &lifecycleError)); err != nil || lifecycleError != "" {
		t.Fatalf("lifecycle: %s %v", lifecycleError, err)
	}
	// A language change with dirty settings must wait for the root confirmation.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(()=>{window.testConfirmation=null;const input=document.querySelector('[data-setting-key="url_test_url"]');input.value='https://example.com/dirty';input.dispatchEvent(new Event('input'));const select=document.querySelector('.fls-select');window.previousLanguage=localStorage.getItem('fastlane.panel.language');select.value='ru';select.dispatchEvent(new Event('change'));})()`, nil)); err != nil {
		t.Fatal(err)
	}
	check(`!!window.testConfirmation && localStorage.getItem('fastlane.panel.language')===window.previousLanguage`)
	for _, width := range []int64{1440, 1000, 850, 760, 390, 320} {
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(width, 900)); err != nil {
			t.Fatal(err)
		}
		var overflow string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({width:innerWidth,scroll:document.documentElement.scrollWidth,elements:[...document.querySelectorAll('body *')].filter(e=>e.offsetWidth && e.scrollWidth>e.clientWidth).map(e=>({tag:e.tagName,cls:e.className,scroll:e.scrollWidth,client:e.clientWidth,right:e.getBoundingClientRect().right})).slice(0,12)})`, &overflow)); err != nil {
			t.Fatal(err)
		}
		t.Logf("width %d overflow %s", width, overflow)
		check(`document.documentElement.scrollWidth<=innerWidth`)
	}
}
