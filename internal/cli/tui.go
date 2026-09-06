package cli

import (
	"github.com/spf13/cobra"

	fastlanetui "github.com/design-maestro/fastlane/internal/tui"
)

func newTUICmd(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive terminal UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fastlanetui.Run(opts.service)
		},
	}
}
