package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/design-maestro/fastlane/internal/platform/openwrt"
	"github.com/design-maestro/fastlane/internal/update"
)

func TestUpdateStatusDoesNotInitializeVPNServiceOrCreateConfig(t *testing.T) {
	root := filepath.Join(t.TempDir(), "unused-state")
	cmd := newRootCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--root", root, "--json", "update", "status"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("update status touched VPN state: %v", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"current_version"`)) {
		t.Fatal(stdout.String())
	}
}

func TestUpdateInstallerRejectsNonRouterBeforeNetworkOrWrites(t *testing.T) {
	if openwrt.IsOpenWrt() && os.Geteuid() == 0 {
		t.Skip("not a host environment")
	}
	err := newUpdateManager().Install(context.Background(), update.Candidate{}, nil)
	if err == nil {
		t.Fatal("installer accepted non-router environment")
	}
}
