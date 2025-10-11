package run

import (
	"keyop/core"
	"sync"
	"time"
)

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
				// TODO: send to errors channel
			}

			// start a ticker to execute the check at the specified frequency
			ticker := time.NewTicker(serviceConfig.Freq)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					if err := service.Check(); err != nil {
						logger.Error("check", "error", err)
						// TODO: send to errors channel
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
