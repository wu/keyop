package run

import (
	"fmt"
	"keyop/core"

	"github.com/spf13/cobra"
)

func NewCmd(deps core.Dependencies) *cobra.Command {
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run Utility",
		Long: `Spawn a process to run the extensions at scheduled intervals.

This utility is a work in progress.

`,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := deps.MustGetLogger()

			// load the service configuration before calling the run method
			svcs, err := loadServices(deps)
			if err != nil {
				logger.Error("config load", "error", err)
				return err
			}
			if len(svcs) == 0 {
				logger.Error("config load", "error", "no services configured")
				return fmt.Errorf("no services configured")
			}
			return run(deps, svcs)
		},
	}

	return runCmd
}
