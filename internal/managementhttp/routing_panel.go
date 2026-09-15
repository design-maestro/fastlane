package managementhttp

// Integration: call h.routingPanelAPI after the normal authentication/origin
// guards, before panelAPI. Serve web/routing-panel.{js,css} as public assets.
// All writes use the existing service and the shared job coordinator. The only
// external operation is the existing fixed OpenWrt Geo helper (status/update).

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
)

type routingPanelService interface {
	ListFirewallTargetServices() ([]domain.FirewallTargetService, error)
	GetFirewallTargetService(string) (domain.FirewallTargetService, error)
	SetFirewallTargetService(context.Context, string, []string) (domain.FirewallTargetService, error)
	DeleteFirewallTargetService(context.Context, string) error
	ConfigureFirewallBypass(context.Context, []string, []string, bool, int) (domain.FirewallSettings, error)
}

type routingGeoStatus struct {
	Ready       bool   `json:"ready"`
	Updating    bool   `json:"updating"`
	LastResult  string `json:"last_result"`
	LastUpdated string `json:"last_updated,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Optional service extension for a host-owned runner. The concrete app.Service
// currently has no exported command runner. No command or path is client supplied.
type routingGeoService interface {
	RoutingGeoStatus(context.Context) (routingGeoStatus, error)
	RoutingGeoStart(context.Context) (routingGeoStatus, error)
}

type routingGeoHelper struct{}

func (routingGeoHelper) RoutingGeoStatus(ctx context.Context) (routingGeoStatus, error) {
	return runRoutingGeoHelper(ctx, "status")
}
func (routingGeoHelper) RoutingGeoStart(ctx context.Context) (routingGeoStatus, error) {
	return runRoutingGeoUpdate(ctx, runRoutingGeoHelper)
}

// The existing foreground update performs the complete transaction, including
// its EXIT cleanup and lock release. Only then read fresh status: update itself
// prints status while still owning the helper PID lock, so that output may say
// "updating" even after a successful database commit.
func runRoutingGeoUpdate(ctx context.Context, run func(context.Context, string) (routingGeoStatus, error)) (routingGeoStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if _, err := run(ctx, "update"); err != nil {
		return routingGeoStatus{}, err
	}
	if ctx.Err() != nil {
		return routingGeoStatus{}, panelJobError("geo_update_timeout")
	}
	return run(ctx, "status")
}

// Bound captured output without terminating the helper midway through rollback.
type routingGeoOutput struct {
	data     []byte
	overflow bool
}

func (b *routingGeoOutput) Write(value []byte) (int, error) {
	remaining := (64 << 10) - len(b.data)
	if len(value) > remaining {
		b.overflow = true
	}
	b.data = append(b.data, value[:min(len(value), remaining)]...)
	return len(value), nil
}

// Run one owned foreground process group. SIGTERM reaches both the shell and
// its active downloader/Xray child, allowing the helper's existing TERM/EXIT
// traps to roll back and release its lock. Run waits for exit and pipe closure;
// neither a detached worker nor a fire-and-forget cancellation can escape the
// health mutex. Do NOT set WaitDelay/SIGKILL: cleanup may need additional time
// after the work deadline, and forcibly killing it would bypass rollback.
func runRoutingGeoProcess(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 0
	// Do not inherit operator URL, executable or filesystem overrides from the
	// daemon/prototype environment. HTTP cannot supply any command arguments.
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C"}
	var output routingGeoOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, panelJobError("geo_update_timeout")
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, exec.ErrNotFound) {
			return nil, panelJobError("geo_runtime_unavailable")
		}
		return nil, panelJobError("geo_update_failed")
	}
	if output.overflow {
		return nil, panelJobError("geo_status_failed")
	}
	return output.data, nil
}

func runRoutingGeoHelper(ctx context.Context, action string) (routingGeoStatus, error) {
	if action != "status" && action != "update" {
		return routingGeoStatus{}, panelJobError("invalid_request")
	}
	if runtime.GOOS != "linux" {
		return routingGeoStatus{}, panelJobError("geo_runtime_unavailable")
	}
	if _, err := os.Stat("/etc/openwrt_release"); err != nil {
		return routingGeoStatus{}, panelJobError("geo_runtime_unavailable")
	}
	timeout := 30 * time.Second
	if action == "update" {
		timeout = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/libexec/fastlane-geodata", action)
	out, err := runRoutingGeoProcess(ctx, cmd)
	if err != nil {
		return routingGeoStatus{}, err
	}
	var status routingGeoStatus
	if len(out) > 64<<10 || json.Unmarshal(out, &status) != nil || status.LastResult == "" {
		return routingGeoStatus{}, panelJobError("geo_status_failed")
	}
	// Do not forward raw helper messages, filesystem paths or downloaded content.
	status.Error = ""
	if status.LastResult == "error" {
		status.Error = "geo_update_failed"
	}
	return status, nil
}

func (h *Handler) routingGeo() routingGeoService {
	if service, ok := h.service.(routingGeoService); ok {
		return service
	}
	return routingGeoHelper{}
}

// Keep polling support for host-provided adapters. The concrete OpenWrt adapter
// now runs foreground update and waits for cleanup even after cancellation.
func awaitRoutingGeo(ctx context.Context, geo routingGeoService, interval time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	status, err := geo.RoutingGeoStart(ctx)
	for err == nil && (status.Updating || status.LastResult == "updating") {
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return panelJobError("geo_update_timeout")
		case <-timer.C:
		}
		status, err = geo.RoutingGeoStatus(ctx)
	}
	if err != nil {
		return err
	}
	if !status.Ready || status.LastResult != "ok" {
		return panelJobError("geo_update_failed")
	}
	return nil
}

type routingCountryRequest struct {
	Country *string `json:"country"`
	Enabled *bool   `json:"enabled"`
}
type routingGroupRequest struct {
	Name    string   `json:"name"`
	Domains []string `json:"domains"`
	CIDRs   []string `json:"cidrs"`
}

var routingGroupName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func (h *Handler) routingPanelAPI(w http.ResponseWriter, r *http.Request) bool {
	const prefix = "/api/v1/routing/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return false
	}
	// Keep this boundary safe even if an integrator dispatches it too early.
	if h.loopbackOnly && !isLoopbackHost(r.Host) {
		h.writeError(w, 403, "host_rejected")
		return true
	}
	if !sameOrigin(r) || strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		h.writeError(w, 403, "origin_rejected")
		return true
	}
	if !h.authenticated(r) {
		h.writeError(w, 401, "auth_required")
		return true
	}
	path := strings.TrimPrefix(r.URL.Path, prefix)
	methods := map[string]string{"state": "GET", "country": "POST", "geo": "GET", "geo/update": "POST", "groups": "POST", "groups/edit": "POST", "groups/toggle": "POST", "groups/delete": "POST", "happ/preview": "POST", "happ/apply": "POST"}
	method, known := methods[path]
	if !known {
		h.writeError(w, 404, "not_found")
		return true
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		h.writeError(w, 405, "method_not_allowed")
		return true
	}
	if r.URL.RawQuery != "" {
		h.writeError(w, 400, "invalid_request")
		return true
	}
	if path == "happ/preview" || path == "happ/apply" {
		var input struct {
			Link string `json:"link"`
		}
		if decodeJSON(w, r, &input) != nil {
			h.writeError(w, 400, "invalid_request")
			return true
		}
		profile, err := parseRoutingHapp(input.Link)
		if err != nil {
			h.writeError(w, 400, "invalid_happ_profile")
			return true
		}
		if path == "happ/apply" {
			h.writeError(w, 422, "happ_atomic_apply_unavailable")
			return true
		}
		h.writeJSON(w, 200, profile)
		return true
	}
	if path == "geo" {
		status, err := h.routingGeo().RoutingGeoStatus(r.Context())
		if err != nil {
			status = routingGeoStatus{Error: "geo_runtime_unavailable"}
		}
		h.writeJSON(w, 200, status)
		return true
	}
	if path == "geo/update" {
		var input struct{}
		if decodeJSON(w, r, &input) != nil {
			h.writeError(w, 400, "invalid_request")
			return true
		}
		// The existing helper combines download, database replacement, validation
		// and Xray reload. Its PID lock excludes other Geo workers, not scheduler
		// health passes. Explicit Geo maintenance therefore owns the health lock
		// for the complete foreground update (15-minute work deadline plus cleanup),
		// not merely a later settings patch.
		h.startJob(w, "routing-geo", func(ctx context.Context) error { return awaitRoutingGeo(ctx, h.routingGeo(), 3*time.Second) })
		return true
	}
	s, ok := h.service.(routingPanelService)
	if !ok {
		h.writeError(w, 501, "feature_unavailable")
		return true
	}
	switch path {
	case "state":
		snapshot, err := h.service.Status()
		if err != nil {
			h.internalError(w, "routing state", err)
			return true
		}
		services, err := s.ListFirewallTargetServices()
		rulesError := ""
		if err != nil {
			rulesError = "routing_groups_unavailable"
		}
		geo, err := h.routingGeo().RoutingGeoStatus(r.Context())
		if err != nil {
			geo = routingGeoStatus{Error: "geo_runtime_unavailable"}
		}
		h.writeJSON(w, 200, struct {
			Country    domain.CountryRouting          `json:"country_routing"`
			Firewall   domain.FirewallSettings        `json:"firewall"`
			Services   []domain.FirewallTargetService `json:"services"`
			Geo        routingGeoStatus               `json:"geo"`
			RulesError string                         `json:"rules_error,omitempty"`
			Job        jobSnapshot                    `json:"job"`
		}{snapshot.Settings.CountryRouting, snapshot.Settings.Firewall, services, geo, rulesError, h.jobs.snapshot()})
	case "country":
		var input routingCountryRequest
		if decodeJSON(w, r, &input) != nil || input.Country == nil && input.Enabled == nil {
			h.writeError(w, 400, "invalid_request")
			return true
		}
		patch := map[string]string{}
		if input.Country != nil {
			code, err := domain.NormalizeCountryCode(*input.Country)
			if err != nil || code == "" {
				h.writeError(w, 400, "invalid_country")
				return true
			}
			patch["country-routing.country"] = code
		}
		if input.Enabled != nil {
			patch["country-routing.enabled"] = strconv.FormatBool(*input.Enabled)
		}
		// Enabling country routing may install Geo databases through the same
		// combined helper. Keep preparation and the settings commit in one
		// exclusive job; acquiring runExclusive again below would deadlock.
		h.startJob(w, "routing-country", func(ctx context.Context) error {
			current, err := h.service.Status()
			if err != nil {
				return err
			}
			enabled := current.Settings.CountryRouting.Enabled
			if input.Enabled != nil {
				enabled = *input.Enabled
			}
			if enabled {
				geo := h.routingGeo()
				status, err := geo.RoutingGeoStatus(ctx)
				if err != nil {
					return err
				}
				if !status.Ready || status.Updating {
					if err := awaitRoutingGeo(ctx, geo, 3*time.Second); err != nil {
						return err
					}
				}
			}
			_, err = h.service.PatchSettings(patch)
			return err
		})
	case "groups", "groups/edit":
		var input routingGroupRequest
		if decodeJSON(w, r, &input) != nil || !routingGroupName.MatchString(input.Name) || len(input.Domains)+len(input.CIDRs) > 4096 {
			h.writeError(w, 400, "invalid_request")
			return true
		}
		h.startJob(w, "routing-group", func(ctx context.Context) error {
			current, err := h.service.Status()
			if err != nil {
				return err
			}
			existing, found := domain.LookupFirewallTargetService(current.Settings.Firewall.TargetServiceCatalog, input.Name)
			if path == "groups" && found {
				return panelJobError("routing_group_exists")
			}
			if path == "groups/edit" && (!found || existing.ReadOnly) {
				return panelJobError("routing_group_not_editable")
			}
			selectors := slices.Concat(input.Domains, input.CIDRs)
			// Composite aliases cannot be silently discarded by this two-field editor.
			if found {
				selectors = slices.Concat(existing.Services, selectors)
			}
			if _, _, err := domain.ParseFirewallTargetDefinition(input.Name, selectors, current.Settings.Firewall.TargetServiceCatalog); err != nil {
				return panelJobError("invalid_routing_group")
			}
			if _, err := s.SetFirewallTargetService(ctx, input.Name, selectors); err != nil {
				return err
			}
			if path == "groups" {
				if err := h.routingGroupToggle(ctx, s, input.Name, true); err != nil {
					// The service may have persisted configuration before runtime failed.
					// Report that explicitly; never claim an atomic rollback we do not own.
					return panelJobError("routing_group_saved_activation_failed")
				}
			}
			return nil
		})
	case "groups/toggle", "groups/delete":
		var input struct {
			Name    string `json:"name"`
			Enabled *bool  `json:"enabled"`
		}
		if decodeJSON(w, r, &input) != nil || !routingGroupName.MatchString(input.Name) || path == "groups/toggle" && input.Enabled == nil || path == "groups/delete" && input.Enabled != nil {
			h.writeError(w, 400, "invalid_request")
			return true
		}
		h.startJob(w, "routing-group", func(ctx context.Context) error {
			existing, err := s.GetFirewallTargetService(input.Name)
			if err != nil || existing.ReadOnly {
				return panelJobError("routing_group_not_editable")
			}
			if path == "groups/toggle" {
				return h.routingGroupToggle(ctx, s, input.Name, *input.Enabled)
			}
			current, err := h.service.Status()
			if err != nil {
				return err
			}
			if slices.Contains(current.Settings.Firewall.Split.Bypass.Services, input.Name) {
				if err := h.routingGroupToggle(ctx, s, input.Name, false); err != nil {
					return err
				}
			}
			if err := s.DeleteFirewallTargetService(ctx, input.Name); err != nil {
				return panelJobError("routing_group_delete_failed")
			}
			return nil
		})
	}
	return true
}

func (h *Handler) routingGroupToggle(ctx context.Context, s routingPanelService, name string, enabled bool) error {
	current, err := h.service.Status()
	if err != nil {
		return err
	}
	fw := current.Settings.Firewall
	services := slices.DeleteFunc(slices.Clone(fw.Split.Bypass.Services), func(value string) bool { return value == name })
	if enabled {
		services = append(services, name)
	}
	// Exactly the LuCI `firewall set bypass` semantics, preserving unrelated
	// bypass selectors and excluded LAN sources from fresh service state.
	_, err = s.ConfigureFirewallBypass(ctx, slices.Concat(services, fw.Split.Bypass.Domains, fw.Split.Bypass.CIDRs), fw.Split.ExcludedSources, true, fw.TransparentPort)
	return err
}

// The LuCI v5 source explicitly disables partial HAPP application. There is no
// service method for an atomic direct/proxy/block import; retain that contract.
func parseRoutingHapp(link string) (map[string]any, error) {
	const prefix = "happ://routing/onadd/"
	if len(link) > 1<<20 || !strings.HasPrefix(strings.TrimSpace(link), prefix) {
		return nil, errors.New("invalid HAPP link")
	}
	encoded := strings.TrimPrefix(strings.TrimSpace(link), prefix)
	encoded = strings.NewReplacer("-", "+", "_", "/").Replace(strings.TrimRight(encoded, "="))
	raw, err := base64.RawStdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	var profile map[string]any
	if err := json.Unmarshal(raw, &profile); err != nil || profile == nil {
		return nil, errors.New("invalid HAPP profile")
	}
	if name, present := profile["Name"]; present {
		if _, ok := name.(string); !ok {
			return nil, errors.New("invalid name")
		}
	}
	for _, field := range []string{"DirectSites", "DirectIp", "ProxySites", "ProxyIp", "BlockSites", "BlockIp"} {
		if value, present := profile[field]; present {
			items, ok := value.([]any)
			if !ok || len(items) > 4096 {
				return nil, errors.New("invalid rule list")
			}
			for _, item := range items {
				if _, ok := item.(string); !ok {
					return nil, errors.New("invalid rule")
				}
			}
		}
	}
	return profile, nil
}
