package managementhttp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/design-maestro/fastlane/internal/app"
	"github.com/design-maestro/fastlane/internal/domain"
	apiresponse "github.com/design-maestro/fastlane/pkg/api"
)

const (
	maxRequestBodyBytes = 5 << 20
	sessionLifetime     = 12 * time.Hour
	loginWindow         = time.Minute
	maxLoginAttempts    = 10
	maxLoginClients     = 1024
)

// Service is the existing application boundary exposed by the management API.
// Implementations must keep all persistence and runtime locking inside the
// application layer.
type Service interface {
	Status() (app.StatusSnapshot, error)
	ListSubscriptions() ([]domain.Subscription, error)
	AddSubscription(context.Context, app.AddSubscriptionRequest) (domain.Subscription, error)
	RemoveSubscription(context.Context, string) error
	RemoveSubscriptionNode(context.Context, string, string) error
	RefreshAll(context.Context) ([]domain.Subscription, error)
	ConnectManual(context.Context, string, string) error
	ConnectAuto(context.Context, string) (domain.Node, error)
	Disconnect(context.Context) error
	RunAutoHealthCheck(context.Context) error
	PatchSettings(map[string]string) (domain.Settings, error)
}

// HandlerConfig controls HTTP authentication without changing application state.
type HandlerConfig struct {
	AccessToken  string
	LoopbackOnly bool
	Logger       *slog.Logger
	RunExclusive func(context.Context, func(context.Context) error) error
	HealthCheck  func(context.Context) error
}

type Handler struct {
	service      Service
	baseContext  context.Context
	accessToken  string
	loopbackOnly bool
	sessionToken string
	logger       *slog.Logger
	runExclusive func(context.Context, func(context.Context) error) error
	healthCheck  func(context.Context) error
	jobs         jobTracker
	loginMu      sync.Mutex
	loginByIP    map[string][]time.Time
	now          func() time.Time
}

