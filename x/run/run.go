package run

import (
	"fmt"
	"keyop/core"
	"sync"
	"time"

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

func run(deps core.Dependencies, serviceConfigs []core.ServiceConfig) error {
	logger := deps.MustGetLogger()
	logger.Info("run called")
	ctx := deps.MustGetContext()

	var wg sync.WaitGroup

	// start a goroutine for each check
	for _, check := range serviceConfigs {
		wg.Add(1)
		go func(serviceConfig core.ServiceConfig) {
			defer wg.Done()

			// pass service config to constructor
			service := ServiceRegistry[serviceConfig.Type](deps, serviceConfig)

			// execute first check immediately
			if err := service.Check(); err != nil {
				logger.Error("check", "error", err)
			}

			// start a ticker to execute the check at the specified frequency
			ticker := time.NewTicker(serviceConfig.Freq)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					if err := service.Check(); err != nil {
						logger.Error("check", "error", err)
					}
				case <-ctx.Done():
					return
				}
			}
		}(check)
	}

	// Wait for context cancellation
	<-ctx.Done()
	wg.Wait()
	return ctx.Err()
}
