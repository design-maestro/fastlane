package openwrt_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

var browserEnvNames = []string{
	"FASTLANE_OPENWRT_BROWSER_BIN",
	"CHROME_BIN",
	"CHROMIUM_BIN",
	"BROWSER",
}

var browserCandidates = []string{
	"headless_shell",
	"headless-shell",
	"chromium",
	"chromium-browser",
	"google-chrome",
	"google-chrome-stable",
	"google-chrome-beta",
	"google-chrome-unstable",
	"/usr/bin/google-chrome",
	"/usr/local/bin/chrome",
	"/snap/bin/chromium",
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	"chrome",
}

type luciPageSpec struct {
	name              string
	route             string
	rootSelector      string
	requiredSelectors []string
}

var (
	luciVPNPage = luciPageSpec{
		name:         "VPN",
		route:        "vpn",
		rootSelector: "#fastlane-content",
		requiredSelectors: []string{
			".fl-status",
			".fl-sourcebar",
			".fl-source-add",
			".fl-server-panel",
			".fl-search",
			".fl-toolbar-refresh",
		},
	}
	luciDiagnosticsPage = luciPageSpec{
		name:         "diagnostics",
		route:        "diagnostics",
		rootSelector: "#fastlane-diagnostics-root",
		requiredSelectors: []string{
			".fld-overview",
			".fastlane-diagnostics-actions button",
			".fastlane-diagnostics-advanced",
		},
	}
	luciRoutingPage = luciPageSpec{
		name:         "routing",
		route:        "routing",
		rootSelector: ".flr-page",
		requiredSelectors: []string{
			".flr-control",
			".flr-switch input",
			".flr-flow",
			".flr-advanced",
		},
	}
	luciSettingsPage = luciPageSpec{
		name:         "settings",
		route:        "settings",
		rootSelector: ".fastlane-settings",
		requiredSelectors: []string{
			".fls-actions button",
			".fls-grid",
			".fls-input",
			".fls-toggle input",
		},
	}
)

func (h *openWRTHarness) AssertLuCIVPNPage(ctx context.Context, expectedTexts ...string) error {
	return h.assertLuCIPage(ctx, luciVPNPage, expectedTexts...)
}

func (h *openWRTHarness) AssertLuCIDiagnosticsPage(ctx context.Context, expectedTexts ...string) error {
	return h.assertLuCIPage(ctx, luciDiagnosticsPage, expectedTexts...)
}

func (h *openWRTHarness) AssertLuCIRoutingPage(ctx context.Context, expectedTexts ...string) error {
	return h.assertLuCIPage(ctx, luciRoutingPage, expectedTexts...)
}

func (h *openWRTHarness) AssertLuCISettingsPage(ctx context.Context, expectedTexts ...string) error {
	return h.assertLuCIPage(ctx, luciSettingsPage, expectedTexts...)
}

