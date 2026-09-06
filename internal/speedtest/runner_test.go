package speedtest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestDefaultTransferProfileUsesAdaptiveMultiStreamSample(t *testing.T) {
	t.Parallel()

	if defaultWarmupDuration <= 0 {
		t.Fatalf("expected positive warm-up duration, got %s", defaultWarmupDuration)
	}
	if defaultDownloadDuration < 4*time.Second {
		t.Fatalf("expected download phase to run at least 4s, got %s", defaultDownloadDuration)
	}
	if defaultUploadDuration < 3*time.Second {
		t.Fatalf("expected upload phase to run at least 3s, got %s", defaultUploadDuration)
	}
	if defaultWarmupStreams < 2 {
		t.Fatalf("expected at least 2 warm-up streams, got %d", defaultWarmupStreams)
	}
	if defaultMeasureStreams < 4 {
		t.Fatalf("expected at least 4 measure streams, got %d", defaultMeasureStreams)
	}
}

func TestWarmGETLatencyUsesSecondRequest(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			time.Sleep(45 * time.Millisecond)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if _, err := measureSingleLatency(context.Background(), server.Client(), server.URL); err != nil {
		t.Fatalf("warm up request: %v", err)
	}
	latency, err := measureSingleLatency(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("measured request: %v", err)
	}
	if requests.Load() != 2 {
		t.Fatalf("expected two GET requests, got %d", requests.Load())
	}
	if latency >= 40*time.Millisecond {
		t.Fatalf("expected the measured GET to exclude warm-up delay, got %s", latency)
	}
}

func TestZeroReaderFillsBufferWithZeros(t *testing.T) {
	t.Parallel()

	buf := bytes.Repeat([]byte{0xff}, 64)
	n, err := zeroReader{}.Read(buf)
	if err != nil {
		t.Fatalf("read zero stream: %v", err)
	}
	if n != len(buf) {
		t.Fatalf("expected %d bytes, got %d", len(buf), n)
	}
	if !bytes.Equal(buf, make([]byte, len(buf))) {
		t.Fatal("expected zero-filled upload buffer")
	}
}

func TestTransferDownloadWindowUsesParallelWorkers(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests int
	var inFlight int
	var maxInFlight int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		requests++
		mu.Unlock()
		defer func() {
			mu.Lock()
			inFlight--
			mu.Unlock()
		}()
		size := r.URL.Query().Get("bytes")
		if size != "1048576" {
			t.Fatalf("expected 1 MiB download chunk, got %q", size)
		}
		time.Sleep(20 * time.Millisecond)
		_, _ = io.CopyN(w, zeroReader{}, downloadChunkBytes)
	}))
	defer server.Close()

	total, duration, err := transferDownloadWindow(
		context.Background(),
		server.Client(),
		server.URL+"?bytes=%d",
		120*time.Millisecond,
		downloadChunkBytes,
		4,
	)
	if err != nil {
		t.Fatalf("transfer download: %v", err)
	}
	if total <= 0 {
		t.Fatalf("expected positive downloaded bytes, got %d", total)
	}
	mu.Lock()
	requestCount := requests
	peakInFlight := maxInFlight
	mu.Unlock()
	if requestCount < 4 {
		t.Fatalf("expected multiple download requests, got %d", requestCount)
	}
	if peakInFlight < 2 {
		t.Fatalf("expected parallel download workers, got max concurrency %d", peakInFlight)
	}
	if duration <= 0 {
		t.Fatalf("expected positive duration, got %s", duration)
	}
}

func TestTransferUploadWindowUsesParallelWorkers(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests int
	var uploaded int64
	var inFlight int
	var maxInFlight int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		requests++
		mu.Unlock()
		defer func() {
			mu.Lock()
			inFlight--
			mu.Unlock()
		}()
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			t.Fatalf("read upload body: %v", err)
		}
		mu.Lock()
		uploaded += n
		mu.Unlock()
		if r.ContentLength != uploadChunkBytes {
			t.Fatalf("expected 512 KiB upload chunk, got %d", r.ContentLength)
		}
		time.Sleep(20 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	total, duration, err := transferUploadWindow(
		context.Background(),
		server.Client(),
		server.URL,
		120*time.Millisecond,
		uploadChunkBytes,
		4,
	)
	if err != nil {
		t.Fatalf("transfer upload: %v", err)
	}
	if total <= 0 {
		t.Fatalf("expected positive uploaded bytes, got %d", total)
	}
	mu.Lock()
	uploadedBytes := uploaded
	requestCount := requests
	peakInFlight := maxInFlight
	mu.Unlock()
	if uploadedBytes <= 0 {
		t.Fatalf("expected server to receive positive bytes, got %d", uploadedBytes)
	}
	if requestCount < 4 {
		t.Fatalf("expected multiple upload requests, got %d", requestCount)
	}
	if peakInFlight < 2 {
		t.Fatalf("expected parallel upload workers, got max concurrency %d", peakInFlight)
	}
	if duration <= 0 {
		t.Fatalf("expected positive duration, got %s", duration)
	}
}

