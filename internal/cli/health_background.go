package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/design-maestro/fastlane/internal/app"
	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/platform/openwrt"
)

const (
	healthCheckRequestFile  = "health-check.request"
	healthCheckProgressFile = "health-check-progress.json"
	healthCheckCancelFile   = "health-check.cancel"
	healthCheckRuntimeRoot  = "/tmp/fastlane-runtime"
)

type healthCheckProgress struct {
	Status     string                       `json:"status"`
	Scope      string                       `json:"scope"`
	StartedAt  time.Time                    `json:"started_at,omitempty"`
	FinishedAt time.Time                    `json:"finished_at,omitempty"`
	Total      int                          `json:"total"`
	Done       int                          `json:"done"`
	Healthy    int                          `json:"healthy"`
	Failed     int                          `json:"failed"`
	Error      string                       `json:"error,omitempty"`
	Results    map[string]domain.NodeHealth `json:"results,omitempty"`
}

func healthRoot(opts *rootOptions) string {
	persistentRoot := ""
	if opts != nil {
		persistentRoot = strings.TrimSpace(opts.rootDir)
	}
	if persistentRoot == "" {
		persistentRoot = openwrt.RootDir()
	}
	return resolveHealthRoot(persistentRoot, openwrt.RootDir(), openwrt.IsOpenWrt())
}

func resolveHealthRoot(persistentRoot, platformRoot string, isOpenWrt bool) string {
	if isOpenWrt && filepath.Clean(persistentRoot) == filepath.Clean(platformRoot) {
		return healthCheckRuntimeRoot
	}
	return persistentRoot
}

func healthCheckRequestPath(opts *rootOptions) string {
	return filepath.Join(healthRoot(opts), healthCheckRequestFile)
}

func healthCheckProgressPath(opts *rootOptions) string {
	return filepath.Join(healthRoot(opts), healthCheckProgressFile)
}

func healthCheckCancelPath(opts *rootOptions) string {
	return filepath.Join(healthRoot(opts), healthCheckCancelFile)
}

func queueHealthCheck(opts *rootOptions, scope string) (healthCheckProgress, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "all"
	}
	progressPath := healthCheckProgressPath(opts)
	if current, err := readHealthCheckProgress(progressPath); err == nil && (current.Status == "running" || current.Status == "cancelling") {
		return current, nil
	}
	_ = os.Remove(healthCheckCancelPath(opts))
	progress := healthCheckProgress{Status: "queued", Scope: scope}
	if err := writeHealthCheckProgress(progressPath, progress); err != nil {
		return healthCheckProgress{}, err
	}
	requestPath := healthCheckRequestPath(opts)
	if err := os.MkdirAll(filepath.Dir(requestPath), 0o700); err != nil {
		return healthCheckProgress{}, fmt.Errorf("create health-check directory: %w", err)
	}
	temporary := requestPath + ".tmp"
	if err := os.WriteFile(temporary, []byte(scope+"\n"), 0o600); err != nil {
		return healthCheckProgress{}, fmt.Errorf("write health-check request: %w", err)
	}
	if err := os.Rename(temporary, requestPath); err != nil {
		return healthCheckProgress{}, fmt.Errorf("queue health-check request: %w", err)
	}
	return progress, nil
}

func cancelHealthCheck(opts *rootOptions) (healthCheckProgress, error) {
	progressPath := healthCheckProgressPath(opts)
	progress, err := readHealthCheckProgress(progressPath)
	if err != nil {
		return healthCheckProgress{}, err
	}
	if progress.Status != "queued" && progress.Status != "running" && progress.Status != "cancelling" {
		return progress, nil
	}

	wasQueued := progress.Status == "queued"
	_ = os.Remove(healthCheckRequestPath(opts))
	cancelPath := healthCheckCancelPath(opts)
	if err := os.MkdirAll(filepath.Dir(cancelPath), 0o700); err != nil {
		return healthCheckProgress{}, fmt.Errorf("create health-check directory: %w", err)
	}
	temporary := cancelPath + ".tmp"
	if err := os.WriteFile(temporary, []byte("cancel\n"), 0o600); err != nil {
		return healthCheckProgress{}, fmt.Errorf("write health-check cancellation: %w", err)
	}
	if err := os.Rename(temporary, cancelPath); err != nil {
		return healthCheckProgress{}, fmt.Errorf("queue health-check cancellation: %w", err)
	}

	if wasQueued {
		progress.Status = "cancelled"
		progress.FinishedAt = time.Now().UTC()
	} else {
		progress.Status = "cancelling"
	}
	if err := writeHealthCheckProgress(progressPath, progress); err != nil {
		return healthCheckProgress{}, err
	}
	return progress, nil
}

func consumeHealthCheckRequest(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	scope := strings.TrimSpace(string(data))
	if scope == "" {
		scope = "all"
	}
	return scope, true
}

