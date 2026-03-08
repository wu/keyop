package speak

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"runtime"
	"strconv"
	"time"
)

// SpeechEvent represents a spoken-text event emitted by the 'speak' service.
// It intentionally carries only a timestamp and the spoken text in the Text field.
type SpeechEvent struct {
	Now     time.Time `json:"now"`
	Summary string    `json:"summary"`
}

// PayloadType returns the canonical payload type for speech events.
func (e SpeechEvent) PayloadType() string { return "service.speech.v1" }

// Service converts text payloads into spoken audio using the macOS speech synthesis APIs
type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig

	// rate limiter
	limiter *util.RateLimiter
}

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

// RegisterPayloads registers the speak service payload types with the provided registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	if err := reg.Register("speech", func() any { return &SpeechEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register speech alias: %w", err)
		}
	}
	if err := reg.Register("service.speech.v1", func() any { return &SpeechEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register service.speech.v1: %w", err)
		}
	}
	return nil
}

// Name returns the canonical name of the 'speak' service type.
func (svc *Service) Name() string { return "speak" }

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	var errs []error

	if runtime.GOOS != "darwin" {
		err := fmt.Errorf("this service currently only supports MacOS")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	// validate required subscriptions
	errs = append(errs, util.ValidateConfig("subs", svc.Cfg.Subs, []string{"alerts"}, logger)...)
	return errs
}

// Initialize subscribes to the configured 'alerts' channel
func (svc *Service) Initialize() error {
	// create rate limiter from config
	limit := 5
	if svc.Cfg.Config != nil {
		if v, ok := svc.Cfg.Config["rate_limit_per_minute"]; ok {
			switch t := v.(type) {
			case int:
				limit = t
			case int64:
				limit = int(t)
			case float64:
				limit = int(t)
			case string:
				if n, err := strconv.Atoi(t); err == nil {
					limit = n
				}
			}
		}
	}
	svc.limiter = util.NewRateLimiter(limit)

	messenger := svc.Deps.MustGetMessenger()
	return messenger.Subscribe(svc.Deps.MustGetContext(), svc.Cfg.Name, svc.Cfg.Subs["alerts"].Name, svc.Cfg.Type, svc.Cfg.Name, svc.Cfg.Subs["alerts"].MaxAge, svc.messageHandler)
}

func (svc *Service) messageHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	var text string
	if msg.Summary != "" {
		text = msg.Summary
	} else if msg.Text != "" {
		text = msg.Text
	} else {
		return nil
	}

	correlation := ""
	if msg.Correlation != "" {
		correlation = msg.Correlation
	} else if msg.Uuid != "" {
		correlation = msg.Uuid
	}

	now := time.Now()
	allowed, firstDrop := svc.limiter.AddEventAt(now)
	if !allowed {
		logger.Warn("Rate limit exceeded", "count", svc.limiter.Total())
		if firstDrop {
			summary := "Too many text alerts; some alerts have been skipped."
			osProvider := svc.Deps.MustGetOsProvider()
			cmd := osProvider.Command("say", summary)
			if err := cmd.Run(); err != nil {
				logger.Error("Failed to execute say for rate-limit summary", "error", err)
			}
			// emit rate_limit event for the summary
			e := SpeechEvent{Now: time.Now(), Summary: summary}
			if sendErr := messenger.Send(core.Message{
				Correlation: correlation,
				ChannelName: svc.Cfg.Name,
				ServiceName: svc.Cfg.Name,
				ServiceType: svc.Cfg.Type,
				Event:       "rate_limit",
				Summary:     summary,
				Text:        summary,
				Data:        e,
			}); sendErr != nil {
				logger.Error("Failed to send rate-limit event", "error", sendErr)
			}
		}
		// drop the original alert
		return nil
	}

	logger.Info("Speaking text", "text", text)
	osProvider := svc.Deps.MustGetOsProvider()
	cmd := osProvider.Command("say", text)
	if err := cmd.Run(); err != nil {
		logger.Error("Failed to execute say command", "error", err)
		// send error event with the error message
		if sendErr := messenger.Send(core.Message{
			Correlation: correlation,
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "error",
			Status:      "error",
			Text:        err.Error(),
		}); sendErr != nil {
			logger.Error("Failed to send error event", "error", sendErr)
		}
		return err
	}

	// On success, emit a minimal speech event with the spoken text in the Text.
	e := SpeechEvent{Now: time.Now(), Summary: text}
	if sendErr := messenger.Send(core.Message{
		Correlation: correlation,
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "speech",
		Text:        text,
		Data:        e,
	}); sendErr != nil {
		logger.Error("Failed to send speech event", "error", sendErr)
	}

	return nil
}

// Check is a no-op for this service, it only reacts to incoming messages from a subscription.
func (svc *Service) Check() error {
	return nil
}
