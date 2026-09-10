package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
)

func TestQueueHealthCheckPersistsRequestAndProgress(t *testing.T) {
	t.Parallel()

	opts := &rootOptions{rootDir: t.TempDir()}
	queued, err := queueHealthCheck(opts, "sub-1")
	if err != nil {
		t.Fatalf("queue health check: %v", err)
	}
	if queued.Status != "queued" || queued.Scope != "sub-1" {
		t.Fatalf("unexpected queued progress: %+v", queued)
	}
	if _, err := os.Stat(healthCheckRequestPath(opts)); err != nil {
		t.Fatalf("request marker missing: %v", err)
	}
	scope, ok := consumeHealthCheckRequest(healthCheckRequestPath(opts))
	if !ok || scope != "sub-1" {
		t.Fatalf("consumed trigger=(%q,%t), want sub-1,true", scope, ok)
	}
	if _, err := os.Stat(healthCheckRequestPath(opts)); !os.IsNotExist(err) {
		t.Fatalf("request marker survived consume: %v", err)
	}
	stored, err := readHealthCheckProgress(healthCheckProgressPath(opts))
	if err != nil || stored.Status != "queued" || stored.Scope != "sub-1" {
		t.Fatalf("unexpected stored progress: %+v err=%v", stored, err)
	}
}

func TestCancelQueuedHealthCheckStopsRequestAndNextQueueClearsMarker(t *testing.T) {
	t.Parallel()

	opts := &rootOptions{rootDir: t.TempDir()}
	if _, err := queueHealthCheck(opts, "all"); err != nil {
		t.Fatalf("queue health check: %v", err)
	}
	cancelled, err := cancelHealthCheck(opts)
	if err != nil {
		t.Fatalf("cancel health check: %v", err)
	}
	if cancelled.Status != "cancelled" || cancelled.FinishedAt.IsZero() {
		t.Fatalf("unexpected cancelled progress: %+v", cancelled)
	}
	if _, err := os.Stat(healthCheckRequestPath(opts)); !os.IsNotExist(err) {
		t.Fatalf("request marker survived cancellation: %v", err)
	}
	if _, err := os.Stat(healthCheckCancelPath(opts)); err != nil {
		t.Fatalf("cancellation marker missing: %v", err)
	}

	queued, err := queueHealthCheck(opts, "sub-2")
	if err != nil {
		t.Fatalf("queue replacement health check: %v", err)
	}
	if queued.Status != "queued" || queued.Scope != "sub-2" {
		t.Fatalf("unexpected replacement progress: %+v", queued)
	}
	if _, err := os.Stat(healthCheckCancelPath(opts)); !os.IsNotExist(err) {
		t.Fatalf("stale cancellation marker survived new queue: %v", err)
	}
}

func TestCancelRunningHealthCheckCancelsWatcher(t *testing.T) {
	t.Parallel()

	opts := &rootOptions{rootDir: t.TempDir()}
	progress := healthCheckProgress{Status: "running", Scope: "all", StartedAt: time.Now().UTC()}
	if err := writeHealthCheckProgress(healthCheckProgressPath(opts), progress); err != nil {
		t.Fatalf("write running progress: %v", err)
	}
	runCtx, stop := watchHealthCheckCancellation(context.Background(), healthCheckCancelPath(opts))
	defer stop()

	cancelling, err := cancelHealthCheck(opts)
	if err != nil {
		t.Fatalf("cancel running health check: %v", err)
	}
	if cancelling.Status != "cancelling" {
		t.Fatalf("unexpected running cancellation state: %+v", cancelling)
	}
	select {
	case <-runCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("health-check worker did not observe cancellation")
	}
	if _, err := os.Stat(healthCheckCancelPath(opts)); !os.IsNotExist(err) {
		t.Fatalf("worker did not consume cancellation marker: %v", err)
	}
}

func TestHealthCheckFilesStayInExplicitTestRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	opts := &rootOptions{rootDir: root}
	if got := healthRoot(opts); got != root {
		t.Fatalf("health runtime root = %q, want explicit test root %q", got, root)
	}
	if got := healthCheckProgressPath(opts); got != filepath.Join(root, healthCheckProgressFile) {
		t.Fatalf("health progress path = %q", got)
	}
}

func TestHealthCheckFilesUseRAMForDefaultOpenWrtRoot(t *testing.T) {
	t.Parallel()

	if got := resolveHealthRoot("/etc/fastlane", "/etc/fastlane", true); got != healthCheckRuntimeRoot {
		t.Fatalf("OpenWrt health runtime root = %q, want %q", got, healthCheckRuntimeRoot)
	}
	if got := resolveHealthRoot("/tmp/test-root", "/etc/fastlane", true); got != "/tmp/test-root" {
		t.Fatalf("explicit OpenWrt test root = %q, want /tmp/test-root", got)
	}
}

func TestHealthCheckProgressRoundTripsPartialResults(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), healthCheckProgressFile)
	checkedAt := time.Date(2026, 9, 4, 20, 0, 0, 0, time.UTC)
	want := healthCheckProgress{
		Status: "running", Scope: "all", StartedAt: checkedAt, Total: 3, Done: 1, Healthy: 1,
		Results: map[string]domain.NodeHealth{
			"node-1": {NodeID: "node-1", Healthy: true, LastLatency: domain.NewDuration(42 * time.Millisecond), LastCheckedAt: checkedAt},
		},
	}
	if err := writeHealthCheckProgress(path, want); err != nil {
		t.Fatalf("write progress: %v", err)
	}
	got, err := readHealthCheckProgress(path)
	if err != nil {
		t.Fatalf("read progress: %v", err)
	}
	if got.Status != "running" || got.Done != 1 || got.Results["node-1"].LastLatency.Duration() != 42*time.Millisecond {
		t.Fatalf("unexpected progress round trip: %+v", got)
	}
}