func readHealthCheckProgress(path string) (healthCheckProgress, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return healthCheckProgress{Status: "idle"}, nil
		}
		return healthCheckProgress{}, err
	}
	var progress healthCheckProgress
	if err := json.Unmarshal(data, &progress); err != nil {
		return healthCheckProgress{}, fmt.Errorf("decode health-check progress: %w", err)
	}
	if progress.Status == "" {
		progress.Status = "idle"
	}
	return progress, nil
}

func writeHealthCheckProgress(path string, progress healthCheckProgress) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create health-check directory: %w", err)
	}
	data, err := json.MarshalIndent(progress, "", "  ")
	if err != nil {
		return fmt.Errorf("encode health-check progress: %w", err)
	}
	data = append(data, '\n')
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return fmt.Errorf("write health-check progress: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("replace health-check progress: %w", err)
	}
	return nil
}

func runTrackedHealthCheck(ctx context.Context, opts *rootOptions, scope string, connect bool) error {
	if !connect {
		// A cancelled queued request can leave a marker behind when the daemon
		// never consumed it. It must not cancel an unrelated scheduled pass.
		_ = os.Remove(healthCheckCancelPath(opts))
		scope = scheduledHealthScope(opts)
	}
	scope = normalizeHealthScope(scope)
	startedAt := time.Now().UTC()
	total := healthCheckNodeCount(opts, scope)
	progress := healthCheckProgress{Status: "running", Scope: scope, StartedAt: startedAt, Total: total, Results: make(map[string]domain.NodeHealth)}
	_ = writeHealthCheckProgress(healthCheckProgressPath(opts), progress)
	runCtx, stopCancellationWatch := watchHealthCheckCancellation(ctx, healthCheckCancelPath(opts))
	defer stopCancellationWatch()
	runCtx = app.WithAutoHealthProgress(runCtx, func(health domain.NodeHealth) {
		progress.Results[health.NodeID] = health
		progress.Done = len(progress.Results)
		progress.Healthy = 0
		for _, observed := range progress.Results {
			if observed.Healthy && observed.LastLatency.Duration() > 0 && observed.ConsecutiveFailures == 0 && strings.TrimSpace(observed.LastFailureReason) == "" {
				progress.Healthy++
			}
		}
		progress.Failed = progress.Done - progress.Healthy
		_ = writeHealthCheckProgress(healthCheckProgressPath(opts), progress)
	})

	var runErr error
	if connect {
		_, runErr = opts.service.ConnectAuto(runCtx, scope)
	} else {
		runErr = opts.service.RunAutoHealthCheck(runCtx)
	}
	progress.FinishedAt = time.Now().UTC()
	if errors.Is(runErr, context.Canceled) || errors.Is(runCtx.Err(), context.Canceled) {
		progress.Status = "cancelled"
		progress.Error = ""
		runErr = nil
	} else if runErr != nil {
		progress.Status = "failed"
		progress.Error = runErr.Error()
	} else {
		progress.Status = "completed"
	}
	_ = writeHealthCheckProgress(healthCheckProgressPath(opts), progress)
	return runErr
}

func watchHealthCheckCancellation(ctx context.Context, cancelPath string) (context.Context, func()) {
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if consumeHealthCheckCancellation(cancelPath) {
			cancel()
			return
		}
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if consumeHealthCheckCancellation(cancelPath) {
					cancel()
					return
				}
			}
		}
	}()

	return runCtx, func() {
		cancel()
		<-done
		_ = os.Remove(cancelPath)
	}
}

func consumeHealthCheckCancellation(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	err := os.Remove(path)
	return err == nil || errors.Is(err, os.ErrNotExist)
}

func scheduledHealthScope(opts *rootOptions) string {
	status, err := opts.service.Status()
	if err != nil || status.State.Mode != domain.SelectionModeAuto || status.State.ActiveSubscriptionID == "" {
		return "none"
	}
	if status.State.AutoScope == "all" {
		return "all"
	}
	return status.State.ActiveSubscriptionID
}

func healthCheckNodeCount(opts *rootOptions, scope string) int {
	subscriptions, err := opts.service.ListSubscriptions()
	if err != nil {
		return 0
	}
	settings, err := opts.service.GetSettings()
	if err != nil {
		return 0
	}
	now := time.Now().UTC()
	total := 0
	for _, sub := range subscriptions {
		if sub.IsExpired(now) || (scope != "" && scope != "all" && sub.ID != scope) {
			continue
		}
		for _, node := range sub.Nodes {
			if !domain.IsNodeExcludedFromAuto(settings, sub.ID, node) {
				total++
			}
		}
	}
	return total
}

func normalizeHealthScope(scope string) string {
	if strings.TrimSpace(scope) == "" {
		return "all"
	}
	return strings.TrimSpace(scope)
}
