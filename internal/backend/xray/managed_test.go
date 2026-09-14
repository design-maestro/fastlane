package xray

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/design-maestro/fastlane/internal/domain"
)

type apiCall struct {
	stdin []byte
	name  string
	args  []string
}

type scriptedAPIRunner struct {
	calls []apiCall
	run   func(apiCall) ([]byte, error)
}

func (r *scriptedAPIRunner) Run(_ context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	call := apiCall{stdin: append([]byte(nil), stdin...), name: name, args: append([]string(nil), args...)}
	r.calls = append(r.calls, call)
	if r.run != nil {
		return r.run(call)
	}
	return nil, nil
}

func TestManagedBackendPrepareUsesStdinAndVerifiesTag(t *testing.T) {
	t.Setenv("FASTLANE_XRAY_BINARY", "/test/xray")
	runner := &scriptedAPIRunner{}
	runtimeBackend := NewRuntimeBackend(filepath.Join(t.TempDir(), "config.json"), nil)
	runtimeBackend.apiRunner = runner
	node := domain.Node{ID: "display-id", Name: "Secret display name", Protocol: domain.ProtocolSocks, Address: "secret-node.example", Port: 9999}
	_, expectedTag, err := managedOutboundForNode(node, 0)
	if err != nil {
		t.Fatal(err)
	}
	runner.run = func(call apiCall) ([]byte, error) {
		if len(call.args) > 1 && call.args[1] == "lso" {
			return []byte(`{"outbounds":[{"tag":"` + expectedTag + `"}]}`), nil
		}
		return []byte(`{}`), nil
	}

	tag, err := runtimeBackend.PrepareOutbound(context.Background(), node, 0)
	if err != nil {
		t.Fatalf("prepare outbound: %v", err)
	}
	if tag != expectedTag {
		t.Fatalf("tag = %q, want %q", tag, expectedTag)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(runner.calls))
	}
	if got := runner.calls[0].args; !reflect.DeepEqual(got[:4], []string{"api", "ado", "--server=" + managedAPIAddress, "--timeout=3"}) {
		t.Fatalf("unexpected add args: %#v", got)
	}
	if strings.Contains(strings.Join(runner.calls[0].args, " "), node.Address) {
		t.Fatal("subscription parameters leaked into process arguments")
	}
	if !strings.Contains(string(runner.calls[0].stdin), expectedTag) {
		t.Fatal("managed outbound was not passed through stdin")
	}
}

func TestManagedBackendSelectReadsBackActualTarget(t *testing.T) {
	runner := &scriptedAPIRunner{}
	runtimeBackend := NewRuntimeBackend(filepath.Join(t.TempDir(), "config.json"), nil)
	runtimeBackend.apiRunner = runner
	tag := managedOutboundPrefix + "0123456789abcdef"
	runner.run = func(call apiCall) ([]byte, error) {
		if len(call.args) > 1 && call.args[1] == "bi" {
			return []byte(`{"balancer":{"override":{"target":"` + tag + `"}}}`), nil
		}
		return nil, nil
	}
	if err := runtimeBackend.SelectOutbound(context.Background(), tag); err != nil {
		t.Fatalf("select outbound: %v", err)
	}
	if len(runner.calls) != 2 || runner.calls[0].args[1] != "bo" || runner.calls[1].args[1] != "bi" {
		t.Fatalf("unexpected API sequence: %#v", runner.calls)
	}
}

func TestManagedBackendSelectRejectsReadbackMismatch(t *testing.T) {
	runner := &scriptedAPIRunner{run: func(call apiCall) ([]byte, error) {
		if len(call.args) > 1 && call.args[1] == "bi" {
			return []byte(`{"balancer":{"override":{"target":"fastlane-direct"}}}`), nil
		}
		return nil, nil
	}}
	runtimeBackend := NewRuntimeBackend(filepath.Join(t.TempDir(), "config.json"), nil)
	runtimeBackend.apiRunner = runner
	err := runtimeBackend.SelectOutbound(context.Background(), managedOutboundPrefix+"0123456789abcdef")
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("expected readback mismatch, got %v", err)
	}
}

func TestManagedBackendSelectAcceptsAppliedTargetAfterAmbiguousAPIError(t *testing.T) {
	tag := managedOutboundPrefix + "0123456789abcdef"
	runner := &scriptedAPIRunner{run: func(call apiCall) ([]byte, error) {
		if len(call.args) > 1 && call.args[1] == "bo" {
			return []byte("connection closed"), errors.New("exit 1")
		}
		return []byte(`{"balancer":{"override":{"target":"` + tag + `"}}}`), nil
	}}
	runtimeBackend := NewRuntimeBackend(filepath.Join(t.TempDir(), "config.json"), nil)
	runtimeBackend.apiRunner = runner
	if err := runtimeBackend.SelectOutbound(context.Background(), tag); err != nil {
		t.Fatalf("ambiguous switch should use actual target: %v", err)
	}
}

func TestManagedBackendRemoveAcceptsMissingTargetAfterAmbiguousAPIError(t *testing.T) {
	runner := &scriptedAPIRunner{run: func(call apiCall) ([]byte, error) {
		if len(call.args) > 1 && call.args[1] == "rmo" {
			return []byte("connection closed"), errors.New("exit 1")
		}
		return []byte(`{"outbounds":[]}`), nil
	}}
	runtimeBackend := NewRuntimeBackend(filepath.Join(t.TempDir(), "config.json"), nil)
	runtimeBackend.apiRunner = runner
	if err := runtimeBackend.RemoveOutbound(context.Background(), managedOutboundPrefix+"0123456789abcdef"); err != nil {
		t.Fatalf("ambiguous removal should use actual state: %v", err)
	}
}

func TestManagedBackendPersistConfigDoesNotReload(t *testing.T) {
	dir := t.TempDir()
	controller := &scriptedController{}
	runtimeBackend := NewRuntimeBackend(filepath.Join(dir, "config.json"), controller)
	runtimeBackend.tester = &recordingConfigTester{}
	if err := runtimeBackend.PersistConfig(context.Background(), testConfigRequest()); err != nil {
		t.Fatalf("persist config: %v", err)
	}
	if controller.reloadCalls != 0 {
		t.Fatalf("persist reloaded Xray %d times", controller.reloadCalls)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Fatalf("persisted config missing: %v", err)
	}
}

func TestManagedBackendPrepareReturnsOriginalAPIErrorWhenVerificationFails(t *testing.T) {
	runner := &scriptedAPIRunner{run: func(call apiCall) ([]byte, error) {
		if len(call.args) > 1 && call.args[1] == "ado" {
			return []byte("denied"), errors.New("exit 1")
		}
		return []byte(`{"outbounds":[]}`), nil
	}}
	runtimeBackend := NewRuntimeBackend(filepath.Join(t.TempDir(), "config.json"), nil)
	runtimeBackend.apiRunner = runner
	_, err := runtimeBackend.PrepareOutbound(context.Background(), domain.Node{Protocol: domain.ProtocolSocks, Address: "127.0.0.1", Port: 9999}, 0)
	if err == nil || !strings.Contains(err.Error(), "ado") {
		t.Fatalf("expected add API error, got %v", err)
	}
}
