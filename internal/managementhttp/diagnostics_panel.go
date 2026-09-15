package managementhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/design-maestro/fastlane/internal/platform/openwrt"
	"github.com/design-maestro/fastlane/internal/update"
)

// SettingsPanelHost is an optional adapter for non-default configured hosts and
// isolated tests. A Service wrapper may implement SettingsPanelHost() returning
// this interface; no HandlerConfig change is necessary. RunUpdate must wait for
// completion, not merely enqueue. Paths must use the same root/config paths as
// the wrapped app.Service. They are never supplied by an HTTP request.
type SettingsPanelHost interface {
	Capabilities() SettingsPanelHostCapabilities
	UpdateStatus(context.Context) (update.State, error)
	RunUpdate(context.Context, string, int64) error
	Uninstall(context.Context) error
	Paths() map[string]string
}

type SettingsPanelHostCapabilities struct{ Update, Uninstall, Files bool }

func (h *Handler) settingsHost() SettingsPanelHost {
	if provider, ok := h.service.(interface{ SettingsPanelHost() SettingsPanelHost }); ok {
		return provider.SettingsPanelHost()
	}
	if openwrt.IsOpenWrt() {
		return openWrtPanelHost{}
	}
	return nil
}

type openWrtPanelHost struct{ rootDir, configPath string }

// NewOpenWrtSettingsPanelHost binds a non-default daemon --root/config path to
// diagnostics and fixed helpers. Return this from a Service wrapper's
// SettingsPanelHost() method. Empty paths use the CLI's FASTLANE_* defaults.
func NewOpenWrtSettingsPanelHost(rootDir, configPath string) SettingsPanelHost {
	if !openwrt.IsOpenWrt() {
		return nil
	}
	return openWrtPanelHost{rootDir: rootDir, configPath: configPath}
}

func panelExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
}

func (openWrtPanelHost) Capabilities() SettingsPanelHostCapabilities {
	return SettingsPanelHostCapabilities{Update: panelExecutable("/usr/bin/fastlane"), Uninstall: panelExecutable("/usr/libexec/fastlane-uninstall"), Files: true}
}

// Only these fixed helper paths and argument shapes may run. No shell, user
// path, URL, script, environment override, or arbitrary CLI command is accepted.
func (host openWrtPanelHost) updateCommand(ctx context.Context, args ...string) (update.State, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/fastlane", append([]string{"--json", "update"}, args...)...)
	cmd.Env = host.environment()
	output := &panelLimitedOutput{}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil {
		return update.State{}, panelJobError("update_helper_failed")
	}
	var state update.State
	if err := json.Unmarshal(output.Bytes(), &state); err != nil {
		return state, panelJobError("update_status_invalid")
	}
	if state.Status == "" {
		return state, panelJobError("update_status_invalid")
	}
	return state, nil
}

type panelLimitedOutput struct{ bytes.Buffer }

func (b *panelLimitedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 256*1024 {
		return 0, errors.New("helper output too large")
	}
	return b.Buffer.Write(p)
}

func (host openWrtPanelHost) UpdateStatus(ctx context.Context) (update.State, error) {
	return host.updateCommand(ctx, "status")
}

func (host openWrtPanelHost) RunUpdate(ctx context.Context, operation string, releaseID int64) error {
	return runPanelUpdate(ctx, operation, releaseID, host.updateCommand)
}

