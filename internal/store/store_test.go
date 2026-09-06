package store_test

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/store"
)

func TestAtomicWriteJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	root := filepath.Join(dir, "fastlane")
	path := filepath.Join(root, "settings.json")

	settings := domain.DefaultSettings()
	settings.LogLevel = "debug"

	if err := store.AtomicWriteJSON(path, settings); err != nil {
		t.Fatalf("atomic write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("expected file content")
	}

	matches, err := filepath.Glob(filepath.Join(root, "*.tmp-*"))
	if err != nil {
		t.Fatalf("glob temp files: %v", err)
	}

	if len(matches) != 0 {
		t.Fatalf("unexpected temp files: %v", matches)
	}

	assertPerm(t, path, store.SecretFilePerm)
	assertPerm(t, root, store.PrivateDirPerm)
}

func TestFileStoreRoundTrip(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fs := store.NewFileStore(root)

	sub := domain.Subscription{
		ID:              "sub-1",
		ProviderName:    "Example",
		SourceType:      domain.SourceTypeRaw,
		Source:          "vless://...",
		LastUpdatedAt:   time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC),
		RefreshInterval: domain.NewDuration(time.Hour),
		Nodes: []domain.Node{
			{ID: "node-1", Name: "Node 1", Protocol: domain.ProtocolVLESS, Address: "example.com", Port: 443},
		},
	}

	state := domain.RuntimeState{
		SchemaVersion:        1,
		Mode:                 domain.SelectionModeManual,
		Connected:            true,
		ActiveSubscriptionID: sub.ID,
		ActiveNodeID:         "node-1",
	}

	settings := domain.DefaultSettings()

	if err := fs.SaveSubscriptions([]domain.Subscription{sub}); err != nil {
		t.Fatalf("save subscriptions: %v", err)
	}

	if err := fs.SaveState(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	if err := fs.SaveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	subs, err := fs.LoadSubscriptions()
	if err != nil {
		t.Fatalf("load subscriptions: %v", err)
	}

	if len(subs) != 1 || subs[0].ID != sub.ID {
		t.Fatalf("unexpected subscriptions: %+v", subs)
	}

	gotState, err := fs.LoadState()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}

	if gotState.ActiveNodeID != "node-1" {
		t.Fatalf("unexpected state: %+v", gotState)
	}

	gotSettings, err := fs.LoadSettings()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}

	if gotSettings.LogLevel != settings.LogLevel {
		t.Fatalf("unexpected settings: %+v", gotSettings)
	}

	assertPerm(t, filepath.Join(root, "subscriptions.json"), store.SecretFilePerm)
	assertPerm(t, filepath.Join(root, "settings.json"), store.SecretFilePerm)
	assertPerm(t, filepath.Join(root, "state.json"), store.SecretFilePerm)
}

func TestFileStoreWithWriteLockSerializesAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	releasePath := filepath.Join(dir, "release")

	cmd := exec.Command(os.Args[0], "-test.run=^TestFileStoreWithWriteLockHelperProcess$")
	cmd.Env = append(os.Environ(),
		"FASTLANE_STORE_LOCK_HELPER=1",
		"FASTLANE_STORE_LOCK_DIR="+dir,
		"FASTLANE_STORE_LOCK_RELEASE="+releasePath,
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read helper ready line: %v", err)
	}
	if strings.TrimSpace(line) != "locked" {
		t.Fatalf("unexpected helper ready line: %q", line)
	}

	fs := store.NewFileStore(dir)
	acquired := make(chan error, 1)
	go func() {
		acquired <- fs.WithWriteLock(func() error { return nil })
	}()

	select {
	case err := <-acquired:
		t.Fatalf("lock acquired before helper released it: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	if err := os.WriteFile(releasePath, []byte("release\n"), 0o644); err != nil {
		t.Fatalf("write release marker: %v", err)
	}

	select {
	case err := <-acquired:
		if err != nil {
			t.Fatalf("acquire lock after release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for lock after helper release")
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait helper: %v", err)
	}
}

func TestFileStoreWithWriteLockSerializesWithinProcess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	assertFileStoreWriteLocksSerialize(t, store.NewFileStore(dir), store.NewFileStore(dir))
}