type stateResponse struct {
	Status        apiresponse.StatusResponse        `json:"status"`
	Subscriptions []apiresponse.SubscriptionSummary `json:"subscriptions"`
	Job           jobSnapshot                       `json:"job"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// NewHandler creates an API-only HTTP handler. It never serves third-party UI
// assets and never creates a second Fast Lane service.
func NewHandler(ctx context.Context, service Service, config HandlerConfig) (http.Handler, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if service == nil {
		return nil, fmt.Errorf("management service is required")
	}

	sessionBytes := make([]byte, 32)
	if _, err := rand.Read(sessionBytes); err != nil {
		return nil, fmt.Errorf("create management session secret: %w", err)
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Handler{
		service:      service,
		baseContext:  ctx,
		accessToken:  strings.TrimSpace(config.AccessToken),
		loopbackOnly: config.LoopbackOnly,
		sessionToken: hex.EncodeToString(sessionBytes),
		logger:       logger,
		runExclusive: config.RunExclusive,
		healthCheck:  config.HealthCheck,
		loginByIP:    make(map[string][]time.Time),
		now:          time.Now,
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	w.Header().Set("Cache-Control", "no-store")

	if h.loopbackOnly && !isLoopbackHost(r.Host) {
		h.writeError(w, http.StatusForbidden, "host_rejected")
		return
	}
	if !sameOrigin(r) || strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
		h.writeError(w, http.StatusForbidden, "origin_rejected")
		return
	}

	if r.Method == http.MethodPost && r.URL.Path == "/api/v1/session" {
		h.login(w, r)
		return
	}
	if !h.authenticated(r) {
		h.writeError(w, http.StatusUnauthorized, "auth_required")
		return
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/state":
		h.getState(w)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/jobs/health-check":
		check := h.healthCheck
		if check == nil {
			check = h.service.RunAutoHealthCheck
		}
		h.startJob(w, "health-check", check)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/jobs/refresh":
		h.startJob(w, "refresh", func(ctx context.Context) error {
			_, err := h.service.RefreshAll(ctx)
			return err
		})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/connect":
		h.connect(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/disconnect":
		h.startJob(w, "disconnect", h.service.Disconnect)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/subscriptions":
		h.addSubscription(w, r)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/subscriptions/"):
		h.removeSubscription(w, r)
	case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/settings":
		h.patchSettings(w, r)
	default:
		h.writeError(w, http.StatusNotFound, "not_found")
	}
}

func (h *Handler) getState(w http.ResponseWriter) {
	snapshot, err := h.service.Status()
	if err != nil {
		h.internalError(w, "read status", err)
		return
	}
	subscriptions, err := h.service.ListSubscriptions()
	if err != nil {
		h.internalError(w, "list subscriptions", err)
		return
	}

	h.writeJSON(w, http.StatusOK, stateResponse{
		Status:        apiresponse.StatusResponseFromSnapshot(snapshot),
		Subscriptions: apiresponse.SubscriptionSummariesFromDomainWithRefresh(subscriptions, true, snapshot.Settings.RefreshInterval),
		Job:           h.jobs.snapshot(),
	})
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Mode           string `json:"mode"`
		SubscriptionID string `json:"subscription_id"`
		NodeID         string `json:"node_id"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	switch strings.ToLower(strings.TrimSpace(request.Mode)) {
	case "auto":
		scope := strings.TrimSpace(request.SubscriptionID)
		h.startJob(w, "connect-auto", func(ctx context.Context) error {
			_, err := h.service.ConnectAuto(ctx, scope)
			return err
		})
	case "manual":
		subscriptionID := strings.TrimSpace(request.SubscriptionID)
		nodeID := strings.TrimSpace(request.NodeID)
		if subscriptionID == "" || nodeID == "" {
			h.writeError(w, http.StatusBadRequest, "subscription_id_and_node_id_required")
			return
		}
		h.startJob(w, "connect-manual", func(ctx context.Context) error {
			return h.service.ConnectManual(ctx, subscriptionID, nodeID)
		})
	default:
		h.writeError(w, http.StatusBadRequest, "invalid_connection_mode")
	}
}

