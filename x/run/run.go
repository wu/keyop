package run

import (
	"context"
	"fmt"
	"keyop/core"
	"keyop/x/sqlite"
	"keyop/x/webui"
	"os"
	"os/signal"
	"syscall"
)

// ServiceWrapper bundles a constructed service instance with its core configuration and constructor info used by the runtime.
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

		// Check if service implements sqlite.SchemaProvider
		if provider, ok := service.(sqlite.SchemaProvider); ok {
			// Find the sqlite service if it exists and accepts this provider's service type
			for _, other := range services {
				if sqliteSvc, ok := other.Service.(*sqlite.Service); ok {
					if sqliteSvc.AcceptsServiceType(serviceConfig.Type) {
						sqliteSvc.RegisterProvider(serviceConfig.Type, provider)
					}
				}
			}
		}

		// Check if service implements sqlite.Consumer
		if consumer, ok := service.(sqlite.Consumer); ok {
			// Find the sqlite service if it exists and accepts this consumer's service type
			for _, other := range services {
				if sqliteSvc, ok := other.Service.(*sqlite.Service); ok {
					if sqliteSvc.AcceptsServiceType(serviceConfig.Type) {
						consumer.SetSQLiteDB(sqliteSvc.GetSQLiteDB())
					}
				}
			}
		}

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

		// Check if service implements webui.TabProvider
		if provider, ok := service.(webui.TabProvider); ok {
			// Find the webui service if it exists
			for _, other := range services {
				if webuiSvc, ok := other.Service.(*webui.Service); ok {
					webuiSvc.RegisterProvider(serviceConfig.Type, provider)
				}
			}
		}

		// Check if service implements webui.PanelProvider
		if panelProv, ok := service.(webui.PanelProvider); ok {
			for _, other := range services {
				if webuiSvc, ok := other.Service.(*webui.Service); ok {
					webuiSvc.RegisterPanelProvider(serviceConfig.Type, panelProv)
				}
			}
		}

		services = append(services, ServiceWrapper{Service: service, Config: serviceConfig})

		// If this is the sqlite service, check previously created services for SchemaProvider or SQLiteConsumer
		if sqliteSvc, ok := service.(*sqlite.Service); ok {
			for _, other := range services[:len(services)-1] {
				if provider, ok := other.Service.(sqlite.SchemaProvider); ok {
					if sqliteSvc.AcceptsServiceType(other.Config.Type) {
						sqliteSvc.RegisterProvider(other.Config.Type, provider)
					}
				}
				if consumer, ok := other.Service.(sqlite.Consumer); ok {
					if sqliteSvc.AcceptsServiceType(other.Config.Type) {
						consumer.SetSQLiteDB(sqliteSvc.GetSQLiteDB())
					}
				}
			}
		}

		// If this is the webui service, check previously created services for TabProvider
		if webuiSvc, ok := service.(*webui.Service); ok {
			// Self-register so the webui's own tab and assets are available
			webuiSvc.RegisterProvider(webuiSvc.Cfg.Type, webuiSvc)
			for _, other := range services[:len(services)-1] {
				if provider, ok := other.Service.(webui.TabProvider); ok {
					webuiSvc.RegisterProvider(other.Config.Type, provider)
				}
				if panelProv, ok := other.Service.(webui.PanelProvider); ok {
					webuiSvc.RegisterPanelProvider(other.Config.Type, panelProv)
				}
			}
		}
	}

	// validate all service configs before initializing any services
	// Propagate sqlite DB path mappings from the webui service config.
	// Only the 'dbPaths' key is accepted (mapping: serviceType -> dbPath).
	for _, sw := range services {
		if _, ok := sw.Service.(*webui.Service); !ok {
			continue
		}
		raw, ok := sw.Config.Config["dbPaths"]
		if !ok {
			continue
		}
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		mapping := make(map[string]string)
		for k, v := range m {
			if s, ok := v.(string); ok && s != "" {
				mapping[k] = s
			}
		}
		if len(mapping) == 0 {
			continue
		}
		// Apply mapping to sqlite services
		for _, other := range services {
			if sqliteSvc, ok := other.Service.(*sqlite.Service); ok {
				for svcType, path := range mapping {
					if sqliteSvc.AcceptsServiceType(svcType) {
						if sqliteSvc.Cfg.Config == nil {
							sqliteSvc.Cfg.Config = make(map[string]interface{})
						}
						sqliteSvc.Cfg.Config["dbPath"] = path
					}
				}
			}
		}
	}
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
