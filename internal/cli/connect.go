package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newConnectCmd(opts *rootOptions) *cobra.Command {
	var subscriptionID string
	var nodeID string
	var auto bool
	var latencyMS float64
	var global bool

	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Connect to a specific node or enable auto mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			if auto {
				if nodeID != "" {
					if subscriptionID == "" {
						return fmt.Errorf("--subscription is required with --auto --node")
					}
					node, err := opts.service.ConnectAutoSelected(context.Background(), subscriptionID, nodeID, latencyMS, global)
					if err != nil {
						return err
					}
					return printOutput(cmd, opts.jsonOutput, node, fmt.Sprintf("Auto selected %s (%s) by HTTPS GET", node.DisplayName(), node.ID))
				}
				node, err := opts.service.ConnectAuto(context.Background(), subscriptionID)
				if err != nil {
					return err
				}
				if node.ID == "" {
					return printOutput(
						cmd,
						opts.jsonOutput,
						map[string]string{"subscription": subscriptionID, "transport": "zapret"},
						fmt.Sprintf("Auto mode enabled Zapret fallback for %s", autoScopeLabel(subscriptionID)),
					)
				}
				return printOutput(cmd, opts.jsonOutput, node, fmt.Sprintf("Auto selected %s (%s)", node.DisplayName(), node.ID))
			}
			if subscriptionID == "" {
				return fmt.Errorf("--subscription is required for manual connect")
			}

			if err := opts.service.ConnectManual(context.Background(), subscriptionID, nodeID); err != nil {
				return err
			}

			return printOutput(cmd, opts.jsonOutput, map[string]string{"subscription": subscriptionID, "node": nodeID}, fmt.Sprintf("Connected %s/%s", subscriptionID, nodeID))
		},
	}

	cmd.Flags().StringVar(&subscriptionID, "subscription", "", "Subscription ID or unique prefix")
	cmd.Flags().StringVar(&nodeID, "node", "", "Node ID")
	cmd.Flags().BoolVar(&auto, "auto", false, "Automatically select the best node")
	cmd.Flags().Float64Var(&latencyMS, "latency-ms", 0, "Measured HTTPS GET latency in milliseconds")
	cmd.Flags().BoolVar(&global, "global", false, "Keep auto mode scoped to all subscriptions")
	return cmd
}

func autoScopeLabel(subscriptionID string) string {
	if subscriptionID == "" || subscriptionID == "all" {
		return "all subscriptions"
	}
	return subscriptionID
}