func (h *Handler) addSubscription(w http.ResponseWriter, r *http.Request) {
	var request struct {
		URL      string `json:"url"`
		Raw      string `json:"raw"`
		Name     string `json:"name"`
		HWID     string `json:"hwid"`
		FileName string `json:"file_name"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(request.URL) == "" && strings.TrimSpace(request.Raw) == "" {
		h.writeError(w, http.StatusBadRequest, "subscription_source_required")
		return
	}

	payload := app.AddSubscriptionRequest{
		URL:      request.URL,
		Raw:      request.Raw,
		Name:     request.Name,
		HWID:     request.HWID,
		FileName: request.FileName,
	}
	h.startJob(w, "add-subscription", func(ctx context.Context) error {
		_, err := h.service.AddSubscription(ctx, payload)
		return err
	})
}

func (h *Handler) removeSubscription(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/subscriptions/")
	parts := strings.Split(rest, "/")
	for idx := range parts {
		decoded, err := url.PathUnescape(parts[idx])
		if err != nil || strings.TrimSpace(decoded) == "" {
			h.writeError(w, http.StatusBadRequest, "invalid_resource_id")
			return
		}
		parts[idx] = decoded
	}

	switch {
	case len(parts) == 1:
		id := parts[0]
		h.startJob(w, "remove-subscription", func(ctx context.Context) error {
			return h.service.RemoveSubscription(ctx, id)
		})
	case len(parts) == 3 && parts[1] == "nodes":
		subscriptionID := parts[0]
		nodeID := parts[2]
		h.startJob(w, "remove-node", func(ctx context.Context) error {
			return h.service.RemoveSubscriptionNode(ctx, subscriptionID, nodeID)
		})
	default:
		h.writeError(w, http.StatusNotFound, "not_found")
	}
}

func (h *Handler) patchSettings(w http.ResponseWriter, r *http.Request) {
	var values map[string]string
	if err := decodeJSON(w, r, &values); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(values) == 0 {
		h.writeError(w, http.StatusBadRequest, "settings_patch_required")
		return
	}

	settings, err := h.service.PatchSettings(values)
	if err != nil {
		h.internalError(w, "patch settings", err)
		return
	}
	h.writeJSON(w, http.StatusOK, settings)
}

func (h *Handler) startJob(w http.ResponseWriter, kind string, run func(context.Context) error) {
	job, ok := h.jobs.start(h.baseContext, kind, func(ctx context.Context) error {
		var err error
		if h.runExclusive != nil {
			err = h.runExclusive(ctx, run)
		} else {
			err = run(ctx)
		}
		if err != nil {
			h.logger.Error("management job failed", "kind", kind, "error", err.Error())
		}
		return err
	})
	if !ok {
		h.writeError(w, http.StatusConflict, "job_already_running")
		return
	}
	h.writeJSON(w, http.StatusAccepted, job)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	if !h.allowLogin(r) {
		h.writeError(w, http.StatusTooManyRequests, "auth_rate_limited")
		return
	}

	var request struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.accessToken != "" && !safeEqual(request.Token, h.accessToken) {
		h.writeError(w, http.StatusUnauthorized, "auth_required")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "fastlane_session",
		Value:    h.sessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionLifetime.Seconds()),
	})
	h.writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) authenticated(r *http.Request) bool {
	if h.accessToken == "" {
		return true
	}
	if cookie, err := r.Cookie("fastlane_session"); err == nil && safeEqual(cookie.Value, h.sessionToken) {
		return true
	}
	const prefix = "Bearer "
	authorization := r.Header.Get("Authorization")
	return strings.HasPrefix(authorization, prefix) && safeEqual(strings.TrimPrefix(authorization, prefix), h.accessToken)
}

func (h *Handler) allowLogin(r *http.Request) bool {
	clientIP := remoteIP(r.RemoteAddr)
	now := h.now()
	cutoff := now.Add(-loginWindow)

	h.loginMu.Lock()
	defer h.loginMu.Unlock()

	if len(h.loginByIP) >= maxLoginClients {
		for key, attempts := range h.loginByIP {
			if len(attempts) == 0 || attempts[len(attempts)-1].Before(cutoff) {
				delete(h.loginByIP, key)
			}
		}
		if _, known := h.loginByIP[clientIP]; !known && len(h.loginByIP) >= maxLoginClients {
			return false
		}
	}
	attempts := h.loginByIP[clientIP]
	kept := attempts[:0]
	for _, attempt := range attempts {
		if attempt.After(cutoff) {
			kept = append(kept, attempt)
		}
	}
	if len(kept) >= maxLoginAttempts {
		h.loginByIP[clientIP] = kept
		return false
	}
	h.loginByIP[clientIP] = append(kept, now)
	return true
}

func (h *Handler) internalError(w http.ResponseWriter, operation string, err error) {
	h.logger.Error("management request failed", "operation", operation, "error", err.Error())
	h.writeError(w, http.StatusInternalServerError, "operation_failed")
}

func (h *Handler) writeError(w http.ResponseWriter, status int, code string) {
	h.writeJSON(w, status, errorResponse{Error: code})
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return errors.New("json_required")
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid_request")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("invalid_request")
	}
	return nil
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
}

func sameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

func isLoopbackHost(hostPort string) bool {
	host := hostPort
	if parsedHost, _, err := net.SplitHostPort(hostPort); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err == nil && host != "" {
		return host
	}
	if remoteAddr == "" {
		return "unknown"
	}
	return remoteAddr
}

func safeEqual(left, right string) bool {
	leftHash := sha256.Sum256([]byte(left))
	rightHash := sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftHash[:], rightHash[:]) == 1
}
