package managementhttp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/app"
	"github.com/design-maestro/fastlane/internal/domain"
)

const testAccessToken = "0123456789abcdef0123456789abcdef"

type fakeService struct {
	mu                sync.Mutex
	snapshot          app.StatusSnapshot
	subscriptions     []domain.Subscription
	healthStarted     chan struct{}
	healthRelease     chan struct{}
	healthContextDone bool
	removedSubID      string
	removedNodeSubID  string
	removedNodeID     string
}

func (f *fakeService) Status() (app.StatusSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshot, nil
}

func (f *fakeService) ListSubscriptions() ([]domain.Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.Subscription(nil), f.subscriptions...), nil
}

func (f *fakeService) AddSubscription(context.Context, app.AddSubscriptionRequest) (domain.Subscription, error) {
	return domain.Subscription{}, nil
}

func (f *fakeService) RemoveSubscription(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removedSubID = id
	return nil
}

func (f *fakeService) RemoveSubscriptionNode(_ context.Context, subscriptionID, nodeID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removedNodeSubID = subscriptionID
	f.removedNodeID = nodeID
	return nil
}

func (f *fakeService) RefreshAll(context.Context) ([]domain.Subscription, error) {
	return nil, nil
}

func (f *fakeService) ConnectManual(context.Context, string, string) error { return nil }

func (f *fakeService) ConnectAuto(context.Context, string) (domain.Node, error) {
	return domain.Node{}, nil
}

func (f *fakeService) Disconnect(context.Context) error { return nil }

func (f *fakeService) RunAutoHealthCheck(ctx context.Context) error {
	if f.healthStarted != nil {
		close(f.healthStarted)
	}
	if f.healthRelease == nil {
		return nil
	}
	select {
	case <-f.healthRelease:
		return nil
	case <-ctx.Done():
		f.mu.Lock()
		f.healthContextDone = true
		f.mu.Unlock()
		return ctx.Err()
	}
}

func (f *fakeService) PatchSettings(map[string]string) (domain.Settings, error) {
	return domain.DefaultSettings(), nil
}

func TestHandlerProtectsStateAndDoesNotExposeSecrets(t *testing.T) {
	t.Parallel()

	settings := domain.DefaultSettings()
	service := &fakeService{
		snapshot: app.StatusSnapshot{Settings: settings, State: domain.DefaultRuntimeState()},
		subscriptions: []domain.Subscription{{
			ID:           "sub-1",
			DisplayName:  "Provider",
			ProviderName: "Provider",
			Source:       "https://provider.example/sub?token=private-subscription-token",
			Nodes: []domain.Node{{
				ID:             "node-1",
				SubscriptionID: "sub-1",
				Name:           "Safe name",
				Address:        "vpn.example",
				Port:           443,
				Protocol:       domain.ProtocolVLESS,
				UUID:           "private-node-uuid",
				PublicKey:      "private-reality-key",
			}},
		}},
	}
	handler := mustHandler(t, context.Background(), service, HandlerConfig{AccessToken: testAccessToken})

	unauthorized := request(handler, http.MethodGet, "/api/v1/state", "", "", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	authorized := request(handler, http.MethodGet, "/api/v1/state", "", testAccessToken, "")
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized status = %d: %s", authorized.Code, authorized.Body.String())
	}
	body := authorized.Body.String()
	for _, secret := range []string{"private-subscription-token", "private-node-uuid", "private-reality-key"} {
		if strings.Contains(body, secret) {
			t.Fatalf("state response exposed %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, `"address":"vpn.example"`) {
		t.Fatalf("state response omitted safe node summary: %s", body)
	}
}

func TestHandlerLoginUsesStrictHTTPOnlyCookieAndRejectsCrossOrigin(t *testing.T) {
	t.Parallel()

	handler := mustHandler(t, context.Background(), &fakeService{}, HandlerConfig{AccessToken: testAccessToken})

	crossOrigin := request(handler, http.MethodPost, "/api/v1/session", `{"token":"`+testAccessToken+`"}`, "", "http://evil.example")
	if crossOrigin.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", crossOrigin.Code)
	}

	login := request(handler, http.MethodPost, "/api/v1/session", `{"token":"`+testAccessToken+`"}`, "", "http://router.local")
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d: %s", login.Code, login.Body.String())
	}
	cookie := login.Header().Get("Set-Cookie")
	for _, attribute := range []string{"fastlane_session=", "HttpOnly", "SameSite=Strict"} {
		if !strings.Contains(cookie, attribute) {
			t.Fatalf("cookie missing %q: %s", attribute, cookie)
		}
	}
}

func TestHandlerRejectsUnknownJSONFields(t *testing.T) {
	t.Parallel()

	handler := mustHandler(t, context.Background(), &fakeService{}, HandlerConfig{})
	response := request(handler, http.MethodPost, "/api/v1/connect", `{"mode":"auto","extra":true}`, "", "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "invalid_request") {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
}

func TestHealthCheckContinuesAfterRequestEnds(t *testing.T) {
	t.Parallel()

	baseContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := &fakeService{healthStarted: make(chan struct{}), healthRelease: make(chan struct{})}
	handler := mustHandler(t, baseContext, service, HandlerConfig{})

	requestContext, stopRequest := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/health-check", nil).WithContext(requestContext)
	req.Host = "router.local"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	stopRequest()

	select {
	case <-service.healthStarted:
	case <-time.After(time.Second):
		t.Fatal("health check did not start")
	}

	service.mu.Lock()
	requestCancellationReachedJob := service.healthContextDone
	service.mu.Unlock()
	if requestCancellationReachedJob {
		t.Fatal("request cancellation stopped the background job")
	}

	conflict := request(handler, http.MethodPost, "/api/v1/jobs/health-check", "", "", "")
	if conflict.Code != http.StatusConflict {
		t.Fatalf("second job status = %d: %s", conflict.Code, conflict.Body.String())
	}

	close(service.healthRelease)
	waitForJob(t, handler, false)
	state := request(handler, http.MethodGet, "/api/v1/state", "", "", "")
	if state.Code != http.StatusOK || !strings.Contains(state.Body.String(), `"succeeded":true`) {
		t.Fatalf("completed job missing from state: %s", state.Body.String())
	}
}

func TestHandlerRemovesIndividualNode(t *testing.T) {
	t.Parallel()

	service := &fakeService{}
	handler := mustHandler(t, context.Background(), service, HandlerConfig{})
	response := request(handler, http.MethodDelete, "/api/v1/subscriptions/sub%201/nodes/node%201", "", "", "")
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	waitForJob(t, handler, false)

	service.mu.Lock()
	defer service.mu.Unlock()
	if service.removedNodeSubID != "sub 1" || service.removedNodeID != "node 1" {
		t.Fatalf("removed node = %q/%q", service.removedNodeSubID, service.removedNodeID)
	}
}

func mustHandler(t *testing.T, ctx context.Context, service Service, config HandlerConfig) http.Handler {
	t.Helper()
	handler, err := NewHandler(ctx, service, config)
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}
	return handler
}

func request(handler http.Handler, method, path, body, bearer, origin string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Host = "router.local"
	req.RemoteAddr = "192.0.2.10:12345"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func waitForJob(t *testing.T, handler http.Handler, running bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		response := request(handler, http.MethodGet, "/api/v1/state", "", "", "")
		needle := `"running":false`
		if running {
			needle = `"running":true`
		}
		if strings.Contains(response.Body.String(), needle) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job did not reach running=%t", running)
}
