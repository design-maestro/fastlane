package managementhttp

import (
	"context"
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
	if err := chromedp.Run(ctx, chromedp.EmulateViewport(1440, 1000), chromedp.Navigate(server.URL), chromedp.WaitVisible("#login"), chromedp.SendKeys("#login-form input", testAccessToken), chromedp.Click("#login-form button"), chromedp.WaitVisible("#application"), chromedp.Click("#add-toggle"), chromedp.SendKeys("#add-form input[name=name]", "Тестовый источник"), chromedp.SendKeys("#add-form textarea", "vless://11111111-1111-4111-8111-111111111111@vpn.example:443?security=tls#Test%20server"), chromedp.Click("#add-form button"), chromedp.WaitVisible("#server-list tbody tr")); err != nil {
		t.Fatal(err)
	}
	var valid bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('#server-list').textContent.includes('Test server')`, &valid)); err != nil || !valid {
		t.Fatalf("import failed: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.Focus("#server-list button"), chromedp.Sleep(3500*time.Millisecond), chromedp.Evaluate(`document.activeElement === document.querySelector('#server-list button')`, &valid)); err != nil || !valid {
		t.Fatalf("poll lost keyboard focus: %v", err)
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
