package run

import (
	"fmt"
	"keyop/core"
	"sync"
	"time"
)

type ServiceWrapper struct {
	Service core.Service
	Config  core.ServiceConfig
}

func run(deps core.Dependencies, serviceConfigs []core.ServiceConfig) error {
	logger := deps.MustGetLogger()
	logger.Info("run called")
	ctx := deps.MustGetContext()

	var wg sync.WaitGroup

	// iterate over service configs and create service instances
	logger.Info("Creating service instances")
	var services []ServiceWrapper
	for _, serviceConfig := range serviceConfigs {
		// pass service config to constructor
		serviceFunc, serviceFuncExists := ServiceRegistry[serviceConfig.Type]
		if !serviceFuncExists {
			logger.Error("service type not registered", "type", serviceConfig.Type)
			return fmt.Errorf("service type not registered: %s", serviceConfig.Type)
		}
		service := serviceFunc(deps, serviceConfig)
		services = append(services, ServiceWrapper{Service: service, Config: serviceConfig})
	}

	// validate all service configs before initializing any services
	logger.Info("Validating service configurations")
	err := validateServiceConfig(services, logger)
	if err != nil {
		return err
	}
	logger.Info("OK: Validation successful")

	// initialize each service
	logger.Info("Initializing services")
	for _, serviceWrapper := range services {
		if err := serviceWrapper.Service.Initialize(); err != nil {
			logger.Error("service initialization failed", "error", err)
			return fmt.Errorf("service initialization failed: %w", err)
		}
	}
	logger.Info("OK: Initialized services")

	// start a goroutine for each service to run its Check method at the specified frequency
	logger.Info("Starting service check loops")
	for _, serviceWrapper := range services {
		wg.Add(1)

		go func(serviceWrapper ServiceWrapper) {
			defer wg.Done()

			// execute first check immediately
			if err := serviceWrapper.Service.Check(); err != nil {
				logger.Error("check", "error", err)
				// TODO: send to errors channel
			}

			if serviceWrapper.Config.Freq > 0 {
				// start a ticker to execute the check at the specified frequency
				ticker := time.NewTicker(serviceWrapper.Config.Freq)
				defer ticker.Stop()

				for {
					select {
					case <-ticker.C:
						if err := serviceWrapper.Service.Check(); err != nil {
							logger.Error("check", "error", err)
							// TODO: send to errors channel
						}
					case <-ctx.Done():
						logger.Error("context done, exiting check loop", "service", serviceWrapper.Service)
						return
					}
				}
			}
		}(serviceWrapper)
	}

	logger.Warn("Waiting for context cancellation")
	<-ctx.Done()

	logger.Warn("Waiting for all checks to complete")
	wg.Wait()

	logger.Warn("All checks completed, exiting")
	return ctx.Err()
}
