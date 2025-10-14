package run

import (
	"fmt"
	"keyop/core"
)

func validateServiceConfig(services []ServiceWrapper, logger core.Logger) error {
	// validate all service configs before initializing any services
	{
		var errCount int
		for _, serviceWrapper := range services {
			if serviceWrapper.Config.Name == "" {
				logger.Error("service config is missing the required field 'name'", "config", serviceWrapper.Config)
				errCount++
			}
			if serviceWrapper.Config.Type == "" {
				logger.Error("service config is missing the required field 'type'", "name", serviceWrapper.Config.Name)
				errCount++
			}

			logger.Info("validating service config", "name", serviceWrapper.Config.Name, "type", serviceWrapper.Config.Type)
			errs := serviceWrapper.Service.ValidateConfig()
			for _, err := range errs {
				logger.Error("service config validation error", "name", serviceWrapper.Config.Name, "error", err)
			}
		}
		if errCount > 0 {
			return fmt.Errorf("service configuration errors detected, see log for details")
		}

		logger.Info("all service configs validated successfully")
	}
	return nil
}
