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

		// Build per-service deps, wrapping the messenger with preprocessing if configured.
		serviceDeps := deps
		subConds, pubConds := core.ParsePreprocessConditions(serviceConfig)
		if len(subConds) > 0 || len(pubConds) > 0 {
			serviceDeps = deps.Clone()
			serviceDeps.SetMessenger(core.NewPreprocessMessenger(deps.MustGetMessenger(), subConds, pubConds))
			logger.Info("Preprocessing enabled for service", "name", serviceConfig.Name,
				"sub_preprocess_rules", len(subConds), "pub_preprocess_rules", len(pubConds))
		}

		service := serviceFunc(serviceDeps, serviceConfig)

		// Check if service implements RuntimePlugin for payload registration
		if rtPlugin, ok := service.(core.RuntimePlugin); ok {
			reg := serviceDeps.MustGetMessenger().GetPayloadRegistry()
			if reg != nil {
				// Built-in services might already have their payloads registered if they are multiple instances,
				// but RegisterPayloads should handle (or reg.Register handles) duplicates.
				// In core.Register, we return an error for duplicates.
				if err := rtPlugin.RegisterPayloads(reg); err != nil {
					// We allow duplicates for built-in services as they might have multiple instances
					if core.IsDuplicatePayloadRegistration(err) {
						logger.Debug("Service payload already registered", "name", serviceConfig.Name, "info", err)
					} else {
						logger.Error("Service failed payload registration", "name", serviceConfig.Name, "error", err)
						return fmt.Errorf("service payload registration failed: %w", err)
					}
				} else {
					logger.Info("Service registered payloads", "name", serviceConfig.Name)
				}
			}
		}

		services = append(services, ServiceWrapper{Service: service, Config: serviceConfig})
	}

	// validate all service configs before initializing any services
	logger.Info("Validating service configurations")
	err := validateServiceConfig(services, logger)
	if err != nil {
		logger.Error("ERROR: Validation failed", "error", err)
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
		task := Task{
			Name:     serviceWrapper.Config.Name,
			Interval: serviceWrapper.Config.Freq,
			Run: func() error {
				return serviceWrapper.Service.Check()
			},
			Ctx:    svcCtx,
			Cancel: svcCancel,
		}
		if errorChan, ok := serviceWrapper.Config.Pubs["errors"]; ok {
			task.ErrorChannelName = errorChan.Name
		}
		tasks = append(tasks, task)
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

	if err := StartKernel(deps, tasks); err != nil {
		return fmt.Errorf("run: kernel returned an error: %w", err)
	}

	logger.Warn("run: All tasks successfully shut down")

	return nil
}
