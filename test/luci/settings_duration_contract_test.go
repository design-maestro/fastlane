package luci_test

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestFastLaneSettingsRoundTripGoDurationsAcrossUIUnits(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for LuCI JavaScript duration contract")
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	type durationCase struct {
		Name             string         `json:"name"`
		GoValue          string         `json:"goValue"`
		Units            []string       `json:"units"`
		WantParts        map[string]int `json:"wantParts"`
		WantMilliseconds int64          `json:"wantMilliseconds"`
	}
	cases := []durationCase{
		{
			Name:             "refresh interval decomposes into hours minutes and seconds",
			GoValue:          (time.Hour + 2*time.Minute + 3*time.Second).String(),
			Units:            []string{"h", "m", "s"},
			WantParts:        map[string]int{"h": 1, "m": 2, "s": 3},
			WantMilliseconds: 3_723_000,
		},
		{
			Name:             "switch cooldown converts hours into total minutes",
			GoValue:          (time.Hour + 2*time.Minute + 3*time.Second).String(),
			Units:            []string{"m", "s"},
			WantParts:        map[string]int{"m": 62, "s": 3},
			WantMilliseconds: 3_723_000,
		},
		{
			Name:             "latency threshold converts fractional Go seconds into milliseconds",
			GoValue:          (1500 * time.Millisecond).String(),
			Units:            []string{"ms"},
			WantParts:        map[string]int{"ms": 1500},
			WantMilliseconds: 1_500,
		},
		{
			Name:             "mixed Go duration preserves millisecond remainder",
			GoValue:          (time.Hour + 2*time.Minute + 3*time.Second + 4*time.Millisecond).String(),
			Units:            []string{"h", "m", "s", "ms"},
			WantParts:        map[string]int{"h": 1, "m": 2, "s": 3, "ms": 4},
			WantMilliseconds: 3_723_004,
		},
	}

	payload, err := json.Marshal(cases)
	if err != nil {
		t.Fatalf("encode duration cases: %v", err)
	}
	script := filepath.Join(root, "test", "luci", "settings_duration_contract.js")
	cmd := exec.Command(node, script, root, string(payload))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Fast Lane settings duration contract failed: %v\n%s", err, output)
	}
	t.Logf("Fast Lane settings duration contract:\n%s", output)
}
