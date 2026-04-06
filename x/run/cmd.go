// Package run contains helpers for executing and supervising configured services from the CLI.
package run

import (
	"keyop/core"

	"github.com/spf13/cobra"
)

// NewCmd builds the run subcommand responsible for loading plugins, services, and starting the kernel.
func NewCmd(deps core.Dependencies) *cobra.Command {
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run Utility",
		Long: `Spawn a process to run the extensions at scheduled intervals.

This utility is a work in progress.

`,
		RunE: func(_ *cobra.Command, _ []string) error {
			logger := deps.MustGetLogger()

			// 3. Load plugins (and register their payload types)
			// This must happen after registry is created (in InitializeDependencies)
			// and before services/subscribers start.
			if err := LoadPlugins(deps); err != nil {
				logger.Error("plugin load", "error", err)
				return err
			}

			// 4. Load the service configuration
			svcs, err := loadServiceConfigs(deps)
			if err != nil {
				logger.Error("config load", "error", err)
				return err
			}

			// 4b. Initialise the new keyop-messenger if messenger.yaml is present.
			newMsgr, bridge, err := initNewMessenger(deps)
			if err != nil {
				logger.Error("new messenger init", "error", err)
				return err
			}
			if newMsgr != nil {
				deps.SetNewMessenger(newMsgr)
				ctx := deps.MustGetContext()
				go func() {
					<-ctx.Done()
					if closeErr := newMsgr.Close(); closeErr != nil {
						logger.Error("new messenger close error", "error", closeErr)
					}
				}()
				if bridge != nil {
					bridge.Start(ctx)
				}
			}

			// 5. Start services/subscribers
			return run(deps, svcs)
		},
	}

	return runCmd
}
