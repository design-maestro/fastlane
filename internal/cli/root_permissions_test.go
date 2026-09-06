package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/design-maestro/fastlane/internal/domain"
	"github.com/design-maestro/fastlane/internal/store"
)

func TestRootCommandHardensExistingSecretFilePermissions(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "fastlane")
	fileStore := store.NewFileStore(root)

	if err := fileStore.SaveSubscriptions([]domain.Subscription{
		{
			ID:              "sub-1",
			SourceType:      domain.SourceTypeRaw,
			Source:          "vless://11111111-1111-1111-1111-111111111111@example.com:443?encryption=none",
			ProviderName:    "Example",
			DisplayName:     "Example",
			RefreshInterval: domain.NewDuration(domain.DefaultSettings().RefreshInterval.Duration()),
		},
	}); err != nil {
		t.Fatalf("save subscriptions: %v", err)
	}
	if err := fileStore.SaveSettings(domain.DefaultSettings()); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	if err := fileStore.SaveState(domain.DefaultRuntimeState()); err != nil {
		t.Fatalf("save state: %v", err)
	}

	for _, path := range []string{
		filepath.Join(root, ".fastlane.lock"),
		filepath.Join(root, "speedtest.lock"),
		filepath.Join(root, "settings.corrupt-20260329T120000Z.json"),
		filepath.Join(root, "xray-config.json"),
		filepath.Join(root, "xray-config.json.last-known-good"),
	} {
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatalf("chmod root insecure: %v", err)
	}
	for _, path := range []string{
		filepath.Join(root, "subscriptions.json"),
		filepath.Join(root, "settings.json"),
		filepath.Join(root, "state.json"),
		filepath.Join(root, ".fastlane.lock"),
		filepath.Join(root, "speedtest.lock"),
		filepath.Join(root, "settings.corrupt-20260329T120000Z.json"),
		filepath.Join(root, "xray-config.json"),
		filepath.Join(root, "xray-config.json.last-known-good"),
	} {
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatalf("chmod %s insecure: %v", path, err)
		}
	}

	cmd := newRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"--root", root, "status"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute status: %v", err)
	}

	assertPathPerm(t, root, store.PrivateDirPerm)
	for _, path := range []string{
		filepath.Join(root, "subscriptions.json"),
		filepath.Join(root, "settings.json"),
		filepath.Join(root, "state.json"),
		filepath.Join(root, ".fastlane.lock"),
		filepath.Join(root, "speedtest.lock"),
		filepath.Join(root, "settings.corrupt-20260329T120000Z.json"),
		filepath.Join(root, "xray-config.json"),
		filepath.Join(root, "xray-config.json.last-known-good"),
	} {
		assertPathPerm(t, path, store.SecretFilePerm)
	}
}

func TestRootCommandRecoversCorruptFilesBeforeServiceReads(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "fastlane")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create root: %v", err)
	}
	for name, contents := range map[string]string{
		"settings.json":      `{"refresh_interval":`,
		"state.json":         `{"connected":"invalid"}`,
		"subscriptions.json": `[{"id":`,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	cmd := newRootCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"--root", root, "settings", "get"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute settings get: %v", err)
	}

	fileStore := store.NewFileStore(root)
	if _, err := fileStore.LoadSettings(); err != nil {
		t.Fatalf("load recovered settings: %v", err)
	}
	if _, err := fileStore.LoadState(); err != nil {
		t.Fatalf("load recovered state: %v", err)
	}
	if subscriptions, err := fileStore.LoadSubscriptions(); err != nil {
		t.Fatalf("load recovered subscriptions: %v", err)
	} else if len(subscriptions) != 0 {
		t.Fatalf("expected empty recovered subscriptions, got %+v", subscriptions)
	}

	for _, stem := range []string{"settings", "state", "subscriptions"} {
		matches, err := filepath.Glob(filepath.Join(root, stem+".corrupt-*.json"))
		if err != nil {
			t.Fatalf("glob %s backups: %v", stem, err)
		}
		if len(matches) != 1 {
			t.Fatalf("expected one %s backup, got %v", stem, matches)
		}
	}
}

func assertPathPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("unexpected mode for %s: got %o want %o", path, got, want)
	}
}
