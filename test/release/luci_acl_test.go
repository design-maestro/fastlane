package release_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLuCIACLReadPermissionsUseSafeWhitelist(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repoRoot(t), "luci-app-fastlane", "root", "usr", "share", "rpcd", "acl.d", "luci-app-fastlane.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ACL file: %v", err)
	}

	var payload struct {
		App struct {
			Read struct {
				File map[string][]string `json:"file"`
			} `json:"read"`
			Write struct {
				File map[string][]string `json:"file"`
			} `json:"write"`
		} `json:"luci-app-fastlane"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal ACL json: %v", err)
	}

	wantRead := map[string]struct{}{
		"/usr/bin/fastlane --json status":                      {},
		"/usr/bin/fastlane --json inspect health-check-status": {},
		"/usr/bin/fastlane --json diagnostics":                 {},
		"/usr/bin/fastlane --json list subscriptions":          {},
		"/usr/bin/fastlane --json settings get":                {},
		"/usr/bin/fastlane --json update status":               {},
		"/usr/bin/fastlane --json dns get":                     {},
		"/usr/bin/fastlane --json firewall get":                {},
		"/usr/bin/fastlane --json zapret get":                  {},
		"/usr/bin/fastlane --json zapret status":               {},
		"/usr/bin/fastlane --json services list":               {},
		"/usr/libexec/fastlane-geodata status":                 {},
	}

	if len(payload.App.Read.File) != len(wantRead) {
		t.Fatalf("unexpected read ACL entries:\nwant=%v\ngot=%v", wantRead, payload.App.Read.File)
	}
	for command, permissions := range payload.App.Read.File {
		if _, ok := wantRead[command]; !ok {
			t.Fatalf("unexpected read ACL command %q", command)
		}
		if len(permissions) != 1 || permissions[0] != "exec" {
			t.Fatalf("unexpected read ACL permissions for %q: %v", command, permissions)
		}
		if strings.Contains(command, "*") {
			t.Fatalf("read ACL must not use wildcard command %q", command)
		}
	}

	writePermissions, ok := payload.App.Write.File["/usr/bin/fastlane *"]
	if !ok {
		t.Fatalf("expected write ACL wildcard to remain, got %v", payload.App.Write.File)
	}
	if len(writePermissions) != 1 || writePermissions[0] != "exec" {
		t.Fatalf("unexpected write ACL permissions: %v", writePermissions)
	}

	for _, helper := range []string{
		"/usr/libexec/fastlane-self-update",
		"/usr/libexec/fastlane-xray-update",
		"/usr/libexec/fastlane-geodata *",
	} {
		permissions, ok := payload.App.Write.File[helper]
		if !ok {
			t.Fatalf("expected write ACL helper %q, got %v", helper, payload.App.Write.File)
		}
		if len(permissions) != 1 || permissions[0] != "exec" {
			t.Fatalf("unexpected write ACL permissions for %q: %v", helper, permissions)
		}
	}
}
