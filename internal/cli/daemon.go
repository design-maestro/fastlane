package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/design-maestro/fastlane/internal/app"
	"github.com/design-maestro/fastlane/internal/managementhttp"
)

func newDaemonCmd(opts *rootOptions) *cobra.Command {
	var tick time.Duration
	var once bool
	var managementListen string
	var managementTokenFile string

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run background refresh and auto health monitoring",
		RunE: func(cmd *cobra.Command, args []string) error {
			if tick <= 0 {
				return fmt.Errorf("scheduler tick must be greater than zero")
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer stop()

			if err := opts.service.RestoreRuntime(ctx); err != nil {
				if _, writeErr := fmt.Fprintf(cmd.ErrOrStderr(), "restore runtime: %v\n", err); writeErr != nil {
					return writeErr
				}
			}

			scheduler := app.NewScheduler(opts.service)
			scheduler.SetTick(tick)
			scheduler.SetHealthCheck(func(checkCtx context.Context) {
				_ = runTrackedHealthCheck(checkCtx, opts, "", false)
			})
			scheduler.SetHealthTrigger(
				func() (string, bool) { return consumeHealthCheckRequest(healthCheckRequestPath(opts)) },
				func(checkCtx context.Context, scope string) {
					_ = runTrackedHealthCheck(checkCtx, opts, normalizeHealthScope(scope), true)
				},
			)

			if once {
				scheduler.RunOnce(ctx)
				return nil
			}

			var managementServer *managementhttp.Server
			var managementErrors chan error
			if strings.TrimSpace(managementListen) != "" {
				accessToken, err := readManagementAccessToken(managementTokenFile)
				if err != nil {
					return err
				}
				managementServer, err = managementhttp.NewServer(ctx, managementhttp.Config{
					ListenAddr:   managementListen,
					AccessToken:  accessToken,
					Logger:       opts.logger,
					RunExclusive: scheduler.RunExclusiveHealthOperation,
					HealthCheck: func(checkCtx context.Context) error {
						return runTrackedHealthCheck(checkCtx, opts, "", false)
					},
				}, opts.service)
				if err != nil {
					return err
				}
				managementErrors = make(chan error, 1)
				go func() {
					managementErrors <- managementServer.Run(ctx)
				}()
				if opts.logger != nil {
					opts.logger.Info("management HTTP started", "address", managementServer.Addr().String())
				}
			}

			scheduler.Start(ctx)
			if managementServer == nil {
				<-ctx.Done()
				scheduler.Stop()
				return nil
			}

			select {
			case <-ctx.Done():
				scheduler.Stop()
				return <-managementErrors
			case err := <-managementErrors:
				stop()
				scheduler.Stop()
				return err
			}
		},
	}

	cmd.Flags().DurationVar(&tick, "tick", time.Minute, "Background refresh scan interval")
	cmd.Flags().BoolVar(&once, "once", false, "Run one refresh scan and exit")
	cmd.Flags().StringVar(&managementListen, "management-listen", "", "Optional management HTTP address, for example 127.0.0.1:9080")
	cmd.Flags().StringVar(&managementTokenFile, "management-token-file", "", "File containing the management HTTP access token")

	return cmd
}

func readManagementAccessToken(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read management access token: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("management access token file must not be readable by group or other users")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read management access token: %w", err)
	}
	token := strings.TrimSpace(string(content))
	if token == "" {
		return "", fmt.Errorf("management access token file is empty")
	}
	return token, nil
}
