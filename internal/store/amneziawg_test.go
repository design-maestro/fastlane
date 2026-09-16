package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/design-maestro/fastlane/internal/amneziawg"
	"github.com/design-maestro/fastlane/internal/domain"
)

func TestAWGProfileStoreUsesOwnerOnlyPermissions(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)
	raw := []byte("[Interface]\nPrivateKey = secret\n")
	id := "awg-test"
	if _, err := store.SaveAWGProfile(amneziawg.ProfileMetadata{ID: id, Name: "Test"}, raw); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "amneziawg.d", id+".conf"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
	loaded, err := store.LoadAWGProfile(id)
	if err != nil || len(loaded) == 0 {
		t.Fatalf("load = %q, %v", loaded, err)
	}
	if err := store.RemoveAWGProfile(id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadAWGProfile(id); !os.IsNotExist(err) {
		t.Fatalf("load after remove = %v", err)
	}
}

func TestAWGProfileStoreStacksAndDeduplicatesStableIDs(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)
	first := amneziawg.ProfileMetadata{ID: "awg-first", Name: "First"}
	second := amneziawg.ProfileMetadata{ID: "awg-second", Name: "Second"}
	if created, err := store.SaveAWGProfile(first, []byte("first")); err != nil || !created {
		t.Fatalf("save first: created=%t err=%v", created, err)
	}
	if created, err := store.SaveAWGProfile(first, []byte("changed")); err != nil || created {
		t.Fatalf("duplicate save: created=%t err=%v", created, err)
	}
	if created, err := store.SaveAWGProfile(second, []byte("second")); err != nil || !created {
		t.Fatalf("save second: created=%t err=%v", created, err)
	}
	profiles, err := store.ListAWGProfiles()
	if err != nil || len(profiles) != 2 {
		t.Fatalf("profiles = %+v, %v", profiles, err)
	}
	loaded, err := store.LoadAWGProfile(first.ID)
	if err != nil || string(loaded) != "first\n" {
		t.Fatalf("duplicate overwrote secret: %q, %v", loaded, err)
	}
	for _, path := range []string{filepath.Join(dir, "amneziawg-profiles.json"), filepath.Join(dir, "amneziawg.d", "awg-first.conf"), filepath.Join(dir, "amneziawg.d", "awg-second.conf")} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat %s: %v", path, statErr)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode=%v", path, info.Mode().Perm())
		}
	}
}

func TestAWGRuntimeStateRoundTrip(t *testing.T) {
	store := NewFileStore(t.TempDir())
	state := domain.DefaultRuntimeState()
	state.ActiveConnectionKind = "amneziawg"
	state.AWGProfileName = "Stand profile"
	state.AWGLastProbe = &domain.AWGProbeState{Success: true, CheckedAt: time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC), LatencyMS: 42.5}
	state.ActiveAWGProfileID = "awg-test"
	state.PreparedAWGProfileID = "awg-test"
	state.AWGProfileProbes["awg-test"] = *state.AWGLastProbe
	if err := store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ActiveConnectionKind != state.ActiveConnectionKind || loaded.AWGProfileName != state.AWGProfileName || loaded.AWGLastProbe == nil || !loaded.AWGLastProbe.Success || loaded.AWGLastProbe.LatencyMS != 42.5 || loaded.ActiveAWGProfileID != "awg-test" || loaded.PreparedAWGProfileID != "awg-test" || !loaded.AWGProfileProbes["awg-test"].Success {
		t.Fatalf("AWG runtime state was not preserved: %+v", loaded)
	}
}
