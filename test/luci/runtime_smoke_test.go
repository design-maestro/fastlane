package luci_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFastLaneProductionViewsRuntimeSmoke(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for LuCI JavaScript runtime smoke")
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	script := filepath.Join(root, "test", "luci", "runtime_smoke.js")
	command := exec.Command(node, script, root)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Fast Lane LuCI runtime smoke failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "SUMMARY\t") || !strings.Contains(string(output), " PASS") {
		t.Fatalf("Fast Lane LuCI runtime smoke did not report its matrix:\n%s", output)
	}
	t.Logf("Fast Lane LuCI runtime smoke:\n%s", output)
}
