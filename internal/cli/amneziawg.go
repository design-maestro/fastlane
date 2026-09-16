package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

const maxAWGProfileBytes = 1024 * 1024

func newAWGCmd(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "awg", Short: "Manage one experimental AmneziaWG Legacy or 2.0 profile"}
	cmd.AddCommand(
		newAWGStatusCmd(opts),
		newAWGImportCmd(opts),
		newAWGConnectCmd(opts),
		newAWGCheckCmd(opts),
		newAWGDisconnectCmd(opts),
		newAWGRemoveCmd(opts),
	)
	return cmd
}

func newAWGStatusCmd(opts *rootOptions) *cobra.Command {
	return &cobra.Command{Use: "status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		status, err := opts.service.GetAWGStatus(cmd.Context())
		if err != nil {
			return err
		}
		return printOutput(cmd, opts.jsonOutput, status, fmt.Sprintf("AmneziaWG: %s", status.State))
	}}
}

func newAWGImportCmd(opts *rootOptions) *cobra.Command {
	var path string
	var fromStdin bool
	var name string
	cmd := &cobra.Command{Use: "import", Short: "Import one native AWG Legacy or 2.0 .conf", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if (strings.TrimSpace(path) == "") == !fromStdin {
			return fmt.Errorf("use exactly one of --file or --stdin")
		}
		var reader io.Reader = cmd.InOrStdin()
		if path != "" {
			opened, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("open AmneziaWG profile: %w", err)
			}
			reader = opened
			defer opened.Close()
		}
		raw, err := io.ReadAll(io.LimitReader(reader, maxAWGProfileBytes+1))
		if path != "" && strings.HasPrefix(path, "/var/run/fastlane/") {
			defer os.Remove(path)
		}
		if err != nil {
			return fmt.Errorf("read AmneziaWG profile: %w", err)
		}
		if len(raw) > maxAWGProfileBytes {
			return fmt.Errorf("AmneziaWG profile exceeds 1 MiB")
		}
		status, err := opts.service.ImportAWGProfile(name, raw)
		if err != nil {
			return err
		}
		return printOutput(cmd, opts.jsonOutput, status, "AmneziaWG profile imported")
	}}
	cmd.Flags().StringVar(&path, "file", "", "Read profile from a file")
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "Read profile from standard input")
	cmd.Flags().StringVar(&name, "name", "", "Display name")
	return cmd
}

func newAWGConnectCmd(opts *rootOptions) *cobra.Command {
	return &cobra.Command{Use: "connect", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if err := opts.service.ConnectAWG(cmd.Context()); err != nil {
			return err
		}
		status, err := opts.service.GetAWGStatus(cmd.Context())
		if err != nil {
			return err
		}
		return printOutput(cmd, opts.jsonOutput, status, "AmneziaWG connected")
	}}
}

func newAWGCheckCmd(opts *rootOptions) *cobra.Command {
	return &cobra.Command{Use: "check", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		status, err := opts.service.CheckAWG(cmd.Context())
		if err != nil {
			return err
		}
		return printOutput(cmd, opts.jsonOutput, status, "AmneziaWG check passed")
	}}
}

func newAWGDisconnectCmd(opts *rootOptions) *cobra.Command {
	return &cobra.Command{Use: "disconnect", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if err := opts.service.DisconnectAWG(cmd.Context()); err != nil {
			return err
		}
		return printOutput(cmd, opts.jsonOutput, map[string]string{"state": "direct"}, "AmneziaWG disconnected")
	}}
}

func newAWGRemoveCmd(opts *rootOptions) *cobra.Command {
	return &cobra.Command{Use: "remove", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if err := opts.service.RemoveAWG(cmd.Context()); err != nil {
			return err
		}
		return printOutput(cmd, opts.jsonOutput, map[string]string{"state": "absent"}, "AmneziaWG profile removed")
	}}
}
