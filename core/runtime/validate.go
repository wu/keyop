package runtime

import (
	"fmt"
	"github.com/wu/keyop/core"
)

// validateServiceConfig checks every service's configuration before any service
// is initialized.
//
// lookup resolves a payload type string to its Go type, and is what lets a rule's
// field paths be checked here rather than silently doing nothing at runtime. It
// may be nil (no messenger configured), in which case rules still get their
// structural checks but not their type-specific ones.
func validateServiceConfig(services []ServiceWrapper, lookup core.PrototypeLookup, logger core.Logger) error {
	// validate all service configs before initializing any services
	// report all validation errors found before returning
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

			// Rules first: a rule naming a field that does not exist is a
			// configuration error, not something to discover on the first
			// message that happens to match.
			ruleErrs := core.ValidateRules(serviceWrapper.Config.Name+" pub_rules", serviceWrapper.Config.PubRules, lookup)
			ruleErrs = append(ruleErrs,
				core.ValidateRules(serviceWrapper.Config.Name+" sub_rules", serviceWrapper.Config.SubRules, lookup)...)
			for _, err := range ruleErrs {
				logger.Error("service rule validation error", "name", serviceWrapper.Config.Name, "type", serviceWrapper.Config.Type, "error", err)
				errCount++
			}

			errs := serviceWrapper.Service.ValidateConfig()
			for _, err := range errs {
				logger.Error("service config validation error", "name", serviceWrapper.Config.Name, "type", serviceWrapper.Config.Type, "error", err)
				errCount++
			}
		}
		if errCount > 0 {
			return fmt.Errorf("service configuration errors detected, see log for details")
		}

		logger.Info("all service configs validated successfully")
	}
	return nil
}

// payloadPrototypeLookup adapts the messenger's payload registry to the lookup
// ValidateRules expects. Returns nil when no messenger is configured, which is a
// supported state (messenger.yaml absent) rather than an error: rules then get
// their structural checks only.
func payloadPrototypeLookup(deps core.Dependencies) core.PrototypeLookup {
	msgr := deps.GetMessenger()
	if msgr == nil {
		return nil
	}
	return msgr.PayloadPrototype
}