func TestFileStoreWithWriteLockCanonicalizesEquivalentPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	relativeDir, err := filepath.Rel(workingDir, dir)
	if err != nil {
		t.Fatalf("make store path relative: %v", err)
	}
	symlinkDir := filepath.Join(t.TempDir(), "store-link")
	if err := os.Symlink(dir, symlinkDir); err != nil {
		t.Fatalf("create store symlink: %v", err)
	}

	assertFileStoreWriteLocksSerialize(t, store.NewFileStore(relativeDir), store.NewFileStore(symlinkDir))
}

func TestRecoverCorruptFilesDoesNotOverwriteConcurrentSuccessfulPatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	settingsPath := filepath.Join(root, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"refresh_interval":`), store.SecretFilePerm); err != nil {
		t.Fatalf("write corrupt settings: %v", err)
	}

	writer := store.NewFileStore(root)
	recovery := store.NewFileStore(root)
	recoveryStarted := make(chan struct{})
	recoveryDone := make(chan error, 1)

	if err := writer.WithWriteLock(func() error {
		go func() {
			close(recoveryStarted)
			recoveryDone <- recovery.RecoverCorruptFiles()
		}()
		<-recoveryStarted

		select {
		case err := <-recoveryDone:
			return fmt.Errorf("recovery bypassed active writer lock: %v", err)
		case <-time.After(100 * time.Millisecond):
		}

		patched := domain.DefaultSettings()
		patched.URLTestTimeout = domain.NewDuration(7 * time.Second)
		patched.LogLevel = "debug"
		return writer.SaveSettings(patched)
	}); err != nil {
		t.Fatalf("write patched settings: %v", err)
	}

	select {
	case err := <-recoveryDone:
		if err != nil {
			t.Fatalf("recover after patch: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("recovery did not finish after writer released the lock")
	}

	got, err := writer.LoadSettings()
	if err != nil {
		t.Fatalf("load patched settings: %v", err)
	}
	if got.LogLevel != "debug" || got.URLTestTimeout.Duration() != 7*time.Second {
		t.Fatalf("recovery overwrote successful patch: %+v", got)
	}

	backups, err := filepath.Glob(filepath.Join(root, "settings.corrupt-*.json"))
	if err != nil {
		t.Fatalf("glob corrupt backups: %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("valid patched settings were incorrectly backed up: %v", backups)
	}
}

func TestLoadInsideWriteLockIsPureAndDoesNotDeadlockOnCorruption(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	settingsPath := filepath.Join(root, "settings.json")
	corrupt := []byte(`{"refresh_interval":`)
	if err := os.WriteFile(settingsPath, corrupt, store.SecretFilePerm); err != nil {
		t.Fatalf("write corrupt settings: %v", err)
	}

	fileStore := store.NewFileStore(root)
	done := make(chan error, 1)
	go func() {
		done <- fileStore.WithWriteLock(func() error {
			_, err := fileStore.LoadSettings()
			if err == nil {
				return fmt.Errorf("expected corrupt settings error")
			}
			return nil
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("load inside write lock: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("read-only load attempted a reentrant store lock")
	}

	got, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings after load: %v", err)
	}
	if string(got) != string(corrupt) {
		t.Fatalf("LoadSettings mutated corrupt file: %q", got)
	}
}

func assertFileStoreWriteLocksSerialize(t *testing.T, first, second *store.FileStore) {
	t.Helper()

	entered := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- first.WithWriteLock(func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case err := <-firstDone:
		t.Fatalf("first lock failed before entering callback: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("first in-process lock did not enter callback")
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- second.WithWriteLock(func() error { return nil })
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("second in-process lock entered before release: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first lock: %v", err)
	}
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second lock: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second in-process lock did not acquire after release")
	}
}

func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("unexpected mode for %s: got %o want %o", path, got, want)
	}
}

func TestFileStoreWithWriteLockHelperProcess(t *testing.T) {
	if os.Getenv("FASTLANE_STORE_LOCK_HELPER") != "1" {
		return
	}

	dir := os.Getenv("FASTLANE_STORE_LOCK_DIR")
	releasePath := os.Getenv("FASTLANE_STORE_LOCK_RELEASE")

	fs := store.NewFileStore(dir)
	if err := fs.WithWriteLock(func() error {
		fmt.Fprintln(os.Stdout, "locked")
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if _, err := os.Stat(releasePath); err == nil {
				return nil
			}
		}
		return fmt.Errorf("timed out waiting for release marker")
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	os.Exit(0)
}
