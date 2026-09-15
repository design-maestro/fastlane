package managementhttp

import (
	"context"
	"embed"
	"errors"
	"net/http"
	"strings"

	"github.com/design-maestro/fastlane/internal/amneziawg"
	"github.com/design-maestro/fastlane/internal/app"
	"github.com/design-maestro/fastlane/internal/domain"
)

//go:embed web/*
var panelFiles embed.FS

// Only fixed, public codes may cross the asynchronous job boundary. In
// particular, parser errors may contain unexpected text from an imported file.
type panelJobError string

func (e panelJobError) Error() string { return string(e) }

func servePanel(w http.ResponseWriter, r *http.Request) bool {
	name := map[string]string{"/": "index.html", "/panel.js": "panel.js", "/panel.css": "panel.css", "/legacy.css": "legacy.css", "/legacy-brand.png": "legacy-brand.png", "/legacy-icons.js": "legacy-icons.js"}[r.URL.Path]
	if r.URL.Path == "/legacy-countries.js" || r.URL.Path == "/panel-i18n.js" {
		name = strings.TrimPrefix(r.URL.Path, "/")
	}
	for _, asset := range []string{"routing-panel.js", "routing-panel.css", "settings-panel.js", "settings-panel.css", "diagnostics-panel.js", "diagnostics-panel.css"} {
		if r.URL.Path == "/"+asset {
			name = asset
		}
	}
	if name == "" || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		return false
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", map[string]string{"index.html": "text/html; charset=utf-8", "panel.js": "text/javascript; charset=utf-8", "panel.css": "text/css; charset=utf-8", "legacy.css": "text/css; charset=utf-8", "legacy-brand.png": "image/png", "legacy-icons.js": "text/javascript; charset=utf-8"}[name])
	if strings.HasSuffix(name, ".js") {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	}
	if strings.HasSuffix(name, ".css") {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	}
	data, err := panelFiles.ReadFile("web/" + name)
	if err != nil {
		http.Error(w, "Panel unavailable", 500)
		return true
	}
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
	return true
}

type panelService interface {
	SetSetting(string, string) (domain.Settings, error)
	UpdateDNS(context.Context, domain.DNSSettings) (domain.Settings, error)
	GetAWGStatus(context.Context) (app.AWGStatus, error)
	ImportAWGProfile(string, []byte) (app.AWGStatus, error)
	CheckAWG(context.Context) (app.AWGStatus, error)
	ConnectAWG(context.Context) error
	DisconnectAWG(context.Context) error
	RemoveAWG(context.Context) error
	ConfigureFirewallBypass(context.Context, []string, []string, bool, int) (domain.FirewallSettings, error)
	ConfigureFirewallSplit(context.Context, []string, []string, []string, bool, int) (domain.FirewallSettings, error)
	DisableFirewall(context.Context) (domain.FirewallSettings, error)
}

