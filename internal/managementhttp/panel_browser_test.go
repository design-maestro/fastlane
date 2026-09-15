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
	if err := chromedp.Run(ctx, chromedp.Click("#add-toggle"), chromedp.Click("#add-file-tab"), chromedp.SetUploadFiles(`#add-form input[type=file]`, []string{profilePath}), chromedp.Click("#add-submit"), chromedp.WaitNotPresent("#add-dialog[open]"), chromedp.WaitVisible(`tr[data-key="local-awg/awg-profile"]`), chromedp.Evaluate(`document.querySelectorAll('#server-list tbody tr').length===2 && !document.querySelector('.awg') && document.querySelector('tr[data-key="local-awg/awg-profile"]').textContent.includes('AWG test')`, &valid)); err != nil || !valid {
		t.Fatalf("AWG unified import: %v", err)
	}
	badPath := filepath.Join(t.TempDir(), "Bad.conf")
	beforeReplacement, err := fs.LoadAWGProfile()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badPath, []byte("[Interface]\nPostUp = forbidden\n[Peer]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Click("#add-toggle"), chromedp.Click("#add-file-tab"), chromedp.SetUploadFiles(`#add-form input[type=file]`, []string{badPath}), chromedp.Click("#add-submit"), chromedp.WaitVisible("#confirm"), chromedp.Click(`#confirm button[value=confirm]`), chromedp.Sleep(3500*time.Millisecond), chromedp.Evaluate(`document.querySelector('#add-error').textContent.includes('команды') && document.querySelector('#add-dialog').open`, &valid)); err != nil || !valid {
		t.Fatalf("unsafe AWG import did not leave actionable error: %v", err)
	}
	stored, err := fs.LoadAWGProfile()
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
	for _, viewport := range []struct {
		name          string
		width, height int64
	}{{"desktop", 1440, 1000}, {"mobile", 390, 844}} {
		var shot []byte
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(viewport.width, viewport.height), chromedp.Evaluate(`document.documentElement.scrollWidth <= innerWidth`, &valid), chromedp.FullScreenshot(&shot, 90)); err != nil {
			t.Fatal(err)
		}
		if !valid {
			t.Fatalf("horizontal overflow at %s", viewport.name)
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
		if err := chromedp.Run(ctx, chromedp.Navigate(server.URL+"/#"+page), chromedp.WaitVisible("#"+page), chromedp.Evaluate(`document.documentElement.scrollWidth <= innerWidth`, &valid)); err != nil || !valid {
			t.Fatalf("mobile page %s failed or overflowed: %v", page, err)
		}
	}
	if err := chromedp.Run(ctx, chromedp.Click(`nav a[href="#settings"]`), chromedp.SendKeys(`#settings-form input[name="refresh-interval"]`, "7"), chromedp.Sleep(3500*time.Millisecond), chromedp.Evaluate(`document.querySelector('#settings-form input').value.endsWith('7')`, &valid)); err != nil || !valid {
		t.Fatalf("poll overwrote form: %v", err)
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
