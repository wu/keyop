// Package condition evaluates configured boolean conditions used across services to gate actions or generate alerts.
package condition

import (
	"fmt"
	"keyop/core"
	"keyop/util"
)

// Config is an alias for core.ConditionConfig kept for backwards compatibility.
type Config = core.ConditionConfig

// Service provides the condition evaluation engine that runs configured expressions and emits results for other services.
type Service struct {
	Deps       core.Dependencies
	Cfg        core.ServiceConfig
	Conditions []Config
}

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps: deps,
		Cfg:  cfg,
	}

	if condsRaw, ok := cfg.Config["conditions"].([]interface{}); ok {
		svc.Conditions = core.ParseConditions(condsRaw)
	}

	return svc
}

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("subs", svc.Cfg.Subs, []string{"source"}, logger)

	condsRaw, ok := svc.Cfg.Config["conditions"].([]interface{})
	if !ok || len(condsRaw) == 0 {
		errs = append(errs, fmt.Errorf("condition: 'conditions' must be a non-empty array"))
		return errs
	}

	errs = append(errs, core.ValidateConditions("condition", condsRaw)...)

	return errs
}

// Initialize performs one-time startup required by the service (resource loading or connectivity checks).
func (svc *Service) Initialize() error {
	messenger := svc.Deps.MustGetMessenger()
	return messenger.Subscribe(svc.Deps.MustGetContext(), svc.Cfg.Name, svc.Cfg.Subs["source"].Name, svc.Cfg.Type, svc.Cfg.Name, svc.Cfg.Subs["source"].MaxAge, svc.messageHandler)
}

func (svc *Service) messageHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	results := core.ApplyConditions(msg, svc.Conditions)
	for _, newMsg := range results {
		logger.Debug("Condition matched, publishing")
		newMsg.ChannelName = svc.Cfg.Name
		if err := messenger.Send(newMsg); err != nil {
			logger.Error("Failed to send updated message", "error", err)
		}
	}

	return nil
}

// Check performs the service's periodic work: collect data, evaluate state, and publish messages/metrics.
func (svc *Service) Check() error {
	return nil
}
