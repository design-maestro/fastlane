package amneziawg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type controllerCall struct {
	stdin []byte
	name  string
	args  []string
}

type controllerRunner struct {
	calls []controllerCall
	run   func(controllerCall) ([]byte, error)
}

func (r *controllerRunner) Run(_ context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	call := controllerCall{stdin: append([]byte(nil), stdin...), name: name, args: append([]string(nil), args...)}
	r.calls = append(r.calls, call)
	if r.run != nil {
		return r.run(call)
	}
	return nil, nil
}

func newControllerForTest(t *testing.T, runner *controllerRunner) *OpenWrtController {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "lib/netifd/proto/amneziawg.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &OpenWrtController{InterfaceName: DefaultInterfaceName, SysRoot: root, Runner: runner, WaitTimeout: time.Second}
}

func successfulControllerRunner() *controllerRunner {
	return &controllerRunner{run: func(call controllerCall) ([]byte, error) {
		joined := call.name + " " + strings.Join(call.args, " ")
		switch {
		case joined == "uname -r":
			return []byte("5.15.150\n"), nil
		case strings.HasPrefix(joined, FastLaneAWGTool+" --version"):
			return []byte("amneziawg-tools v2\n"), nil
		case strings.HasPrefix(joined, "modinfo "):
			return []byte("5.15.150 SMP mod_unload\n"), nil
		case strings.HasPrefix(joined, "modprobe -n "):
			return nil, nil
		case joined == "amneziawg-go --version":
			return nil, errors.New("not installed")
		case strings.HasPrefix(joined, "ubus call "):
			return []byte(`{"up":true,"l3_device":"fastlane_awg","ipv4-address":[{"address":"10.8.0.2"}]}`), nil
		case strings.HasPrefix(joined, FastLaneAWGTool+" show "):
			return []byte(testPublicKey + "\t1700000000\n"), nil
		default:
			return nil, nil
		}
	}}
}

func TestOpenWrtControllerPreparesSecretThroughStdinAndConnectsPolicyRoute(t *testing.T) {
	profile, err := Parse([]byte(validProfile("")))
	if err != nil {
		t.Fatal(err)
	}
	runner := successfulControllerRunner()
	controller := newControllerForTest(t, runner)
	if err := controller.Prepare(context.Background(), profile); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	status, err := controller.Connect(context.Background())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !status.Up || status.Address != "10.8.0.2" {
		t.Fatalf("status = %+v", status)
	}

	var batch string
	for _, call := range runner.calls {
		if call.name == "uci" && len(call.stdin) > 0 {
			batch = string(call.stdin)
		}
		if strings.Contains(strings.Join(call.args, " "), testPrivateKey) {
			t.Fatal("private key leaked into process arguments")
		}
	}
	for _, expected := range []string{"proto='amneziawg'", "private_key='" + testPrivateKey + "'", "route_allowed_ips='0'", "awg_s3='30'", "awg_i1="} {
		if !strings.Contains(batch, expected) {
			t.Fatalf("UCI batch missing %q:\n%s", expected, batch)
		}
	}
	joined := callsText(runner.calls)
	if !strings.Contains(joined, "ubus call network reload") {
		t.Fatalf("netifd configuration was not reloaded:\n%s", joined)
	}
	if !strings.Contains(joined, "ip -4 route replace default dev fastlane_awg table 51821") ||
		!strings.Contains(joined, "ip -4 rule add fwmark 0x200 table 51821 priority 10900") {
		t.Fatalf("policy route was not installed:\n%s", joined)
	}
}

func TestOpenWrtControllerPreparesLegacyWithoutS3S4(t *testing.T) {
	profile, err := Parse([]byte(legacyProfile("")))
	if err != nil {
		t.Fatal(err)
	}
	runner := successfulControllerRunner()
	controller := newControllerForTest(t, runner)
	if err := controller.Prepare(context.Background(), profile); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	var batch string
	for _, call := range runner.calls {
		if call.name == "uci" && len(call.stdin) > 0 {
			batch = string(call.stdin)
		}
	}
	for _, expected := range []string{"awg_s1='10'", "awg_s2='20'", "awg_h1='100-200'"} {
		if !strings.Contains(batch, expected) {
			t.Fatalf("legacy UCI batch missing %q:\n%s", expected, batch)
		}
	}
	for _, forbidden := range []string{"awg_s3=", "awg_s4=", "awg_i1="} {
		if strings.Contains(batch, forbidden) {
			t.Fatalf("legacy UCI batch contains %q:\n%s", forbidden, batch)
		}
	}
}

