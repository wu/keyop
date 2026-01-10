package run

import (
	"context"
	"fmt"
	"keyop/core"
	"os"
	"os/signal"
	"syscall"
)

type ServiceWrapper struct {
	Service core.Service
	Config  core.ServiceConfig
}

func run(deps core.Dependencies, serviceConfigs []core.ServiceConfig) error {
	ctx := deps.MustGetContext()
	logger := deps.MustGetLogger()
	logger.Info("run called")

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

	// initialize each service and create tasks
	var tasks []Task
	logger.Info("Initializing services")
	for _, serviceWrapper := range services {
		if err := serviceWrapper.Service.Initialize(); err != nil {
			logger.Error("service initialization failed", "error", err)
			return fmt.Errorf("service initialization failed: %w", err)
		}
		logger.Info("OK: Initialized service", "name", serviceWrapper.Config.Name)

		svcCtx, svcCancel := context.WithCancel(ctx)
		tasks = append(tasks, Task{
			Name:     serviceWrapper.Config.Name,
			Interval: serviceWrapper.Config.Freq,
			Run: func() error {
				return serviceWrapper.Service.Check()
			},
			Ctx:    svcCtx,
			Cancel: svcCancel,
		})
	}

	// shut down on signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-c
		logger.Warn("run: Received signal, shutting down", "signal", sig)
		cancel := deps.MustGetCancel()
		cancel()
	}()

	StartKernel(deps, tasks)

	logger.Warn("run: All tasks successfully shut down")

	return nil
}