func (h *openWRTHarness) AssertLuCIVPNAddDialog(ctx context.Context) error {
	var clicked bool
	var dialog struct {
		Exists bool   `json:"exists"`
		Text   string `json:"text"`
	}
	err := h.assertLuCIPageWithActions(ctx, luciVPNPage, []string{"Добавить подписку"},
		chromedp.Evaluate(`(() => {
			const button = document.querySelector('.fl-source-add');
			if (!button || button.disabled) return false;
			button.click();
			return true;
		})()`, &clicked),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const form = document.querySelector('.fl-add-form');
			const modal = document.querySelector('.modal');
			return { exists: !!form, text: modal ? (modal.innerText || '') : '' };
		})()`, &dialog),
	)
	if err != nil {
		return err
	}
	if !clicked {
		return fmt.Errorf("VPN add button was missing or disabled")
	}
	if !dialog.Exists {
		return fmt.Errorf("VPN add button did not open the dialog")
	}
	for _, expected := range []string{"Добавить подписку", "Название (необязательно)", "Ссылка или конфигурация", "Отмена", "Добавить"} {
		if !strings.Contains(dialog.Text, expected) {
			return fmt.Errorf("VPN add dialog missing %q; modal=%q", expected, summarizeForError(dialog.Text, 320))
		}
	}
	return nil
}

func (h *openWRTHarness) AssertLuCIRoutingHAPPPreview(ctx context.Context) error {
	var submitted bool
	var preview string
	err := h.assertLuCIPageWithActions(ctx, luciRoutingPage, []string{"Проверить ссылку"},
		chromedp.Evaluate(`(() => {
			const input = document.querySelector('.flr-input');
			const button = Array.from(document.querySelectorAll('.flr-import button')).find((item) =>
				(item.innerText || '').includes('Проверить ссылку')
			);
			if (!input || !button) return false;
			const profile = { Name: 'OpenWrt smoke', DirectSites: ['example.ru'], ProxySites: ['example.com'], BlockSites: ['ads.example'] };
			const encoded = btoa(unescape(encodeURIComponent(JSON.stringify(profile)))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
			input.value = 'happ://routing/onadd/' + encoded;
			input.dispatchEvent(new Event('input', { bubbles: true }));
			button.click();
			return true;
		})()`, &submitted),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Text(`.flr-preview`, &preview, chromedp.ByQuery),
	)
	if err != nil {
		return err
	}
	if !submitted {
		return fmt.Errorf("routing page did not expose the HAPP import preview controls")
	}
	for _, expected := range []string{"OpenWrt smoke", "напряму — 1", "через VPN — 1", "блокировать — 1", "Частично применять её нельзя"} {
		if !strings.Contains(preview, expected) {
			return fmt.Errorf("routing HAPP preview missing %q; preview=%q", expected, summarizeForError(preview, 320))
		}
	}
	return nil
}

func (h *openWRTHarness) SetStrictEgressCheckViaLuCI(ctx context.Context, enabled bool) error {
	var submitted bool
	expression := fmt.Sprintf(`(() => {
		const field = Array.from(document.querySelectorAll('.fls-field')).find((item) =>
			(item.innerText || '').includes('Строгая проверка интернета')
		);
		const input = field && field.querySelector('input[type="checkbox"]');
		const save = document.querySelector('.fls-primary');
		if (!input || !save) return false;
		if (input.checked !== %t) input.click();
		save.click();
		return true;
	})()`, enabled)
	err := h.assertLuCIPageWithActions(ctx, luciSettingsPage, []string{"Строгая проверка интернета", "Сохранить"},
		chromedp.Evaluate(expression, &submitted),
		chromedp.Sleep(750*time.Millisecond),
	)
	if err != nil {
		return err
	}
	if !submitted {
		return fmt.Errorf("settings page did not expose strict egress toggle and save action")
	}

	verifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		output, err := h.sshOutput(verifyCtx, fastlaneRemoteBinary+" --json settings get")
		if err == nil {
			var settings struct {
				StrictEgressCheck bool `json:"strict_egress_check"`
			}
			if json.Unmarshal(output, &settings) == nil && settings.StrictEgressCheck == enabled {
				return nil
			}
		}
		if verifyCtx.Err() != nil {
			return fmt.Errorf("settings page did not persist strict-egress-check=%t: %w", enabled, verifyCtx.Err())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (h *openWRTHarness) assertLuCIPage(ctx context.Context, spec luciPageSpec, expectedTexts ...string) error {
	return h.assertLuCIPageWithActions(ctx, spec, expectedTexts)
}

func (h *openWRTHarness) assertLuCIPageWithActions(ctx context.Context, spec luciPageSpec, expectedTexts []string, actions ...chromedp.Action) error {
	browserPath, err := lookupBrowserBinary()
	if err != nil {
		return err
	}

	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browserPath),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, allocatorOptions...)
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	smokeCtx, cancelSmoke := context.WithTimeout(browserCtx, 90*time.Second)
	defer cancelSmoke()

	luciClient, sessionCookies, err := h.openLuCISession(smokeCtx)
	if err != nil {
		return err
	}
	if err := h.waitForLuCIRoutePage(smokeCtx, luciClient, spec); err != nil {
		return err
	}

	var runtimeErrors []string
	var runtimeMu sync.Mutex
	chromedp.ListenTarget(smokeCtx, func(ev any) {
		switch e := ev.(type) {
		case *runtime.EventExceptionThrown:
			details := ""
			if e.ExceptionDetails.Exception != nil {
				details = strings.TrimSpace(e.ExceptionDetails.Exception.Description)
			}
			if details == "" {
				details = strings.TrimSpace(e.ExceptionDetails.Text)
			}
			if details == "" {
				details = "unknown JavaScript exception"
			}
			runtimeMu.Lock()
			runtimeErrors = append(runtimeErrors, details)
			runtimeMu.Unlock()
		}
	})

	var snapshot luciPageSnapshot
	browserActions := []chromedp.Action{
		runtime.Enable(),
		network.Enable(),
		setBrowserCookies(h.luciURL("/cgi-bin/luci/"), sessionCookies),
		chromedp.Navigate(h.luciURL("/cgi-bin/luci/admin/services/fastlane/" + spec.route)),
		chromedp.WaitReady(`body`, chromedp.ByQuery),
		waitForRenderedLuCIPage(spec, expectedTexts, &snapshot),
	}
	browserActions = append(browserActions, actions...)
	if err := chromedp.Run(smokeCtx, browserActions...); err != nil {
		runtimeMu.Lock()
		defer runtimeMu.Unlock()
		if len(runtimeErrors) > 0 {
			return fmt.Errorf("browser smoke %s page: %w; runtime exception: %s", spec.name, err, runtimeErrors[0])
		}
		return fmt.Errorf("browser smoke %s page: %w", spec.name, err)
	}

	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	if len(runtimeErrors) > 0 {
		return fmt.Errorf("browser runtime exception: %s", runtimeErrors[0])
	}

	return nil
}

type luciPageSnapshot struct {
	BodyHTML              string   `json:"bodyHTML"`
	BodyText              string   `json:"bodyText"`
	HasActiveNavigation   bool     `json:"hasActiveNavigation"`
	HasCompleteNavigation bool     `json:"hasCompleteNavigation"`
	HasLoginForm          bool     `json:"hasLoginForm"`
	HasRoot               bool     `json:"hasRoot"`
	LogoLoaded            bool     `json:"logoLoaded"`
	MissingSelectors      []string `json:"missingSelectors"`
	PageError             string   `json:"pageError"`
	Title                 string   `json:"title"`
	URL                   string   `json:"url"`
}

func (h *openWRTHarness) openLuCISession(ctx context.Context) (*http.Client, []*http.Cookie, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create LuCI cookie jar: %w", err)
	}

	client := &http.Client{
		Jar:     jar,
		Timeout: 15 * time.Second,
	}

	rpcErr := h.authenticateLuCIViaRPC(ctx, client)
	if cookies := jar.Cookies(h.luciBaseURL()); len(cookies) > 0 {
		return client, cookies, nil
	}

	formErr := h.authenticateLuCIViaForm(ctx, client)
	cookies := jar.Cookies(h.luciBaseURL())
	if len(cookies) == 0 {
		return nil, nil, fmt.Errorf("authenticate LuCI session: rpc auth: %v; form auth: %v", rpcErr, formErr)
	}

	return client, cookies, nil
}

func (h *openWRTHarness) authenticateLuCIViaRPC(ctx context.Context, client *http.Client) error {
	body, err := json.Marshal(map[string]any{
		"id":     1,
		"method": "login",
		"params": []string{"root", luciTestPassword},
	})
	if err != nil {
		return fmt.Errorf("marshal LuCI RPC auth request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.luciURL("/cgi-bin/luci/rpc/auth"), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build LuCI RPC auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("POST LuCI RPC auth: %w", err)
	}
	defer resp.Body.Close()

	payload, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if readErr != nil {
		return fmt.Errorf("read LuCI RPC auth response: %w", readErr)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("LuCI RPC auth status %s: %s", resp.Status, summarizeForError(string(payload), 240))
	}

	return nil
}

func (h *openWRTHarness) authenticateLuCIViaForm(ctx context.Context, client *http.Client) error {
	loginURL := h.luciURL("/cgi-bin/luci/")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loginURL, nil)
	if err != nil {
		return fmt.Errorf("build LuCI login GET request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET LuCI login page: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	form := url.Values{
		"luci_username": {"root"},
		"luci_password": {luciTestPassword},
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build LuCI login POST request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err = client.Do(req)
	if err != nil {
		return fmt.Errorf("POST LuCI login form: %w", err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if readErr != nil {
		return fmt.Errorf("read LuCI login form response: %w", readErr)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("LuCI login form status %s: %s", resp.Status, summarizeForError(string(body), 240))
	}

	return nil
}

func (h *openWRTHarness) waitForLuCIRoutePage(ctx context.Context, client *http.Client, spec luciPageSpec) error {
	pageURL := h.luciURL("/cgi-bin/luci/admin/services/fastlane/" + spec.route)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
		if err != nil {
			return fmt.Errorf("build LuCI %s request: %w", spec.name, err)
		}

		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("wait for LuCI %s route: %w", spec.name, ctx.Err())
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read LuCI %s route response: %w", spec.name, readErr)
		}

		page := string(body)
		if resp.StatusCode == http.StatusOK &&
			!strings.Contains(page, `id="luci_password"`) &&
			(strings.Contains(page, "fastlane/"+spec.route) || strings.Contains(page, "Fast Lane")) {
			return nil
		}

		if ctx.Err() != nil {
			return fmt.Errorf("wait for LuCI %s route: %w; last status=%s body=%q", spec.name, ctx.Err(), resp.Status, summarizeForError(page, 320))
		}

		time.Sleep(500 * time.Millisecond)
	}
}

func waitForRenderedLuCIPage(spec luciPageSpec, expectedTexts []string, snapshot *luciPageSnapshot) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		var last luciPageSnapshot
		var lastErr error
		var rootSince time.Time
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			current := luciPageSnapshot{}
			if err := captureLuCIPageSnapshot(spec, &current).Do(ctx); err != nil {
				lastErr = err
			} else {
				last = current
				lastErr = nil
				if current.HasLoginForm {
					return fmt.Errorf("browser returned to LuCI login page: url=%s title=%q", current.URL, current.Title)
				}
				if current.PageError != "" {
					return fmt.Errorf("%s page error: %s", spec.name, summarizeForError(current.PageError, 240))
				}
				ready := current.HasRoot && current.LogoLoaded && current.HasCompleteNavigation && current.HasActiveNavigation && len(current.MissingSelectors) == 0
				if ready && containsAllText(current.BodyText, expectedTexts) {
					if snapshot != nil {
						*snapshot = current
					}
					return nil
				}
				if current.HasRoot {
					if rootSince.IsZero() {
						rootSince = time.Now()
					}
					if time.Since(rootSince) >= 10*time.Second {
						return fmt.Errorf("%s page incomplete: missing text=%v logoLoaded=%t completeNavigation=%t activeNavigation=%t missingSelectors=%v; url=%s title=%q body=%q", spec.name, missingText(current.BodyText, expectedTexts), current.LogoLoaded, current.HasCompleteNavigation, current.HasActiveNavigation, current.MissingSelectors, current.URL, current.Title, summarizeForError(current.BodyText, 320))
					}
				} else {
					rootSince = time.Time{}
				}
			}

			select {
			case <-ctx.Done():
				parts := []string{fmt.Sprintf("wait for rendered %s page: %v", spec.name, ctx.Err())}
				if lastErr != nil {
					parts = append(parts, fmt.Sprintf("last snapshot error=%v", lastErr))
				}
				if last.URL != "" {
					parts = append(parts, fmt.Sprintf("last url=%s", last.URL))
				}
				if last.Title != "" {
					parts = append(parts, fmt.Sprintf("last title=%q", last.Title))
				}
				parts = append(parts,
					fmt.Sprintf("hasRoot=%t", last.HasRoot),
					fmt.Sprintf("logoLoaded=%t", last.LogoLoaded),
					fmt.Sprintf("hasCompleteNavigation=%t", last.HasCompleteNavigation),
					fmt.Sprintf("hasActiveNavigation=%t", last.HasActiveNavigation),
					fmt.Sprintf("missingSelectors=%v", last.MissingSelectors),
					fmt.Sprintf("body=%q", summarizeForError(last.BodyText, 320)),
				)
				return fmt.Errorf("%s", strings.Join(parts, "; "))
			case <-ticker.C:
			}
		}
	})
}

func (h *openWRTHarness) luciURL(path string) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", h.httpPort, path)
}

func (h *openWRTHarness) luciBaseURL() *url.URL {
	base, err := url.Parse(h.luciURL("/cgi-bin/luci/"))
	if err != nil {
		panic(err)
	}
	return base
}

func setBrowserCookies(pageURL string, cookies []*http.Cookie) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		params, err := browserCookieParams(pageURL, cookies)
		if err != nil {
			return err
		}
		return network.SetCookies(params).Do(ctx)
	})
}

func browserCookieParams(pageURL string, cookies []*http.Cookie) ([]*network.CookieParam, error) {
	params := make([]*network.CookieParam, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil || strings.TrimSpace(cookie.Name) == "" {
			continue
		}

		param := &network.CookieParam{
			Name:     cookie.Name,
			Value:    cookie.Value,
			URL:      pageURL,
			Path:     cookie.Path,
			Secure:   cookie.Secure,
			HTTPOnly: cookie.HttpOnly,
		}
		if param.Path == "" {
			param.Path = "/"
		}
		if cookie.Domain != "" {
			param.Domain = strings.TrimPrefix(cookie.Domain, ".")
		}

		params = append(params, param)
	}

	if len(params) == 0 {
		return nil, fmt.Errorf("set browser cookies: no cookies to apply")
	}

	return params, nil
}

func captureLuCIPageSnapshot(spec luciPageSpec, snapshot *luciPageSnapshot) chromedp.Action {
	specJSON, err := json.Marshal(map[string]any{
		"rootSelector":      spec.rootSelector,
		"route":             spec.route,
		"requiredSelectors": spec.requiredSelectors,
	})
	if err != nil {
		return chromedp.ActionFunc(func(context.Context) error {
			return fmt.Errorf("marshal LuCI page spec: %w", err)
		})
	}

	expression := fmt.Sprintf(`(() => {
		const spec = %s;
		const body = document.body;
		const pageError = document.querySelector('.fastlane-page-banner-warning');
		const logo = document.querySelector('.fl-brand .fl-logo');
		const navRoutes = ['vpn', 'routing', 'diagnostics', 'settings'];
		const hasCompleteNavigation = navRoutes.every((route) =>
			!!document.querySelector('.fl-nav-links a[href$="/' + route + '"]')
		);

		return {
			bodyHTML: body ? body.innerHTML : '',
			bodyText: body ? body.innerText : '',
			hasActiveNavigation: !!document.querySelector('.fl-nav-link-active[href$="/' + spec.route + '"][aria-current="page"]'),
			hasCompleteNavigation,
			hasLoginForm: !!document.querySelector('#luci_password'),
			hasRoot: !!document.querySelector(spec.rootSelector),
			logoLoaded: !!logo && logo.complete && logo.naturalWidth > 0 && /fastlane\/assets\/fastlane-mark\.png(?:[?#].*)?$/.test(logo.src),
			missingSelectors: spec.requiredSelectors.filter((selector) => !document.querySelector(selector)),
			pageError: pageError ? pageError.innerText.trim() : '',
			title: document.title || '',
			url: window.location.href
		};
	})()`, string(specJSON))

	return chromedp.Evaluate(expression, snapshot)
}

func containsAllText(body string, expectedTexts []string) bool {
	for _, expected := range expectedTexts {
		if !strings.Contains(body, expected) {
			return false
		}
	}
	return true
}

func missingText(body string, expectedTexts []string) []string {
	missing := make([]string, 0, len(expectedTexts))
	for _, expected := range expectedTexts {
		if !strings.Contains(body, expected) {
			missing = append(missing, expected)
		}
	}
	return missing
}

func summarizeForError(value string, limit int) string {
	text := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

func lookupBrowserBinary() (string, error) {
	return resolveBrowserBinary(os.LookupEnv, exec.LookPath)
}

func resolveBrowserBinary(lookupEnv func(string) (string, bool), lookPath func(string) (string, error)) (string, error) {
	for _, envName := range browserEnvNames {
		if value, ok := lookupEnv(envName); ok {
			path := strings.TrimSpace(value)
			if path != "" {
				if strings.Contains(path, string(os.PathSeparator)) {
					return path, nil
				}
				resolved, err := lookPath(path)
				if err == nil {
					return resolved, nil
				}
				return path, nil
			}
		}
	}

	for _, candidate := range browserCandidates {
		path, err := lookPath(candidate)
		if err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("find browser binary: install chromium/google-chrome/headless-shell or set FASTLANE_OPENWRT_BROWSER_BIN")
}
