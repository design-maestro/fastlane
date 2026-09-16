package managementhttp

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/design-maestro/fastlane/internal/app"
	"github.com/design-maestro/fastlane/internal/store"
)

// Run with FASTLANE_PANEL_BROWSER=1; the real service uses a temporary store,
// without an Xray or router controller. It cannot change host networking.
func TestStandalonePanelBrowser(t *testing.T) {
	if os.Getenv("FASTLANE_PANEL_BROWSER") != "1" {
		t.Skip("set FASTLANE_PANEL_BROWSER=1")
	}
	if dir := os.Getenv("FASTLANE_PANEL_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	fs := store.NewFileStore(t.TempDir())
	s := app.NewService(app.Dependencies{Store: fs, AWGStore: fs})
	h := mustHandler(t, context.Background(), s, HandlerConfig{AccessToken: testAccessToken})
	server := httptest.NewServer(h)
	defer server.Close()
	opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("no-sandbox", true))
	alloc, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()
	ctx, cancel := chromedp.NewContext(alloc)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1440, 1000), chromedp.Navigate(server.URL), chromedp.WaitVisible("#login"), chromedp.SendKeys("#login-form input", testAccessToken), chromedp.Click("#login-form button"), chromedp.WaitVisible("#application"), chromedp.Click("#add-toggle"), chromedp.SendKeys("#add-form input[name=name]", "Тестовый источник"), chromedp.SendKeys("#add-form textarea", "vless://11111111-1111-4111-8111-111111111111@vpn.example:443?security=tls#Test%20server"), chromedp.Click("#add-submit"), chromedp.WaitVisible("#server-list tbody tr"), chromedp.WaitNotPresent("#add-dialog[open]")); err != nil {
		t.Fatal(err)
	}
	var valid bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('#server-list').textContent.includes('Test server')`, &valid)); err != nil || !valid {
		t.Fatalf("import failed: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.Focus("#server-list button"), chromedp.Sleep(3500*time.Millisecond), chromedp.Evaluate(`document.activeElement === document.querySelector('#server-list button')`, &valid)); err != nil || !valid {
		t.Fatalf("poll lost keyboard focus: %v", err)
	}
	profilePath := filepath.Join(t.TempDir(), "AWG test.conf")
	if err := os.WriteFile(profilePath, []byte(panelAWGFixture()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Click("#add-toggle"), chromedp.Click("#add-file-tab"), chromedp.SetUploadFiles(`#add-form input[type=file]`, []string{profilePath}), chromedp.Click("#add-submit"), chromedp.WaitNotPresent("#add-dialog[open]"), chromedp.WaitVisible(`tr[data-key^="server-list/awg-"]`), chromedp.Evaluate(`document.querySelectorAll('#server-list tbody tr').length===2 && !document.querySelector('.awg') && document.querySelector('tr[data-key^="server-list/awg-"]').textContent.includes('AWG test')`, &valid)); err != nil || !valid {
		t.Fatalf("AWG unified import: %v", err)
	}
	badPath := filepath.Join(t.TempDir(), "Bad.conf")
	profiles, err := fs.ListAWGProfiles()
	if err != nil || len(profiles) != 1 {
		t.Fatalf("profiles after import = %+v, %v", profiles, err)
	}
	beforeReplacement, err := fs.LoadAWGProfile(profiles[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badPath, []byte("[Interface]\nPostUp = forbidden\n[Peer]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Click("#add-toggle"), chromedp.Click("#add-file-tab"), chromedp.SetUploadFiles(`#add-form input[type=file]`, []string{badPath}), chromedp.Click("#add-submit"), chromedp.Sleep(3500*time.Millisecond), chromedp.Evaluate(`document.querySelector('#add-error').textContent.includes('команды') && document.querySelector('#add-dialog').open`, &valid)); err != nil || !valid {
		t.Fatalf("unsafe AWG import did not leave actionable error: %v", err)
	}
	stored, err := fs.LoadAWGProfile(profiles[0].ID)
	if err != nil || string(stored) != string(beforeReplacement) {
		t.Fatal("failed replacement overwrote existing AWG profile")
	}
	if dir := os.Getenv("FASTLANE_PANEL_SCREENSHOTS"); dir != "" {
		var shot []byte
		if err := chromedp.Run(ctx, chromedp.FullScreenshot(&shot, 90)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "import-error.png"), shot, 0600); err != nil {
			t.Fatal(err)
		}
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(390, 844), chromedp.Evaluate(`(()=>{const r=document.querySelector('#add-dialog').getBoundingClientRect();return r.x>=0 && r.right<=innerWidth && r.y>=0 && r.bottom<=innerHeight;})()`, &valid), chromedp.FullScreenshot(&shot, 90)); err != nil || !valid {
			t.Fatalf("mobile import dialog outside viewport: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "import-error-mobile.png"), shot, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := chromedp.Run(ctx, chromedp.Click("#add-cancel")); err != nil {
		t.Fatal(err)
	}
	// Manual hide/restore must use the real shared settings, and the original
	// hidden-source tab must remain reachable without a separate checkbox.
	if err := chromedp.Run(ctx, chromedp.Click(`tr:not([data-key="local-awg/awg-profile"]) .fl-more-toggle`), chromedp.Click(`tr:not([data-key="local-awg/awg-profile"]) [data-action=hide]`), chromedp.WaitVisible(`[data-source=hidden]`), chromedp.Click(`[data-source=hidden]`), chromedp.WaitVisible(`.fl-hidden-row`), chromedp.Click(`.fl-hidden-row .fl-more-toggle`), chromedp.Click(`.fl-hidden-row [data-action=hide]`), chromedp.WaitNotPresent(`[data-source=hidden]`)); err != nil {
		t.Fatal("hide/restore browser flow: ", err)
	}
	for _, viewport := range []struct {
		name          string
		width, height int64
	}{{"desktop", 1440, 1000}, {"panel", 1024, 900}, {"tablet", 850, 1000}, {"landscape", 768, 600}, {"small-mobile", 320, 740}, {"mobile", 390, 844}} {
		var shot []byte
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(viewport.width, viewport.height), chromedp.Evaluate(`document.documentElement.scrollWidth <= innerWidth`, &valid), chromedp.FullScreenshot(&shot, 90)); err != nil {
			t.Fatal(err)
		}
		if !valid {
			t.Fatalf("horizontal overflow at %s", viewport.name)
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(()=>{
		  const controls=[...document.querySelectorAll('.fl-toolbar input:not([type=checkbox]),.fl-toolbar select,.fl-toolbar button,.fl-more-toggle')];
		  const rects=controls.map(e=>e.getBoundingClientRect());
		  if(rects.some(r=>r.width<44 || r.height<44 || r.left<0 || r.right>innerWidth)) return false;
		  return [...document.querySelectorAll('.fl-table .fl-meta-cell:not([hidden])')].every(e=>e.scrollWidth<=e.clientWidth+1);
		})()`, &valid)); err != nil || !valid {
			t.Fatalf("cramped controls or server metadata at %s: %v", viewport.name, err)
		}
		if dir := os.Getenv("FASTLANE_PANEL_SCREENSHOTS"); dir != "" {
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, viewport.name+".png"), shot, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := chromedp.Run(ctx, chromedp.Click(`tr[data-key="local-awg/awg-profile"] .fl-more-toggle`), chromedp.Click(`tr[data-key="local-awg/awg-profile"] [data-action=check]`), chromedp.Sleep(400*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if job := h.(*Handler).jobs.snapshot(); job.Kind != "awg-check" || job.Succeeded {
		t.Fatalf("AWG check must use its own adapter and fail without a controller: %+v", job)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`tr[data-key="local-awg/awg-profile"] .fl-more-toggle`), chromedp.Click(`tr[data-key="local-awg/awg-profile"] [data-action=connect]`), chromedp.Sleep(400*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if job := h.(*Handler).jobs.snapshot(); job.Kind != "awg-connect" || job.Succeeded {
		t.Fatalf("AWG connect routed incorrectly: %+v", job)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`tr[data-key="local-awg/awg-profile"] .fl-more-toggle`), chromedp.Click(`tr[data-key="local-awg/awg-profile"] .fl-button-danger`), chromedp.WaitVisible("#confirm"), chromedp.Click(`#confirm button[value=confirm]`), chromedp.WaitNotPresent(`tr[data-key="local-awg/awg-profile"]`), chromedp.Evaluate(`document.querySelectorAll('#server-list tbody tr').length===1`, &valid)); err != nil || !valid {
		t.Fatalf("AWG removal affected ordinary subscription: %v", err)
	}
	for _, page := range []string{"routing", "diagnostics", "settings"} {
		root := map[string]string{"routing": ".flr-control", "diagnostics": ".fld-overview", "settings": ".fastlane-settings"}[page]
		if err := chromedp.Run(ctx, chromedp.Navigate(server.URL+"/#"+page), chromedp.WaitVisible(root), chromedp.Evaluate(`document.documentElement.scrollWidth <= innerWidth`, &valid)); err != nil || !valid {
			t.Fatalf("mobile page %s failed or overflowed: %v", page, err)
		}
		for _, width := range []int64{390, 850, 1440} {
			var shot []byte
			if err := chromedp.Run(ctx, chromedp.EmulateViewport(width, 1000), chromedp.Evaluate(`window.scrollTo(0,0);document.documentElement.scrollWidth <= innerWidth`, &valid), chromedp.FullScreenshot(&shot, 90)); err != nil || !valid {
				t.Fatalf("page %s at %d: %v", page, width, err)
			}
			if dir := os.Getenv("FASTLANE_PANEL_SCREENSHOTS"); dir != "" {
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%d.png", page, width)), shot, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if err := chromedp.Run(ctx, chromedp.Click(`nav a[href="#settings"]`), chromedp.SetValue(`[data-setting-key="refresh_interval"][data-duration-unit="h"]`, "7"), chromedp.Evaluate(`document.querySelector('[data-setting-key="refresh_interval"][data-duration-unit="h"]').dispatchEvent(new Event('input',{bubbles:true}))`, nil), chromedp.Sleep(3500*time.Millisecond), chromedp.Evaluate(`document.querySelector('[data-setting-key="refresh_interval"][data-duration-unit="h"]').value==='7'`, &valid)); err != nil || !valid {
		t.Fatalf("poll overwrote form: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.Click(".fls-head .fls-primary"), chromedp.Sleep(3500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	saved, err := fs.LoadSettings()
	if err != nil || saved.RefreshInterval.Duration() != 7*time.Hour {
		t.Fatalf("settings screen did not persist duration: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`localStorage.setItem('fastlane.panel.language','en')`, nil), chromedp.Navigate(server.URL+"/#vpn"), chromedp.Reload(), chromedp.WaitVisible("#server-list tbody tr"), chromedp.Evaluate(`document.querySelector('nav a[href="#routing"]').textContent.trim()==='Routing' && document.querySelector('#add-toggle').textContent.trim()==='Add servers' && document.documentElement.lang==='en'`, &valid)); err != nil || !valid {
		t.Fatalf("full-panel language: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.Click("#logout"), chromedp.WaitVisible("#login")); err != nil {
		t.Fatal(err)
	}
}

// Synthetic keys and reserved documentation endpoint; never a user profile.
func panelAWGFixture() string {
	private, public := make([]byte, 32), make([]byte, 32)
	for i := range private {
		private[i] = byte(i + 1)
		public[i] = byte(i + 33)
	}
	return fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = 10.8.0.2/32\nJc = 4\nJmin = 20\nJmax = 100\nS1 = 10\nS2 = 20\nS3 = 30\nS4 = 40\nH1 = 100\nH2 = 201\nH3 = 202\nH4 = 203\n[Peer]\nPublicKey = %s\nAllowedIPs = 0.0.0.0/0\nEndpoint = vpn.example:51820\n", base64.StdEncoding.EncodeToString(private), base64.StdEncoding.EncodeToString(public))
}