func (h *Handler) panelAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/api/v1/session" && r.Method == http.MethodDelete {
		if cookie, err := r.Cookie("fastlane_session"); err == nil {
			h.sessionMu.Lock()
			delete(h.sessions, cookie.Value)
			h.sessionMu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: "fastlane_session", Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		h.writeJSON(w, 200, map[string]bool{"ok": true})
		return true
	}
	if r.URL.Path == "/api/v1/jobs/settings" && r.Method == http.MethodPost {
		var values map[string]string
		if err := decodeJSON(w, r, &values); err != nil || len(values) == 0 {
			h.writeError(w, 400, "invalid_request")
			return true
		}
		h.startJob(w, "settings", func(context.Context) error { _, err := h.service.PatchSettings(values); return err })
		return true
	}
	if !strings.HasPrefix(r.URL.Path, "/api/v1/awg") && r.URL.Path != "/api/v1/routing" && r.URL.Path != "/api/v1/dns" && r.URL.Path != "/api/v1/hide-keywords" {
		return false
	}
	s, ok := h.service.(panelService)
	if !ok {
		h.writeError(w, 501, "feature_unavailable")
		return true
	}
	if r.URL.Path == "/api/v1/hide-keywords" && r.Method == http.MethodPost {
		var input struct {
			Keywords []string `json:"keywords"`
		}
		if err := decodeJSON(w, r, &input); err != nil {
			h.writeError(w, 400, "invalid_request")
			return true
		}
		h.startJob(w, "settings", func(context.Context) error {
			_, err := s.SetSetting("auto.hide-keywords", strings.Join(input.Keywords, ","))
			return err
		})
		return true
	}
	if r.URL.Path == "/api/v1/dns" && r.Method == http.MethodPost {
		var input struct {
			Mode      domain.DNSMode      `json:"mode"`
			Transport domain.DNSTransport `json:"transport"`
			Servers   []string            `json:"servers"`
		}
		if err := decodeJSON(w, r, &input); err != nil {
			h.writeError(w, 400, "invalid_request")
			return true
		}
		if input.Transport != domain.DNSTransportDoH && input.Transport != domain.DNSTransportPlain {
			h.writeError(w, 400, "invalid_request")
			return true
		}
		h.startJob(w, "settings", func(ctx context.Context) error {
			snapshot, err := h.service.Status()
			if err != nil {
				return err
			}
			dns := snapshot.Settings.DNS
			dns.Mode = input.Mode
			dns.Transport = input.Transport
			dns.Servers = input.Servers
			_, err = s.UpdateDNS(ctx, dns)
			return err
		})
		return true
	}
	if r.URL.Path == "/api/v1/awg" && r.Method == http.MethodGet {
		status, err := s.GetAWGStatus(r.Context())
		if err != nil {
			h.internalError(w, "AWG status", err)
		} else {
			h.writeJSON(w, 200, status)
		}
		return true
	}
	if r.URL.Path == "/api/v1/awg" && r.Method == http.MethodPost {
		var input struct {
			Name   string `json:"name"`
			Config string `json:"config"`
		}
		if err := decodeJSON(w, r, &input); err != nil || len(input.Config) > 1<<20 {
			h.writeError(w, 400, "invalid_request")
			return true
		}
		h.startJob(w, "awg-import", func(context.Context) error {
			_, err := s.ImportAWGProfile(input.Name, []byte(input.Config))
			switch {
			case err == nil:
				return nil
			case errors.Is(err, amneziawg.ErrUnsafeDirective):
				return panelJobError("unsafe_awg_directive")
			case errors.Is(err, amneziawg.ErrUnsupportedVersion):
				return panelJobError("unsupported_awg_version")
			case errors.Is(err, amneziawg.ErrUnsupportedParameter):
				return panelJobError("unsupported_awg_parameter")
			default:
				return panelJobError("awg_import_failed")
			}
		})
		return true
	}
	if r.URL.Path == "/api/v1/awg" && r.Method == http.MethodDelete {
		h.startJob(w, "awg-remove", s.RemoveAWG)
		return true
	}
	if r.Method == http.MethodPost {
		switch r.URL.Path {
		case "/api/v1/awg/connect":
			h.startJob(w, "awg-connect", s.ConnectAWG)
			return true
		case "/api/v1/awg/disconnect":
			h.startJob(w, "awg-disconnect", s.DisconnectAWG)
			return true
		case "/api/v1/awg/check":
			h.startJob(w, "awg-check", func(ctx context.Context) error { _, err := s.CheckAWG(ctx); return err })
			return true
		case "/api/v1/routing":
			var input struct {
				Mode     string   `json:"mode"`
				Proxy    []string `json:"proxy"`
				Bypass   []string `json:"bypass"`
				Excluded []string `json:"excluded"`
			}
			if err := decodeJSON(w, r, &input); err != nil {
				h.writeError(w, 400, "invalid_request")
				return true
			}
			if input.Mode != "bypass" && input.Mode != "split" && input.Mode != "disabled" {
				h.writeError(w, 400, "invalid_routing_mode")
				return true
			}
			h.startJob(w, "routing", func(ctx context.Context) error {
				snapshot, err := h.service.Status()
				if err != nil {
					return err
				}
				port := snapshot.Settings.Firewall.TransparentPort
				switch input.Mode {
				case "bypass":
					_, err = s.ConfigureFirewallBypass(ctx, input.Bypass, input.Excluded, true, port)
				case "split":
					_, err = s.ConfigureFirewallSplit(ctx, input.Proxy, input.Bypass, input.Excluded, true, port)
				case "disabled":
					_, err = s.DisableFirewall(ctx)
				}
				return err
			})
			return true
		}
	}
	h.writeError(w, 404, "not_found")
	return true
}
