package run

import (
	"keyop/core"
	"keyop/x/heartbeat"
	"keyop/x/temp"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

func NewRunCmd(deps core.Dependencies) *cobra.Command {
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run Utility",
		Long:  `Spawn a process to run the extensions at scheduled intervals.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(deps)
		},
	}

	return runCmd
}

func run(deps core.Dependencies) error {
	deps.Logger.Info("run called")

	type CheckFunc func(core.Dependencies) error

	checks := []CheckFunc{
		heartbeat.Check,
		temp.Check,
	}

	var wg sync.WaitGroup

	runCheck := func(check CheckFunc) {
		defer wg.Done()

		// Execute check immediately
		if err := check(deps); err != nil {
			deps.Logger.Error("check", "error", err)
		}

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := check(deps); err != nil {
					deps.Logger.Error("check", "error", err)
				}
			case <-deps.Context.Done():
				return
			}
		}
	}

	// Start a goroutine for each check
	for _, check := range checks {
		wg.Add(1)
		go runCheck(check)
	}

	// Wait for context cancellation
	<-deps.Context.Done()
	wg.Wait()
	return deps.Context.Err()
}