func TestCleanupStaleTempDirsRemovesOnlySpeedtestDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	staleDir := filepath.Join(root, "fastlane-speedtest-stale")
	keepDir := filepath.Join(root, "keep-me")
	keepFile := filepath.Join(root, "note.txt")

	if err := os.MkdirAll(staleDir, 0o700); err != nil {
		t.Fatalf("create stale dir: %v", err)
	}
	if err := os.MkdirAll(keepDir, 0o755); err != nil {
		t.Fatalf("create keep dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staleDir, "config.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write stale config: %v", err)
	}
	if err := os.WriteFile(keepFile, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write keep file: %v", err)
	}

	if err := cleanupStaleTempDirs(root); err != nil {
		t.Fatalf("cleanup stale temp dirs: %v", err)
	}
	if _, err := os.Stat(staleDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale dir to be removed, got %v", err)
	}
	if _, err := os.Stat(keepDir); err != nil {
		t.Fatalf("expected keep dir to remain, got %v", err)
	}
	if _, err := os.Stat(keepFile); err != nil {
		t.Fatalf("expected keep file to remain, got %v", err)
	}
}

func TestRunnerReturnsBusyWhenLockHeld(t *testing.T) {
	t.Parallel()

	lockPath := filepath.Join(t.TempDir(), "speedtest.lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open lock file: %v", err)
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("lock file: %v", err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	runner := Runner{
		LockPath:   lockPath,
		BinaryPath: "/usr/bin/xray",
		TempRoot:   t.TempDir(),
		Start: func(context.Context, string, string) (Process, error) {
			return &fakeProcess{}, nil
		},
		Measure: func(context.Context, string, Provider) (Metrics, error) {
			return Metrics{}, nil
		},
		Now: func() time.Time { return time.Date(2026, 3, 26, 20, 0, 0, 0, time.UTC) },
	}

	_, err = runner.Test(context.Background(), Request{
		SubscriptionID: "sub-1",
		NodeID:         "node-1",
		NodeName:       "Node 1",
		Config:         []byte(`{}`),
		HTTPProxyPort:  18080,
	})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("expected ErrBusy, got %v", err)
	}
}

func TestRunnerCleansUpTempConfigAndStopsProcessOnSuccess(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	proc := &fakeProcess{}
	var configPath string

	runner := Runner{
		LockPath:   filepath.Join(tempRoot, "speedtest.lock"),
		BinaryPath: "/usr/bin/xray",
		TempRoot:   tempRoot,
		Start: func(_ context.Context, _ string, path string) (Process, error) {
			configPath = path
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("expected config to exist before start: %v", err)
			}
			return proc, nil
		},
		Measure: func(_ context.Context, proxyURL string, provider Provider) (Metrics, error) {
			if proxyURL != "http://127.0.0.1:18080" {
				t.Fatalf("unexpected proxy URL: %s", proxyURL)
			}
			if provider.Name == "" {
				t.Fatal("expected default provider")
			}
			return Metrics{
				Latency:          45 * time.Millisecond,
				DownloadBytes:    8 << 20,
				DownloadDuration: 2 * time.Second,
				UploadBytes:      2 << 20,
				UploadDuration:   time.Second,
			}, nil
		},
		Now: func() time.Time { return time.Date(2026, 3, 26, 20, 0, 0, 0, time.UTC) },
	}

	result, err := runner.Test(context.Background(), Request{
		SubscriptionID: "sub-1",
		NodeID:         "node-1",
		NodeName:       "Node 1",
		Config:         []byte(`{"log":{"loglevel":"warning"}}`),
		HTTPProxyPort:  18080,
	})
	if err != nil {
		t.Fatalf("run speed test: %v", err)
	}

	if proc.stopCalls != 1 {
		t.Fatalf("expected process to be stopped once, got %d", proc.stopCalls)
	}
	if _, err := os.Stat(configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected config to be removed, got %v", err)
	}
	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		t.Fatalf("read temp root: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "fastlane-speedtest-") {
			t.Fatalf("expected temp dir cleanup, found %s", entry.Name())
		}
	}
	if result.SubscriptionID != "sub-1" || result.NodeID != "node-1" || result.NodeName != "Node 1" {
		t.Fatalf("unexpected metadata: %+v", result)
	}
	if result.DownloadBytes != 8<<20 || result.UploadBytes != 2<<20 {
		t.Fatalf("unexpected transfer sizes: %+v", result)
	}
	if result.StartedAt.IsZero() || result.FinishedAt.IsZero() || !result.FinishedAt.After(result.StartedAt) {
		t.Fatalf("unexpected timestamps: %+v", result)
	}
}

