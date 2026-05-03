package runtime

import (
	"context"
	"fmt"
	"github.com/wu/keyop/core"
	"os"
	"os/signal"
	"syscall"

	km "github.com/wu/keyop-messenger"
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
		serviceFunc, serviceFuncExists := core.LookupService(serviceConfig.Type)
		if !serviceFuncExists {
			logger.Error("service type not registered", "type", serviceConfig.Type)
			return fmt.Errorf("service type not registered: %s", serviceConfig.Type)
		}

		// Create a service-specific context with the service name stamped on it
		svcCtx := km.WithServiceName(ctx, serviceConfig.Name)
		svcInstance := serviceFunc(deps, serviceConfig, svcCtx)
		service, ok := svcInstance.(core.Service)
		if !ok {
			logger.Error("service instance does not implement core.Service", "type", serviceConfig.Type)
			return fmt.Errorf("service type %s does not implement core.Service", serviceConfig.Type)
		}

		// Check if service implements core.SchemaProvider
		if provider, ok := service.(core.SchemaProvider); ok {
			// Require provider to declare payload types; register it keyed by payload
			// type with any matching sqlite services. Providers that do not declare
			// payload types will not be registered (payload-only registration).
			if ptp, ok := service.(core.PayloadTypeProvider); ok {
				for _, payloadType := range ptp.PayloadTypes() {
					for _, other := range services {
						if sqliteCoord, ok := other.Service.(core.SQLiteCoordinator); ok {
							if sqliteCoord.AcceptsPayloadType(payloadType) {
								sqliteCoord.RegisterProvider(payloadType, provider)
							}
						}
					}
				}
			} else {
				logger.Warn("sqlite schema provider does not declare payload types; skipping registration", "service", serviceConfig.Name)
			}
		}

		// Check if service implements core.SQLiteConsumer
		if consumer, ok := service.(core.SQLiteConsumer); ok {
			// Require consumer to declare payload types so we can match sqlite services.
			if ptp, ok := service.(core.PayloadTypeProvider); ok {
				assigned := false
				for _, payloadType := range ptp.PayloadTypes() {
					for _, other := range services {
						if sqliteCoord, ok := other.Service.(core.SQLiteCoordinator); ok {
							if sqliteCoord.AcceptsPayloadType(payloadType) {
								consumer.SetSQLiteDB(sqliteCoord.GetSQLiteDB())
								assigned = true
								break
							}
						}
					}
					if assigned {
						break
					}
				}
				if !assigned {
					logger.Debug("sqlite consumer did not match any sqlite service for payload types", "service", serviceConfig.Name)
				}
			} else {
				logger.Warn("sqlite consumer does not declare payload types; not providing DB", "service", serviceConfig.Name)
			}
		}

		// Check if service implements RegisterPayloadTypes for the new messenger
		if regFn, ok := service.(interface {
			RegisterPayloadTypes(newMsgr interface {
				RegisterPayloadType(typeStr string, prototype any) error
			}, logger core.Logger) error
		}); ok {
			newMsgr := deps.MustGetMessenger()
			if newMsgr != nil {
				if err := regFn.RegisterPayloadTypes(newMsgr, logger); err != nil {
					logger.Error("Service failed to register payload types", "name", serviceConfig.Name, "error", err)
					return fmt.Errorf("service payload type registration failed: %w", err)
				}
				logger.Info("Service registered payload types", "name", serviceConfig.Name)
			} else {
				logger.Warn("new messenger not available; skipping payload registration", "name", serviceConfig.Name)
			}
		}

		// Check if service implements core.TabProvider
		if provider, ok := service.(core.TabProvider); ok {
			for _, other := range services {
				if webuiCoord, ok := other.Service.(core.WebUICoordinator); ok {
					webuiCoord.RegisterProvider(serviceConfig.Type, provider)
				}
			}
		}

		// Check if service implements core.PanelProvider
		if panelProv, ok := service.(core.PanelProvider); ok {
			for _, other := range services {
				if webuiCoord, ok := other.Service.(core.WebUICoordinator); ok {
					webuiCoord.RegisterPanelProvider(serviceConfig.Type, panelProv)
				}
			}
		}

		// Check if service implements core.MCPToolProvider
		if toolProv, ok := service.(core.MCPToolProvider); ok {
			for _, other := range services {
				if mcpCoord, ok := other.Service.(core.MCPCoordinator); ok {
					mcpCoord.RegisterMCPToolProvider(toolProv)
				}
			}
		}

		services = append(services, ServiceWrapper{Service: service, Config: serviceConfig})

		// If this is the sqlite coordinator, backfill previously created services
		if sqliteCoord, ok := service.(core.SQLiteCoordinator); ok {
			for _, other := range services[:len(services)-1] {
				if provider, ok := other.Service.(core.SchemaProvider); ok {
					if ptp, ok := other.Service.(core.PayloadTypeProvider); ok {
						for _, payloadType := range ptp.PayloadTypes() {
							if sqliteCoord.AcceptsPayloadType(payloadType) {
								sqliteCoord.RegisterProvider(payloadType, provider)
							}
						}
					} else {
						logger.Warn("previous service is sqlite schema provider but does not declare payload types; skipping", "service", other.Config.Name)
					}
				}
				if consumer, ok := other.Service.(core.SQLiteConsumer); ok {
					if ptp, ok := other.Service.(core.PayloadTypeProvider); ok {
						assigned := false
						for _, payloadType := range ptp.PayloadTypes() {
							if sqliteCoord.AcceptsPayloadType(payloadType) {
								consumer.SetSQLiteDB(sqliteCoord.GetSQLiteDB())
								assigned = true
								break
							}
						}
						if !assigned {
							logger.Debug("sqlite consumer did not match any payloadTypes for sqlite service", "service", other.Config.Name)
						}
					} else {
						logger.Warn("previous service is sqlite consumer but does not declare payload types; not setting DB", "service", other.Config.Name)
					}
				}
			}
		}

		// If this is the webui coordinator, self-register and backfill previously created services
		if webuiCoord, ok := service.(core.WebUICoordinator); ok {
			// Self-register so the webui's own tab and assets are available
			if tab, ok := service.(core.TabProvider); ok {
				webuiCoord.RegisterProvider(webuiCoord.ServiceType(), tab)
			}
			for _, other := range services[:len(services)-1] {
				if provider, ok := other.Service.(core.TabProvider); ok {
					webuiCoord.RegisterProvider(other.Config.Type, provider)
				}
				if panelProv, ok := other.Service.(core.PanelProvider); ok {
					webuiCoord.RegisterPanelProvider(other.Config.Type, panelProv)
				}
			}
		}

		// If this is the MCP coordinator, backfill previously created services
		if mcpCoord, ok := service.(core.MCPCoordinator); ok {
			for _, other := range services[:len(services)-1] {
				if toolProv, ok := other.Service.(core.MCPToolProvider); ok {
					mcpCoord.RegisterMCPToolProvider(toolProv)
				}
			}
		}

		// Note: IndexProvider registration moved to final pass below to ensure
		// deduplication and that search service is fully initialized
	}

	// Register all IndexProviders with search service (after all services created)
	// This is the only registration point to avoid duplicates
	var searchCoord core.SearchCoordinator
	for _, sw := range services {
		if coord, ok := sw.Service.(core.SearchCoordinator); ok {
			searchCoord = coord
			break
		}
	}
	if searchCoord != nil {
		// Track which providers have already been registered to avoid duplicates
		registered := make(map[string]bool)
		for _, sw := range services {
			if indexProv, ok := sw.Service.(core.IndexProvider); ok {
				sourceType := indexProv.SearchSourceType()
				if !registered[sourceType] {
					searchCoord.RegisterIndexProvider(indexProv)
					registered[sourceType] = true
				}
			}
		}
	}

	// validate all service configs before initializing any services
	// Propagate sqlite DB path mappings from the webui service configuration.
	// Only the 'dbPaths' key is accepted (mapping: payloadType -> dbPath).
	for _, sw := range services {
		if _, ok := sw.Service.(core.WebUICoordinator); !ok {
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
		// Apply mapping to sqlite coordinators
		for _, other := range services {
			if sqliteCoord, ok := other.Service.(core.SQLiteCoordinator); ok {
				for payloadType, path := range mapping {
					if sqliteCoord.AcceptsPayloadType(payloadType) {
						sqliteCoord.SetDBPath(payloadType, path)
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
