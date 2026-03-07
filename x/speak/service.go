// Package speak implements a service that converts incoming messages into spoken audio.
// The service will speak the 'Summary' field if it exists, or the 'Text' field if the
// 'Summary' field is empty.
//
// Currently, it only works on macOS, as it relies on the 'say' command to speak text.
// The service validates that it runs on Darwin (macOS) and will return a configuration error
// on other platforms.
//
// When 'say' exits with success, a "speech" event (payload type service.speech.v1) will be emitted
// with the spoken text in the Message Summary.  If there is an error returned from the say command,
// an error event will be emitted with the error details.
//
// Rate limiting
//   - The service supports a per-minute rate limit controlled by the configuration key
//     `rate_limit_per_minute` (integer). If not specified, the default is 5 events per minute.
//   - The limiter uses a rolling 60 second window divided into 10 buckets (6s each). Events are
//     counted into the current bucket; when the total across all buckets exceeds the configured
//     limit, further incoming messages are dropped until the window advances.
//   - When the rate limit is first exceeded, the service emits a "rate_limit" event with a short
//     summary indicating that alerts were skipped. Subsequent dropped events do not re-emit the
//     summary until an allowed event resets the warning state.
//
// # MACOS SPECIFIC NOTES
//
// To use the higher quality siri voices on macOS, it uses the default "system voice"
// setting.  While it is possible to specify a voice to the 'say' command, the choices are
// limited and don't include the highest quality voices.
//
//   - First, in "Apple Intelligence and Siri", select your preferred Siri voice.
//   - Second, in System Preferences > Accessibility => "Read and Speak", set the system voice.
//
// Unfortunately, the exact steps to configure this vary by macOS version.  These instructions are
// for Tahoe.  Try to search in Preferences for 'voice', look for something like "Voice (spoken content)".
// In the system voice drop-down, choose the siri voice option, mine was near the top and was
// named "Siri (Voice 2)".
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
// It intentionally carries only a timestamp and the spoken text in the Summary field.
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

	// On success, emit a minimal speech event with the spoken text in the Summary.
	e := SpeechEvent{Now: time.Now(), Summary: text}
	if sendErr := messenger.Send(core.Message{
		Correlation: correlation,
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "speech",
		Summary:     text,
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