func TestRunnerStopsProcessAndCleansUpOnMeasureFailure(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	proc := &fakeProcess{output: "xray failed"}

	runner := Runner{
		LockPath:   filepath.Join(tempRoot, "speedtest.lock"),
		BinaryPath: "/usr/bin/xray",
		TempRoot:   tempRoot,
		Start: func(_ context.Context, _ string, _ string) (Process, error) {
			return proc, nil
		},
		Measure: func(context.Context, string, Provider) (Metrics, error) {
			return Metrics{}, errors.New("probe failed")
		},
	}

	_, err := runner.Test(context.Background(), Request{
		SubscriptionID: "sub-1",
		NodeID:         "node-1",
		NodeName:       "Node 1",
		Config:         []byte(`{}`),
		HTTPProxyPort:  18080,
	})
	if err == nil || !strings.Contains(err.Error(), "probe failed") {
		t.Fatalf("expected probe failure, got %v", err)
	}
	if proc.stopCalls != 1 {
		t.Fatalf("expected process to be stopped on failure, got %d", proc.stopCalls)
	}
}

func TestWaitForLocalProxyReturnsImmediatelyWhenXrayExits(t *testing.T) {
	t.Parallel()

	process := &monitoredFakeProcess{
		fakeProcess: fakeProcess{output: "failed to listen TCP on 127.0.0.1:18080: bind: address already in use"},
		done:        make(chan struct{}),
		waitErr:     errors.New("exit status 23"),
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(process.done)
	}()

	started := time.Now()
	err := waitForLocalProxy(context.Background(), "127.0.0.1:1", process)
	if err == nil || !strings.Contains(err.Error(), "temporary xray exited") {
		t.Fatalf("expected asynchronous Xray exit, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("process exit was not observed promptly: %s", elapsed)
	}
}

func TestURLTestRejectsForeignProxyWhileXrayBindFailureIsDelayed(t *testing.T) {
	t.Parallel()

	var proxyHits atomic.Int32
	foreignProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer foreignProxy.Close()
	proxyURL, err := url.Parse(foreignProxy.URL)
	if err != nil {
		t.Fatalf("parse foreign proxy URL: %v", err)
	}
	port, err := strconv.Atoi(proxyURL.Port())
	if err != nil {
		t.Fatalf("parse foreign proxy port: %v", err)
	}

	process := &listenerOwnerFakeProcess{
		monitoredFakeProcess: monitoredFakeProcess{
			fakeProcess: fakeProcess{output: "failed to listen: bind: address already in use"},
			done:        make(chan struct{}),
			waitErr:     errors.New("exit status 23"),
		},
		owns: false,
	}
	go func() {
		time.Sleep(250 * time.Millisecond)
		close(process.done)
	}()

	runner := Runner{
		TempRoot: t.TempDir(),
		Start: func(context.Context, string, string) (Process, error) {
			return process, nil
		},
	}
	_, err = runner.URLTest(context.Background(), Request{
		SubscriptionID: "sub-1",
		NodeID:         "node-1",
		NodeName:       "Node 1",
		Config:         []byte(`{}`),
		HTTPProxyPort:  port,
	}, "http://probe.invalid/generate_204")
	if err == nil || !strings.Contains(err.Error(), "temporary xray exited") {
		t.Fatalf("expected delayed bind failure, got %v", err)
	}
	if got := proxyHits.Load(); got != 0 {
		t.Fatalf("foreign proxy handled %d URL-test requests", got)
	}
	if got := process.ownershipChecks.Load(); got == 0 {
		t.Fatal("expected listener ownership to be checked")
	}
}

func TestURLTestProbeTimeoutStartsAfterProxyStartup(t *testing.T) {
	t.Parallel()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	port, err := strconv.Atoi(proxyURL.Port())
	if err != nil {
		t.Fatalf("parse proxy port: %v", err)
	}

	runner := Runner{
		TempRoot: t.TempDir(),
		Start: func(context.Context, string, string) (Process, error) {
			time.Sleep(80 * time.Millisecond)
			return &fakeProcess{}, nil
		},
	}
	started := time.Now()
	result, err := runner.URLTest(context.Background(), Request{
		SubscriptionID: "sub-1",
		NodeID:         "node-1",
		NodeName:       "Node 1",
		Config:         []byte(`{}`),
		HTTPProxyPort:  port,
		ProbeTimeout:   40 * time.Millisecond,
	}, "http://probe.invalid/generate_204")
	if err != nil {
		t.Fatalf("URL test should get its full request timeout after startup: %v", err)
	}
	if result.LatencyMS < 0 {
		t.Fatalf("unexpected URL test result: %+v", result)
	}
	if elapsed := time.Since(started); elapsed < 75*time.Millisecond {
		t.Fatalf("test did not include simulated startup delay: %s", elapsed)
	}
}

func TestURLTestProbeTimeoutStillBoundsHTTPGet(t *testing.T) {
	t.Parallel()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(250 * time.Millisecond):
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	port, err := strconv.Atoi(proxyURL.Port())
	if err != nil {
		t.Fatalf("parse proxy port: %v", err)
	}

	runner := Runner{
		TempRoot: t.TempDir(),
		Start: func(context.Context, string, string) (Process, error) {
			return &fakeProcess{}, nil
		},
	}
	started := time.Now()
	_, err = runner.URLTest(context.Background(), Request{
		SubscriptionID: "sub-1",
		NodeID:         "node-1",
		NodeName:       "Node 1",
		Config:         []byte(`{}`),
		HTTPProxyPort:  port,
		ProbeTimeout:   25 * time.Millisecond,
	}, "http://probe.invalid/generate_204")
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("expected bounded GET timeout, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("GET timeout was not enforced promptly: %s", elapsed)
	}
}

func TestProcessOwnsTCPListenerAtMatchesSocketFDAndListeningPort(t *testing.T) {
	t.Parallel()

	procRoot := t.TempDir()
	processRoot := filepath.Join(procRoot, "4242")
	if err := os.MkdirAll(filepath.Join(processRoot, "fd"), 0o700); err != nil {
		t.Fatalf("create fake process fd directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(processRoot, "net"), 0o700); err != nil {
		t.Fatalf("create fake process net directory: %v", err)
	}
	if err := os.Symlink("socket:[99123]", filepath.Join(processRoot, "fd", "7")); err != nil {
		t.Fatalf("create fake socket descriptor: %v", err)
	}
	tcpTable := "  sl  local_address rem_address   st tx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 0100007F:46A0 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 99123\n"
	if err := os.WriteFile(filepath.Join(processRoot, "net", "tcp"), []byte(tcpTable), 0o600); err != nil {
		t.Fatalf("write fake TCP table: %v", err)
	}

	owned, err := processOwnsTCPListenerAt(procRoot, 4242, "127.0.0.1:18080")
	if err != nil {
		t.Fatalf("inspect fake listener: %v", err)
	}
	if !owned {
		t.Fatal("expected matching listening socket to be owned")
	}
	owned, err = processOwnsTCPListenerAt(procRoot, 4242, "127.0.0.1:18081")
	if err != nil {
		t.Fatalf("inspect unmatched fake listener: %v", err)
	}
	if owned {
		t.Fatal("unexpected ownership for a different port")
	}
}

func TestExecProcessConcurrentStopAndWait(t *testing.T) {
	tempRoot := t.TempDir()
	scriptPath := filepath.Join(tempRoot, "delayed-process.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nsleep 0.05\n"), 0o700); err != nil {
		t.Fatalf("write delayed process script: %v", err)
	}

	for iteration := 0; iteration < 12; iteration++ {
		process, err := startProcess(context.Background(), scriptPath, filepath.Join(tempRoot, "unused.json"))
		if err != nil {
			t.Fatalf("start iteration %d: %v", iteration, err)
		}
		monitor := process.(ProcessMonitor)
		var group sync.WaitGroup
		for worker := 0; worker < 8; worker++ {
			group.Add(1)
			go func() {
				defer group.Done()
				_ = process.Stop()
			}()
			group.Add(1)
			go func() {
				defer group.Done()
				<-monitor.Done()
				_ = monitor.WaitError()
			}()
		}
		group.Wait()
	}
}

type fakeProcess struct {
	stopCalls int
	output    string
}

func (p *fakeProcess) Stop() error {
	p.stopCalls++
	return nil
}

func (p *fakeProcess) Output() string {
	return p.output
}

type monitoredFakeProcess struct {
	fakeProcess
	done    chan struct{}
	waitErr error
}

func (p *monitoredFakeProcess) Done() <-chan struct{} { return p.done }
func (p *monitoredFakeProcess) WaitError() error      { return p.waitErr }

type listenerOwnerFakeProcess struct {
	monitoredFakeProcess
	owns            bool
	ownershipErr    error
	ownershipChecks atomic.Int32
}

func (p *listenerOwnerFakeProcess) OwnsTCPListener(string) (bool, error) {
	p.ownershipChecks.Add(1)
	return p.owns, p.ownershipErr
}
