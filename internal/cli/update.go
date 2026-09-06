package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/design-maestro/fastlane/internal/buildinfo"
	"github.com/design-maestro/fastlane/internal/platform/openwrt"
	"github.com/design-maestro/fastlane/internal/update"
	"github.com/spf13/cobra"
)

func newUpdateManager() *update.Manager {
	m := &update.Manager{Dir: update.RuntimeDir, Current: buildinfo.Current().Version, Arch: update.Architecture(), Client: update.NewClient()}
	m.Spawn = func(operation, token string) (int, error) {
		executable, err := os.Executable()
		if err != nil {
			return 0, err
		}
		child := exec.Command(executable, "update", "worker", operation, token)
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err = child.Start(); err != nil {
			return 0, err
		}
		pid := child.Process.Pid
		_ = child.Process.Release()
		return pid, nil
	}
	m.Install = func(ctx context.Context, candidate update.Candidate, script []byte) error {
		if !openwrt.IsOpenWrt() || os.Geteuid() != 0 {
			return fmt.Errorf("установка доступна только администратору OpenWrt/FriendlyWrt")
		}
		// Download and verify before handing control to the release installer.
		archive, err := update.Download(ctx, m.Client, candidate.Package, 64*1024*1024)
		if err != nil {
			return err
		}
		if err = update.ValidateArchive(archive); err != nil {
			return err
		}
		dir, err := os.MkdirTemp(m.Dir, "install-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		for name, data := range map[string][]byte{
			"install.sh":                       script,
			candidate.Package.Name:             archive,
			candidate.Package.Name + ".sha256": []byte(strings.TrimPrefix(candidate.Package.Digest, "sha256:") + "  " + candidate.Package.Name + "\n"),
		} {
			if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
				return err
			}
		}
		command := exec.CommandContext(ctx, "sh", filepath.Join(dir, "install.sh"), "--version", candidate.Version, "--arch", m.Arch, "--asset-dir", dir, "--without-deps")
		command.Env = append(os.Environ(), "TMPDIR="+dir)
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGKILL) }
		// No unbounded log or subscription/configuration data is persisted.
		if err = command.Run(); err != nil {
			return fmt.Errorf("установщик завершился с ошибкой; проверьте версию в диагностике: %w", err)
		}
		version, err := exec.CommandContext(ctx, "/usr/bin/fastlane", "--json", "version").Output()
		if err != nil {
			return err
		}
		var installed buildinfo.Info
		if err = json.Unmarshal(version, &installed); err != nil {
			return err
		}
		if strings.TrimPrefix(installed.Version, "v") != candidate.Version {
			return fmt.Errorf("версия после установки не совпала с %s", candidate.Version)
		}
		return nil
	}
	return m
}

func newUpdateCmd(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "update", Short: "Check and explicitly install stable GitHub releases"}
	cmd.AddCommand(&cobra.Command{Use: "status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := newUpdateManager().Status()
		if err != nil {
			return err
		}
		return printOutput(cmd, opts.jsonOutput, s, s.Message)
	}})
	cmd.AddCommand(&cobra.Command{Use: "check", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := newUpdateManager().Start("check", 0)
		if err != nil {
			return err
		}
		return printOutput(cmd, opts.jsonOutput, s, s.Message)
	}})
	var releaseID int64
	install := &cobra.Command{Use: "install", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := newUpdateManager().Start("install", releaseID)
		if err != nil {
			return err
		}
		return printOutput(cmd, opts.jsonOutput, s, s.Message)
	}}
	install.Flags().Int64Var(&releaseID, "release", 0, "ID of the checked release explicitly approved for installation")
	cmd.AddCommand(install)
	cmd.AddCommand(&cobra.Command{Use: "worker operation token", Hidden: true, Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		return newUpdateManager().Run(ctx, args[0], args[1])
	}})
	return cmd
}