func runPanelUpdate(ctx context.Context, operation string, releaseID int64, command func(context.Context, ...string) (update.State, error)) error {
	args := []string{operation}
	if operation == "install" {
		if releaseID <= 0 {
			return panelJobError("invalid_release")
		}
		args = append(args, "--release", strconv.FormatInt(releaseID, 10))
	} else if operation != "check" {
		return panelJobError("invalid_request")
	}
	state, err := command(ctx, args...)
	if err != nil {
		return err
	}
	// The installed CLI owns the detached worker and verifies the release. Keep
	// the shared coordinator occupied until that worker actually finishes.
	ctx, cancel := context.WithTimeout(ctx, 16*time.Minute)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for state.Busy() {
		select {
		case <-ctx.Done():
			return panelJobError("update_completion_unconfirmed")
		case <-ticker.C:
		}
		state, err = command(ctx, "status")
		if err != nil {
			return err
		}
	}
	if operation == "install" {
		if state.Status != "updated" || state.Candidate == nil || state.Candidate.ID != releaseID {
			return panelJobError("update_install_failed")
		}
	} else if state.Status != "available" && state.Status != "current" && state.Status != "newer" {
		return panelJobError("update_check_failed")
	}
	return nil
}

func (host openWrtPanelHost) Uninstall(ctx context.Context) error {
	command := exec.Command("/usr/libexec/fastlane-uninstall", "--confirm")
	command.Env = host.environment()
	return startPanelUninstall(ctx, command)
}

func startPanelUninstall(ctx context.Context, command *exec.Cmd) error {
	if ctx.Err() != nil {
		return panelJobError("uninstall_cancelled")
	}
	// The helper stops this daemon before removing its files. It must survive
	// that stop: no CommandContext, inherited pipes, session or process group.
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	command.Stdin, command.Stdout, command.Stderr = nil, nil, nil
	if err := command.Start(); err != nil {
		return panelJobError("uninstall_failed")
	}
	_ = command.Process.Release()
	// The original helper has no durable completion receipt and may remove this
	// HTTP service. Admission is not proof of cleanup. Require external checking.
	return panelJobError("uninstall_completion_unconfirmed")
}

func (host openWrtPanelHost) Paths() map[string]string {
	root := openwrt.RootDir()
	if host.rootDir != "" {
		root = host.rootDir
	}
	config := openwrt.XrayConfigPath()
	if host.configPath != "" {
		config = host.configPath
	}
	return map[string]string{
		"fastlane_binary": "/usr/bin/fastlane", "fastlane_root": root,
		"subscriptions_file": filepath.Join(root, "subscriptions.json"),
		"settings_file":      filepath.Join(root, "settings.json"), "state_file": filepath.Join(root, "state.json"),
		"xray_config": config, "xray_service": openwrt.XrayServicePath(),
		"nft_binary": "/usr/sbin/nft", "firewall_rules": openwrt.FirewallRulesPath(),
	}
}

func (host openWrtPanelHost) environment() []string {
	values := os.Environ()
	for key, value := range map[string]string{"FASTLANE_ROOT": host.rootDir, "FASTLANE_XRAY_CONFIG": host.configPath} {
		if value == "" {
			continue
		}
		prefix := key + "="
		filtered := make([]string, 0, len(values)+1)
		for _, entry := range values {
			if !strings.HasPrefix(entry, prefix) {
				filtered = append(filtered, entry)
			}
		}
		values = append(filtered, prefix+value)
	}
	return values
}

type diagnosticsPanelFile struct {
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	Directory  bool   `json:"directory"`
	Executable bool   `json:"executable"`
	IsSymlink  bool   `json:"is_symlink"`
	Mode       string `json:"mode,omitempty"`
	ModifiedAt string `json:"modified_at,omitempty"`
	Error      string `json:"error,omitempty"`
}

func inspectPanelFile(path string) diagnosticsPanelFile {
	result := diagnosticsPanelFile{Path: path}
	info, err := os.Lstat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			result.Error = "file_status_failed"
		}
		return result
	}
	result.Exists, result.IsSymlink = true, info.Mode()&os.ModeSymlink != 0
	info, err = os.Stat(path)
	if err != nil {
		result.Error = "file_status_failed"
		return result
	}
	result.Directory, result.Executable = info.IsDir(), !info.IsDir() && info.Mode().Perm()&0111 != 0
	result.Mode, result.ModifiedAt = info.Mode().String(), info.ModTime().UTC().Format(time.RFC3339)
	return result
}
