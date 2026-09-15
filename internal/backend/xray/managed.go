package xray

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/design-maestro/fastlane/internal/backend"
	"github.com/design-maestro/fastlane/internal/domain"
)

const managedAPITimeout = 5 * time.Second

type commandRunner interface {
	Run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error)
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	return cmd.CombinedOutput()
}

func (b RuntimeBackend) PrepareOutbound(ctx context.Context, node domain.Node, outboundMark int) (string, error) {
	outbound, tag, err := managedOutboundForNode(node, outboundMark)
	if err != nil {
		return "", err
	}
	if present, listErr := b.outboundPresent(ctx, tag); listErr == nil && present {
		return tag, nil
	}
	payload, err := json.Marshal(map[string]any{"outbounds": []any{outbound}})
	if err != nil {
		return "", fmt.Errorf("marshal xray outbound: %w", err)
	}
	if _, err := b.runAPI(ctx, payload, "ado"); err != nil {
		// An idempotent retry may encounter an already-added handler. Verify the
		// manager before deciding whether the operation failed.
		if present, listErr := b.outboundPresent(ctx, tag); listErr != nil || !present {
			return "", err
		}
	}
	present, err := b.outboundPresent(ctx, tag)
	if err != nil {
		return "", err
	}
	if !present {
		return "", fmt.Errorf("xray did not expose prepared outbound %q", tag)
	}
	return tag, nil
}

func (b RuntimeBackend) PrepareInterfaceOutbound(ctx context.Context, interfaceName, sourceAddress string, outboundMark int) (string, error) {
	outbound, tag, err := managedInterfaceOutbound(interfaceName, sourceAddress, outboundMark)
	if err != nil {
		return "", err
	}
	if present, listErr := b.outboundPresent(ctx, tag); listErr == nil && present {
		return tag, nil
	}
	payload, err := json.Marshal(map[string]any{"outbounds": []any{outbound}})
	if err != nil {
		return "", fmt.Errorf("marshal xray interface outbound: %w", err)
	}
	if _, err := b.runAPI(ctx, payload, "ado"); err != nil {
		if present, listErr := b.outboundPresent(ctx, tag); listErr != nil || !present {
			return "", err
		}
	}
	present, err := b.outboundPresent(ctx, tag)
	if err != nil {
		return "", err
	}
	if !present {
		return "", fmt.Errorf("xray did not expose prepared interface outbound %q", tag)
	}
	return tag, nil
}

func (b RuntimeBackend) RemoveOutbound(ctx context.Context, tag string) error {
	if err := validateManagedTag(tag, true); err != nil {
		return err
	}
	_, removeErr := b.runAPI(ctx, nil, "rmo", tag)
	present, listErr := b.outboundPresent(ctx, tag)
	if listErr == nil && !present {
		return nil
	}
	if removeErr != nil {
		return removeErr
	}
	if listErr != nil {
		return listErr
	}
	return fmt.Errorf("xray still exposes removed outbound %q", tag)
}

func (b RuntimeBackend) SelectOutbound(ctx context.Context, tag string) error {
	if err := validateManagedTag(tag, false); err != nil {
		return err
	}
	return b.selectBalancerTarget(ctx, managedBalancerTag, tag)
}

func (b RuntimeBackend) SelectDirect(ctx context.Context) error {
	return b.SelectOutbound(ctx, managedDirectTag)
}

func (b RuntimeBackend) SelectedOutbound(ctx context.Context) (string, error) {
	return b.selectedBalancerTarget(ctx, managedBalancerTag)
}

func (b RuntimeBackend) selectedBalancerTarget(ctx context.Context, balancer string) (string, error) {
	out, err := b.runAPI(ctx, nil, "bi", "--json", balancer)
	if err != nil {
		return "", err
	}
	var response struct {
		Balancer struct {
			Override struct {
				Target string `json:"target"`
			} `json:"override"`
		} `json:"balancer"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return "", fmt.Errorf("decode xray balancer response: %w", err)
	}
	if strings.TrimSpace(response.Balancer.Override.Target) == "" {
		return "", fmt.Errorf("xray balancer %q has no selected override", balancer)
	}
	return response.Balancer.Override.Target, nil
}

func (b RuntimeBackend) selectBalancerTarget(ctx context.Context, balancer, tag string) error {
	_, switchErr := b.runAPI(ctx, nil, "bo", "-b", balancer, tag)
	actual, readErr := b.selectedBalancerTarget(ctx, balancer)
	if readErr == nil && actual == tag {
		return nil
	}
	if switchErr != nil {
		if readErr != nil {
			return fmt.Errorf("%v; read actual target: %w", switchErr, readErr)
		}
		return fmt.Errorf("%v; actual target is %q", switchErr, actual)
	}
	if readErr != nil {
		return readErr
	}
	return fmt.Errorf("xray balancer %q target mismatch: requested %q, got %q", balancer, tag, actual)
}

func (b RuntimeBackend) SetProbeOutbound(ctx context.Context, slot int, tag string) error {
	if _, err := b.ProbeHTTPPort(slot); err != nil {
		return err
	}
	if err := validateManagedTag(tag, true); err != nil {
		return err
	}
	return b.selectBalancerTarget(ctx, fmt.Sprintf("fastlane-probe-%d", slot), tag)
}

func (b RuntimeBackend) ClearProbeOutbound(ctx context.Context, slot int) error {
	return b.SetProbeOutbound(ctx, slot, "block")
}

func (RuntimeBackend) ProbeHTTPPort(slot int) (int, error) {
	if slot < 0 || slot > 1 {
		return 0, fmt.Errorf("probe slot must be 0 or 1")
	}
	return probeHTTPPortBase + slot, nil
}

func (b RuntimeBackend) PersistConfig(ctx context.Context, req backend.ConfigRequest) error {
	rendered, err := b.GenerateConfig(req)
	if err != nil {
		return err
	}
	if err := b.validateConfig(ctx, rendered); err != nil {
		return err
	}
	if err := b.writer.Write(rendered); err != nil {
		return err
	}
	if err := writeRawConfig(b.backupPath, rendered); err != nil {
		return fmt.Errorf("write xray last-known-good config: %w", err)
	}
	return nil
}

func (b RuntimeBackend) outboundPresent(ctx context.Context, tag string) (bool, error) {
	out, err := b.runAPI(ctx, nil, "lso", "--json")
	if err != nil {
		return false, err
	}
	var response struct {
		Outbounds []struct {
			Tag string `json:"tag"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return false, fmt.Errorf("decode xray outbound list: %w", err)
	}
	for _, outbound := range response.Outbounds {
		if outbound.Tag == tag {
			return true, nil
		}
	}
	return false, nil
}

func (b RuntimeBackend) runAPI(ctx context.Context, stdin []byte, command string, args ...string) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, managedAPITimeout)
		defer cancel()
	}
	binary := xrayBinaryPath()
	runner := b.apiRunner
	if runner == nil {
		runner = execCommandRunner{}
	}
	commandArgs := []string{"api", command, "--server=" + managedAPIAddress, "--timeout=3"}
	commandArgs = append(commandArgs, args...)
	out, err := runner.Run(ctx, stdin, binary, commandArgs...)
	if err != nil {
		return nil, fmt.Errorf("xray api %s failed: %w: %s", command, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func validateManagedTag(tag string, allowBlock bool) error {
	if allowBlock && tag == "block" {
		return nil
	}
	if tag == managedDirectTag || strings.HasPrefix(tag, managedOutboundPrefix) {
		return nil
	}
	return fmt.Errorf("invalid managed outbound tag %q", tag)
}
