package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/domain"
)

func TestAWGProfileStoreUsesOwnerOnlyPermissions(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)
	raw := []byte("[Interface]\nPrivateKey = secret\n")
	if err := store.SaveAWGProfile(raw); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "amneziawg.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
	loaded, err := store.LoadAWGProfile()
	if err != nil || len(loaded) == 0 {
		t.Fatalf("load = %q, %v", loaded, err)
	}
	if err := store.RemoveAWGProfile(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadAWGProfile(); !os.IsNotExist(err) {
		t.Fatalf("load after remove = %v", err)
	}
}

func TestAWGRuntimeStateRoundTrip(t *testing.T) {
	store := NewFileStore(t.TempDir())
	state := domain.DefaultRuntimeState()
	state.ActiveConnectionKind = "amneziawg"
	state.AWGProfileName = "Stand profile"
	state.AWGLastProbe = &domain.AWGProbeState{Success: true, CheckedAt: time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC), LatencyMS: 42.5}
	if err := store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ActiveConnectionKind != state.ActiveConnectionKind || loaded.AWGProfileName != state.AWGProfileName || loaded.AWGLastProbe == nil || !loaded.AWGLastProbe.Success || loaded.AWGLastProbe.LatencyMS != 42.5 {
		t.Fatalf("AWG runtime state was not preserved: %+v", loaded)
	}
}
