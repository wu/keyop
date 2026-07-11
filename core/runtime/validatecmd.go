package runtime

import (
	"context"
	"fmt"
	"os"

	"github.com/wu/keyop/core"

	"github.com/spf13/cobra"
	km "github.com/wu/keyop-messenger"
)

// NewValidateCmd builds the validate-config subcommand. It loads service
// configs exactly the way `run` does (same directory resolution, templating,
// and plugin registration), constructs each service, and runs ValidateConfig()
// — but never calls Initialize() or starts the kernel, so it is safe to run
// against a staged config directory while another keyop instance is live.
func NewValidateCmd(deps core.Dependencies) *cobra.Command {
	var ignoreUnknown bool

	validateCmd := &cobra.Command{
		Use:   "validate-config [dir]",
		Short: "Validate service configuration without starting services",
		Long: `Load a service configuration directory, construct every configured service,
and run its ValidateConfig() checks. No service is initialized and nothing is
started, so this is safe to run against a staged config directory while
another keyop instance is running.

With no argument the normal config directory is used (KEYOP_CONF_DIR or
~/.keyop/conf). Exits non-zero if any config fails validation.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			logger := deps.MustGetLogger()

			// Point the shared directory resolution (service configs and
			// plugins.yaml) at the requested directory.
			if len(args) == 1 {
				if err := os.Setenv("KEYOP_CONF_DIR", args[0]); err != nil {
					return fmt.Errorf("setting KEYOP_CONF_DIR: %w", err)
				}
			}

			// Plugins register additional service types. Missing plugins.yaml
			// or missing .so files are skipped, matching `run` behavior.
			if err := LoadPlugins(deps); err != nil {
				logger.Error("plugin load", "error", err)
				return err
			}

			serviceConfigs, err := loadServiceConfigs(deps)
			if err != nil {
				logger.Error("config load", "error", err)
				return err
			}

			ctx := deps.MustGetContext()
			var services []ServiceWrapper
			var errCount int
			for _, serviceConfig := range serviceConfigs {
				constructor, ok := core.LookupService(serviceConfig.Type)
				if !ok {
					if ignoreUnknown {
						logger.Warn("service type not registered; skipping", "name", serviceConfig.Name, "type", serviceConfig.Type)
						continue
					}
					logger.Error("service type not registered", "name", serviceConfig.Name, "type", serviceConfig.Type)
					errCount++
					continue
				}

				svcCtx := km.WithServiceName(ctx, serviceConfig.Name)
				svcDeps := deps
				if raw := deps.GetMessenger(); raw != nil {
					svcDeps.SetMessenger(core.NewConfigMessenger(raw, serviceConfig.Subs))
				}

				service, err := constructForValidation(constructor, svcDeps, serviceConfig, svcCtx)
				if err != nil {
					logger.Error("service construction failed", "name", serviceConfig.Name, "type", serviceConfig.Type, "error", err)
					errCount++
					continue
				}

				services = append(services, ServiceWrapper{Service: service, Config: serviceConfig})
			}

			if err := validateServiceConfig(services, logger); err != nil {
				return err
			}
			if errCount > 0 {
				return fmt.Errorf("service configuration errors detected, see log for details")
			}

			logger.Info("configuration valid", "services", len(services))
			return nil
		},
	}

	validateCmd.Flags().BoolVar(&ignoreUnknown, "ignore-unknown", false,
		"skip services whose type is not registered (e.g. validating a config that uses plugins not present on this machine)")

	return validateCmd
}

// constructForValidation invokes a service constructor, converting panics into
// errors. Validation runs without a messenger (unlike `run`), so a constructor
// that dereferences one must not take the whole validation pass down with it.
func constructForValidation(constructor core.ServiceConstructor, deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) (svc core.Service, err error) {
	defer func() {
		if r := recover(); r != nil {
			svc = nil
			err = fmt.Errorf("constructor panicked: %v", r)
		}
	}()

	instance := constructor(deps, cfg, ctx)
	service, ok := instance.(core.Service)
	if !ok {
		return nil, fmt.Errorf("service type %s does not implement core.Service", cfg.Type)
	}
	return service, nil
}
