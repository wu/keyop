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
			// load the check configuration before calling the run method
			checks, err := loadChecks()
			if err != nil {
				deps.Logger.Error("config load", "error", err)
				return err
			}
			if len(checks) == 0 {
				deps.Logger.Error("config load", "error", "no checks configured")
				return fmt.Errorf("no checks configured")
			}
			return run(deps, checks)
		},
	}

	return runCmd
}

type Check struct {
	Name    string
	Freq    time.Duration
	X       string
	NewFunc func(core.Dependencies) core.Service
	Err     error
}

func run(deps core.Dependencies, checks []Check) error {
	deps.Logger.Info("run called")

	var wg sync.WaitGroup

	// start a goroutine for each check
	for _, check := range checks {
		wg.Add(1)
		go func(check Check) {
			defer wg.Done()

			obj := check.NewFunc(deps)

			// execute first check immediately
			if err := obj.Check(); err != nil {
				deps.Logger.Error("check", "error", err)
			}

			// start a ticker to execute the check at the specified frequency
			ticker := time.NewTicker(check.Freq)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					if err := obj.Check(); err != nil {
						deps.Logger.Error("check", "error", err)
					}
				case <-deps.Context.Done():
					return
				}
			}
		}(check)
	}

	// Wait for context cancellation
	<-deps.Context.Done()
	wg.Wait()
	return deps.Context.Err()
}
