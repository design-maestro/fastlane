package managementhttp

// Integration: call h.settingsPanelAPI(w, r) after authentication and before
// panelAPI in ServeHTTP. This dispatcher also defends its own auth/origin boundary.
// Assets and the shell are deliberately owned by the root panel integration.

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/design-maestro/fastlane/internal/backend"
	"github.com/design-maestro/fastlane/internal/buildinfo"
	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/update"
	apiresponse "github.com/design-maestro/fastlane/pkg/api"
)

type settingsPanelStore interface {
	GetSettings() (domain.Settings, error)
	SetSetting(string, string) (domain.Settings, error)
	UpdateDNS(context.Context, domain.DNSSettings) (domain.Settings, error)
}

type settingsPanelRuntime interface {
	RuntimeStatus(context.Context) (backend.RuntimeStatus, error)
	DNSStatus(context.Context) (domain.DNSRuntimeStatus, error)
	IPv6Status(context.Context) (domain.IPv6Status, error)
}

// Pointers distinguish omitted fields from deliberately false/zero values.
type settingsPanelPatch struct {
	RefreshInterval     *string `json:"refresh_interval"`
	HealthCheckInterval *string `json:"health_check_interval"`
	URLTestURL          *string `json:"url_test_url"`
	URLTestTimeout      *string `json:"url_test_timeout"`
	SwitchCooldown      *string `json:"switch_cooldown"`
	LatencyThreshold    *string `json:"latency_threshold"`
	StrictEgressCheck   *bool   `json:"strict_egress_check"`
}

func (p settingsPanelPatch) values() map[string]string {
	values := map[string]string{}
	for key, value := range map[string]*string{
		"refresh-interval": p.RefreshInterval, "health-check-interval": p.HealthCheckInterval,
		"url-test-url": p.URLTestURL, "url-test-timeout": p.URLTestTimeout,
		"switch-cooldown": p.SwitchCooldown, "latency-threshold": p.LatencyThreshold,
	} {
		if value != nil {
			values[key] = *value
		}
	}
	if p.StrictEgressCheck != nil {
		values["strict-egress-check"] = "false"
		if *p.StrictEgressCheck {
			values["strict-egress-check"] = "true"
		}
	}
	return values
}

type settingsPanelDNSPatch struct {
	Mode          domain.DNSMode      `json:"mode"`
	Transport     domain.DNSTransport `json:"transport"`
	Servers       *[]string           `json:"servers"`
	Bootstrap     *[]string           `json:"bootstrap"`
	DirectDomains *[]string           `json:"direct_domains"`
}

type settingsPanelUpdate struct {
	Status         string                `json:"status"`
	CurrentVersion string                `json:"current_version"`
	Reason         string                `json:"reason"`
	Message        string                `json:"message,omitempty"`
	Candidate      *settingsPanelRelease `json:"candidate,omitempty"`
}

type settingsPanelRelease struct {
	ID      int64  `json:"id"`
	Version string `json:"version"`
	Page    string `json:"page"`
}

func publicPanelUpdate(state update.State) settingsPanelUpdate {
	result := settingsPanelUpdate{Status: state.Status, CurrentVersion: state.Current, Message: state.Message}
	if state.Candidate != nil {
		result.Candidate = &settingsPanelRelease{state.Candidate.ID, state.Candidate.Version, state.Candidate.Page}
	}
	return result
}

func (h *Handler) panelUpdateStatus(ctx context.Context) settingsPanelUpdate {
	host := h.settingsHost()
	if host == nil || !host.Capabilities().Update {
		return settingsPanelUpdate{Status: "unsupported", CurrentVersion: buildinfo.Current().Version, Reason: "updater_adapter_unavailable"}
	}
	state, err := host.UpdateStatus(ctx)
	if err != nil {
		return settingsPanelUpdate{Status: "error", CurrentVersion: buildinfo.Current().Version, Reason: "update_status_failed"}
	}
	return publicPanelUpdate(state)
}

type settingsPanelResponse struct {
	Settings    domain.Settings     `json:"settings"`
	Update      settingsPanelUpdate `json:"update"`
	Unavailable map[string]string   `json:"unavailable"`
}

type diagnosticsPanelResponse struct {
	Status  apiresponse.StatusResponse `json:"status"`
	Runtime backend.RuntimeStatus      `json:"runtime"`
	DNS     domain.DNSRuntimeStatus    `json:"dns"`
	IPv6    domain.IPv6Status          `json:"ipv6"`
	// The application service does not expose its configured store/file paths.
	// Do not inspect guessed host paths or report zero files as a successful check.
	Files       map[string]diagnosticsPanelFile `json:"files"`
	Unavailable map[string]string               `json:"unavailable"`
}