func TestOpenWrtControllerRejectsKernelVermagicMismatch(t *testing.T) {
	runner := successfulControllerRunner()
	runner.run = func(call controllerCall) ([]byte, error) {
		joined := call.name + " " + strings.Join(call.args, " ")
		switch {
		case joined == "uname -r":
			return []byte("6.6.1\n"), nil
		case strings.HasPrefix(joined, FastLaneAWGTool+" --version"):
			return []byte("v2\n"), nil
		case strings.HasPrefix(joined, "modinfo "):
			return []byte("5.15.150 SMP\n"), nil
		case joined == "amneziawg-go --version":
			return nil, errors.New("not installed")
		default:
			return nil, nil
		}
	}
	controller := newControllerForTest(t, runner)
	status, err := controller.Preflight(context.Background())
	if err == nil || status.Compatible || !strings.Contains(status.FailureReason, "different kernel") {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if strings.Contains(callsText(runner.calls), "modprobe -f") {
		t.Fatal("controller must never force-load a module")
	}
}

func TestOpenWrtControllerAcceptsUserspaceRuntimeWithoutKernelModule(t *testing.T) {
	runner := successfulControllerRunner()
	runner.run = func(call controllerCall) ([]byte, error) {
		joined := call.name + " " + strings.Join(call.args, " ")
		switch {
		case joined == "uname -r":
			return []byte("6.6.134+\n"), nil
		case joined == FastLaneAWGTool+" --version":
			return []byte("amneziawg-tools v1.0.20260618-2\n"), nil
		case strings.HasPrefix(joined, "modinfo "):
			return nil, errors.New("module not found")
		case joined == "amneziawg-go --version":
			return []byte("amneziawg-go v3.1.20260828\n"), nil
		default:
			return nil, errors.New("unexpected command: " + joined)
		}
	}
	controller := newControllerForTest(t, runner)
	protoPath := filepath.Join(controller.SysRoot, "lib/netifd/proto/amneziawg.sh")
	if err := os.WriteFile(protoPath, []byte("#!/bin/sh\nAWG="+FastLaneAWGTool+"\namneziawg-go --version\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	tunPath := filepath.Join(controller.SysRoot, "dev/net/tun")
	if err := os.MkdirAll(filepath.Dir(tunPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tunPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := controller.Preflight(context.Background())
	if err != nil || !status.Compatible || status.Runtime != "amneziawg-go v3.1.20260828" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestOpenWrtControllerPreparesAWG31OnlyWithCompatibleBundledChain(t *testing.T) {
	profile, err := Parse([]byte(validProfile("Version = 3.1\nHeaderProtectionKey = " + testPrivateKey + "\nContentPaddingAddition = 10\nRekeyAfterTime = 60\nRekeyTimeout = 5\nRejectAfterTime = 120\nKeepaliveTimeout = 9\nMaxHandshakeAttempts = 7\nRandomTrailers = true\nDisableCookies = false")))
	if err != nil {
		t.Fatal(err)
	}
	runner := successfulControllerRunner()
	runner.run = func(call controllerCall) ([]byte, error) {
		joined := call.name + " " + strings.Join(call.args, " ")
		switch joined {
		case "uname -r":
			return []byte("6.6.134+\n"), nil
		case FastLaneAWGTool + " --version":
			return []byte("amneziawg-tools v3.1.20260812\n"), nil
		case "amneziawg-go --version":
			return []byte("amneziawg-go v3.1.20260828\n"), nil
		default:
			return nil, nil
		}
	}
	controller := newControllerForTest(t, runner)
	protoPath := filepath.Join(controller.SysRoot, "lib/netifd/proto/amneziawg.sh")
	if err := os.WriteFile(protoPath, []byte("#!/bin/sh\nAWG="+FastLaneAWGTool+"\namneziawg-go\nawg_header_protection_key\nawg_random_trailers\nawg_force_userspace\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	tunPath := filepath.Join(controller.SysRoot, "dev/net/tun")
	if err := os.MkdirAll(filepath.Dir(tunPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tunPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := controller.Prepare(context.Background(), profile); err != nil {
		t.Fatalf("prepare AWG 3.1: %v", err)
	}
	var batch string
	for _, call := range runner.calls {
		if call.name == "uci" && len(call.stdin) > 0 {
			batch = string(call.stdin)
		}
	}
	if !strings.Contains(batch, "awg_force_userspace='1'") {
		t.Fatalf("AWG 3.1 UCI batch must force userspace runtime:\n%s", batch)
	}
}

func TestOpenWrtControllerRejectsAWG31WithOldBundledTools(t *testing.T) {
	profile, err := Parse([]byte(validProfile("Version = 3.1\nHeaderProtectionKey = " + testPrivateKey + "\nContentPaddingAddition = 10\nRekeyAfterTime = 60\nRekeyTimeout = 5\nRejectAfterTime = 120\nKeepaliveTimeout = 9\nMaxHandshakeAttempts = 7\nRandomTrailers = true\nDisableCookies = false")))
	if err != nil {
		t.Fatal(err)
	}
	runner := successfulControllerRunner()
	controller := newControllerForTest(t, runner)
	if err := controller.Prepare(context.Background(), profile); err == nil || !strings.Contains(err.Error(), "incompatible with AWG 3.1") {
		t.Fatalf("prepare error = %v", err)
	}
}

func TestOpenWrtControllerRejectsUserspaceWithoutTUN(t *testing.T) {
	runner := &controllerRunner{run: func(call controllerCall) ([]byte, error) {
		joined := call.name + " " + strings.Join(call.args, " ")
		switch {
		case joined == "uname -r":
			return []byte("6.6.134+\n"), nil
		case joined == FastLaneAWGTool+" --version":
			return []byte("amneziawg-tools v2\n"), nil
		case strings.HasPrefix(joined, "modinfo "):
			return nil, errors.New("module not found")
		case joined == "amneziawg-go --version":
			return []byte("amneziawg-go v3\n"), nil
		default:
			return nil, errors.New("unexpected command: " + joined)
		}
	}}
	controller := newControllerForTest(t, runner)
	protoPath := filepath.Join(controller.SysRoot, "lib/netifd/proto/amneziawg.sh")
	if err := os.WriteFile(protoPath, []byte("#!/bin/sh\nAWG="+FastLaneAWGTool+"\namneziawg-go --version\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	status, err := controller.Preflight(context.Background())
	if err == nil || status.Compatible || !strings.Contains(status.FailureReason, "TUN") {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestOpenWrtControllerUsesOpenWrtKernelABIWhenModinfoIsMissing(t *testing.T) {
	runner := successfulControllerRunner()
	runner.run = func(call controllerCall) ([]byte, error) {
		joined := call.name + " " + strings.Join(call.args, " ")
		switch {
		case joined == "uname -r":
			return []byte("6.6.119\n"), nil
		case joined == FastLaneAWGTool+" --version":
			return []byte("amneziawg-tools v2\n"), nil
		case strings.HasPrefix(joined, "modinfo "):
			return nil, errors.New("not found")
		case joined == "opkg status kmod-amneziawg":
			return []byte("Package: kmod-amneziawg\nDepends: kernel (=6.6.119~abi-r1), kmod-foo\n"), nil
		case joined == "opkg status kernel":
			return []byte("Package: kernel\nVersion: 6.6.119~abi-r1\n"), nil
		case strings.HasPrefix(joined, "modprobe -n "):
			return nil, nil
		default:
			return nil, nil
		}
	}
	controller := newControllerForTest(t, runner)
	modulePath := filepath.Join(controller.SysRoot, "lib/modules/6.6.119/amneziawg.ko")
	if err := os.MkdirAll(filepath.Dir(modulePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modulePath, []byte("module"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := controller.Preflight(context.Background())
	if err != nil || !status.Compatible || !strings.Contains(status.Module, "kernel ABI") {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestOpenWrtControllerRemoveIsScopedToOwnedSections(t *testing.T) {
	runner := successfulControllerRunner()
	controller := newControllerForTest(t, runner)
	if err := controller.Remove(context.Background()); err != nil {
		t.Fatal(err)
	}
	var batch string
	for _, call := range runner.calls {
		if call.name == "uci" && len(call.stdin) > 0 {
			batch = string(call.stdin)
		}
	}
	if batch != "delete network.fastlane_awg\ndelete network.amneziawg_fastlane_awg\n" {
		t.Fatalf("unexpected removal scope: %q", batch)
	}
}

func TestOpenWrtControllerPreflightRequiresNetifd(t *testing.T) {
	controller := &OpenWrtController{SysRoot: t.TempDir(), Runner: successfulControllerRunner()}
	status, err := controller.Preflight(context.Background())
	if err == nil || status.Netifd {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func callsText(calls []controllerCall) string {
	var lines []string
	for _, call := range calls {
		lines = append(lines, call.name+" "+strings.Join(call.args, " "))
	}
	return strings.Join(lines, "\n")
}
