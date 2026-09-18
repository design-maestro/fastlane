package release_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAmneziaWGProtocolKeepsLegacyAndV31ToolchainsSeparate(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repoRoot(t), "openwrt", "root", "lib", "netifd", "proto", "amneziawg.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, required := range []string{
		"SYSTEM_AWG=/usr/bin/awg",
		"FASTLANE_AWG=/usr/libexec/fastlane-amneziawg",
		`if [ "$force_userspace" = 1 ]; then`,
		`awg_tool="$FASTLANE_AWG"`,
		`awg_tool="$SYSTEM_AWG"`,
		`proto_add_host_dependency "$config" "$address" "$tunlink"`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("AmneziaWG protocol is missing runtime isolation marker %q", required)
		}
	}
	if strings.Contains(source, "\nAWG=/usr/libexec/fastlane-amneziawg") {
		t.Fatal("AmneziaWG protocol must not force the 3.1 tool onto Legacy and AWG 2.0 profiles")
	}
	setconf := strings.Index(source, `"$awg_tool" setconf`)
	mtu := strings.Index(source, `ip link set mtu "$mtu" dev "$config"`)
	if setconf < 0 || mtu < setconf {
		t.Fatal("AmneziaWG protocol must apply the profile MTU after setconf")
	}
}