func (h *Handler) settingsPanelAPI(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if path != "/api/v1/settings-panel" && !strings.HasPrefix(path, "/api/v1/settings-panel/") && path != "/api/v1/diagnostics-panel" {
		return false
	}
	setSecurityHeaders(w)
	w.Header().Set("Cache-Control", "no-store")
	if (h.loopbackOnly && !isLoopbackHost(r.Host)) || !sameOrigin(r) || strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
		h.writeError(w, http.StatusForbidden, "origin_rejected")
		return true
	}
	if !h.authenticated(r) {
		h.writeError(w, http.StatusUnauthorized, "auth_required")
		return true
	}

	if path == "/api/v1/diagnostics-panel" {
		if r.Method != http.MethodGet {
			settingsPanelMethod(w, h, "GET")
			return true
		}
		h.readDiagnosticsPanel(w, r)
		return true
	}
	if path == "/api/v1/settings-panel/update" || path == "/api/v1/settings-panel/update/check" || path == "/api/v1/settings-panel/update/install" || path == "/api/v1/settings-panel/uninstall" || path == "/api/v1/settings-panel/language" {
		if path == "/api/v1/settings-panel/update" && r.Method == http.MethodGet {
			h.writeJSON(w, http.StatusOK, h.panelUpdateStatus(r.Context()))
		} else if r.Method == http.MethodPost && path != "/api/v1/settings-panel/update" {
			host := h.settingsHost()
			if host == nil || path == "/api/v1/settings-panel/language" {
				h.writeError(w, 501, "unsupported_runtime")
				return true
			}
			if path == "/api/v1/settings-panel/uninstall" {
				if !host.Capabilities().Uninstall {
					h.writeError(w, 501, "unsupported_runtime")
					return true
				}
				var input struct {
					Confirm bool `json:"confirm"`
				}
				if err := decodeJSON(w, r, &input); err != nil || !input.Confirm {
					h.writeError(w, 400, "confirmation_required")
					return true
				}
				h.startMaintenanceJob(w, "settings-panel-uninstall", func(ctx context.Context) error {
					return safeSettingsPanelError(host.Uninstall(ctx), "uninstall_failed")
				})
			} else {
				if !host.Capabilities().Update {
					h.writeError(w, 501, "unsupported_runtime")
					return true
				}
				operation, releaseID := "check", int64(0)
				if path == "/api/v1/settings-panel/update/install" {
					operation = "install"
					var input struct {
						ReleaseID int64 `json:"release_id"`
						Confirm   bool  `json:"confirm"`
					}
					if err := decodeJSON(w, r, &input); err != nil || input.ReleaseID <= 0 || !input.Confirm {
						h.writeError(w, 400, "confirmation_required")
						return true
					}
					releaseID = input.ReleaseID
				} else {
					var input struct{}
					if err := decodeJSON(w, r, &input); err != nil {
						h.writeError(w, 400, "invalid_request")
						return true
					}
				}
				h.startMaintenanceJob(w, "settings-panel-update-"+operation, func(ctx context.Context) error {
					if operation == "install" {
						state, err := host.UpdateStatus(ctx)
						if err != nil {
							return panelJobError("update_status_failed")
						}
						if state.Status != "available" || state.Candidate == nil || state.Candidate.ID != releaseID {
							return panelJobError("release_changed")
						}
					}
					return safeSettingsPanelError(host.RunUpdate(ctx, operation, releaseID), "update_"+operation+"_failed")
				})
			}
		} else {
			allow := "POST"
			if path == "/api/v1/settings-panel/update" {
				allow = "GET"
			}
			settingsPanelMethod(w, h, allow)
		}
		return true
	}
	if path == "/api/v1/settings-panel" && r.Method == http.MethodPatch {
		var input settingsPanelPatch
		if err := decodeJSON(w, r, &input); err != nil || len(input.values()) == 0 {
			h.writeError(w, 400, "invalid_request")
			return true
		}
		values := input.values()
		h.startJob(w, "settings-panel-save", func(context.Context) error {
			_, err := h.service.PatchSettings(values)
			return safeSettingsPanelError(err, "settings_save_failed")
		})
		return true
	}
	s, ok := h.service.(settingsPanelStore)
	if !ok {
		h.writeError(w, 501, "feature_unavailable")
		return true
	}
	switch path {
	case "/api/v1/settings-panel":
		if r.Method != http.MethodGet {
			settingsPanelMethod(w, h, "GET, PATCH")
			return true
		}
		settings, err := s.GetSettings()
		if err != nil {
			h.internalError(w, "read settings panel", panelJobError("settings_read_failed"))
			return true
		}
		unavailable := map[string]string{"luci_language": "uci_adapter_unavailable", "package_manager": "luci_origin_unavailable"}
		host := h.settingsHost()
		if host == nil || !host.Capabilities().Update {
			unavailable["update"] = "updater_adapter_unavailable"
		}
		if host == nil || !host.Capabilities().Uninstall {
			unavailable["uninstall"] = "uninstaller_adapter_unavailable"
		}
		h.writeJSON(w, 200, settingsPanelResponse{Settings: settings, Update: h.panelUpdateStatus(r.Context()), Unavailable: unavailable})
	case "/api/v1/settings-panel/hide-keywords":
		if r.Method != http.MethodPost {
			settingsPanelMethod(w, h, "POST")
			return true
		}
		var input struct {
			Keywords *[]string `json:"keywords"`
		}
		if err := decodeJSON(w, r, &input); err != nil || input.Keywords == nil || len(*input.Keywords) > 128 {
			h.writeError(w, 400, "invalid_request")
			return true
		}
		for _, word := range *input.Keywords {
			if len([]rune(word)) > 64 || strings.ContainsAny(word, ",\r\n\x00") {
				h.writeError(w, 400, "invalid_hide_keyword")
				return true
			}
		}
		h.startJob(w, "settings-panel-keywords", func(context.Context) error {
			_, err := s.SetSetting("auto.hide-keywords", strings.Join(*input.Keywords, "\n"))
			return safeSettingsPanelError(err, "hide_keywords_save_failed")
		})
	case "/api/v1/settings-panel/dns":
		if r.Method != http.MethodPost {
			settingsPanelMethod(w, h, "POST")
			return true
		}
		var input settingsPanelDNSPatch
		if err := decodeJSON(w, r, &input); err != nil || (input.Mode != domain.DNSModeSystem && input.Mode != domain.DNSModeRemote && input.Mode != domain.DNSModeSplit && input.Mode != domain.DNSModeDisabled) || (input.Transport != domain.DNSTransportDoH && input.Transport != domain.DNSTransportPlain) || input.Servers == nil {
			h.writeError(w, 400, "invalid_request")
			return true
		}
		h.startJob(w, "settings-panel-dns", func(ctx context.Context) error {
			settings, err := s.GetSettings()
			if err != nil {
				return panelJobError("settings_read_failed")
			}
			dns := settings.DNS
			dns.Mode, dns.Transport, dns.Servers = input.Mode, input.Transport, *input.Servers
			if input.Bootstrap != nil {
				dns.Bootstrap = *input.Bootstrap
			}
			if input.DirectDomains != nil {
				dns.DirectDomains = *input.DirectDomains
			}
			_, err = s.UpdateDNS(ctx, dns)
			return safeSettingsPanelError(err, "dns_save_failed")
		})
	default:
		h.writeError(w, 404, "not_found")
	}
	return true
}

