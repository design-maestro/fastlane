package managementhttp

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/speedtest"
)

// The UI stores no connection credentials and never invokes arbitrary commands.
type vpnPanelService interface {
	SetSetting(string, string) (domain.Settings, error)
	RefreshSubscription(context.Context, string) (domain.Subscription, error)
	InspectURLTest(context.Context, string, string) (speedtest.URLTestResult, error)
}
type panelProbeResults struct {
	sync.Mutex
	values map[string]panelProbeResult
}
type panelProbeResult struct {
	speedtest.URLTestResult
	Success bool `json:"success"`
}

func (h *Handler) vpnPanelAPI(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/v1/vpn/") {
		return false
	}
	s, ok := h.service.(vpnPanelService)
	if !ok {
		h.writeError(w, 501, "feature_unavailable")
		return true
	}
	if r.URL.Path == "/api/v1/vpn/probes" && r.Method == http.MethodGet {
		h.probes.Lock()
		defer h.probes.Unlock()
		h.writeJSON(w, 200, h.probes.values)
		return true
	}
	if r.Method != http.MethodPost {
		h.writeError(w, 405, "method_not_allowed")
		return true
	}
	var input struct {
		SubscriptionID string `json:"subscription_id"`
		NodeID         string `json:"node_id"`
		Hidden         bool   `json:"hidden"`
	}
	if decodeJSON(w, r, &input) != nil || input.SubscriptionID == "" {
		h.writeError(w, 400, "invalid_request")
		return true
	}
	switch r.URL.Path {
	case "/api/v1/vpn/refresh":
		h.startJob(w, "refresh", func(ctx context.Context) error {
			_, err := s.RefreshSubscription(ctx, input.SubscriptionID)
			return err
		})
	case "/api/v1/vpn/hidden", "/api/v1/vpn/check":
		if input.NodeID == "" {
			h.writeError(w, 400, "invalid_request")
			return true
		}
		h.startJob(w, "vpn-node", func(ctx context.Context) error {
			subs, err := h.service.ListSubscriptions()
			if err != nil {
				return err
			}
			found := false
			for _, sub := range subs {
				if sub.ID == input.SubscriptionID {
					if r.URL.Path == "/api/v1/vpn/check" && sub.IsExpired(h.now()) {
						return panelJobError("subscription_expired")
					}
					for _, node := range sub.Nodes {
						if node.ID == input.NodeID {
							found = true
						}
					}
				}
			}
			if !found {
				return panelJobError("server_not_found")
			}
			if r.URL.Path == "/api/v1/vpn/check" {
				result, err := s.InspectURLTest(ctx, input.SubscriptionID, input.NodeID)
				h.probes.Lock()
				defer h.probes.Unlock()
				key := input.SubscriptionID + "/" + input.NodeID
				if h.probes.values == nil {
					h.probes.values = make(map[string]panelProbeResult)
				}
				if err != nil {
					h.probes.values[key] = panelProbeResult{URLTestResult: speedtest.URLTestResult{SubscriptionID: input.SubscriptionID, NodeID: input.NodeID, CheckedAt: h.now().UTC()}, Success: false}
					return err
				}
				h.probes.values[key] = panelProbeResult{URLTestResult: result, Success: true}
				return nil
			}
			status, err := h.service.Status()
			if err != nil {
				return err
			}
			key := input.SubscriptionID + "/" + input.NodeID
			values := slices.DeleteFunc(slices.Clone(status.Settings.AutoExcludedNodes), func(v string) bool { return v == key || v == input.NodeID })
			if input.Hidden {
				values = append(values, key)
			}
			_, err = s.SetSetting("auto.excluded-nodes", strings.Join(values, ","))
			return err
		})
	default:
		h.writeError(w, 404, "not_found")
	}
	return true
}