func settingsPanelMethod(w http.ResponseWriter, h *Handler, allow string) {
	w.Header().Set("Allow", allow)
	h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
}

// App/adapter error strings may include user input. Convert before startJob's
// logging boundary as well as before its public response serialization.
func safeSettingsPanelError(err error, code string) error {
	for _, safe := range []string{"uninstall_completion_unconfirmed", "uninstall_cancelled", "update_completion_unconfirmed"} {
		if errors.Is(err, panelJobError(safe)) {
			return panelJobError(safe)
		}
	}
	if err != nil {
		return panelJobError(code)
	}
	return nil
}

func (h *Handler) readDiagnosticsPanel(w http.ResponseWriter, r *http.Request) {
	snapshot, err := h.service.Status()
	if err != nil {
		h.internalError(w, "read diagnostics", panelJobError("diagnostics_read_failed"))
		return
	}
	result := diagnosticsPanelResponse{Status: apiresponse.StatusResponseFromSnapshot(snapshot), Unavailable: map[string]string{"files": "file_inspection_adapter_unavailable"}}
	if host := h.settingsHost(); host != nil && host.Capabilities().Files {
		result.Files = map[string]diagnosticsPanelFile{}
		for key, path := range host.Paths() {
			result.Files[key] = inspectPanelFile(path)
		}
		delete(result.Unavailable, "files")
	}
	if s, ok := h.service.(settingsPanelRuntime); ok {
		result.Runtime, err = s.RuntimeStatus(r.Context())
		if err != nil {
			result.Runtime = backend.RuntimeStatus{}
			result.Unavailable["runtime"] = "runtime_status_failed"
		} else if result.Runtime.ConfigPath == "" && result.Runtime.ServiceState == "" && !result.Runtime.Running {
			result.Unavailable["runtime"] = "unsupported_runtime"
		}
		result.DNS, err = s.DNSStatus(r.Context())
		if err != nil {
			result.DNS = domain.DNSRuntimeStatus{}
			result.Unavailable["dns"] = "dns_status_failed"
		} else if !result.DNS.Available {
			result.Unavailable["dns"] = "unsupported_runtime"
		}
		// Runtime errors can include secrets. Only fixed codes cross this API.
		if result.DNS.Error != "" {
			result.DNS.Error = "dns_status_failed"
			result.Unavailable["dns"] = "dns_status_failed"
		}
		result.IPv6, err = s.IPv6Status(r.Context())
		if err != nil {
			result.IPv6 = domain.IPv6Status{}
			result.Unavailable["ipv6"] = "ipv6_status_failed"
		} else if !result.IPv6.Available {
			result.Unavailable["ipv6"] = "unsupported_runtime"
		}
	} else {
		for _, key := range []string{"runtime", "dns", "ipv6"} {
			result.Unavailable[key] = "unsupported_runtime"
		}
	}
	h.writeJSON(w, 200, result)
}
