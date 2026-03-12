package speak

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Event represents a spoken/text event emitted by the 'speak' service.
// It contains a timestamp, a short summary, whether it was sent, and optional details.
type Event struct {
	Now     time.Time `json:"now"`
	Summary string    `json:"summary"`
	Sent    bool      `json:"sent"`
	Details string    `json:"details,omitempty"`
}

// PayloadType returns the canonical payload type for speak events.
func (e Event) PayloadType() string { return "service.speak.v1" }

// Service converts text payloads into spoken audio using the macOS speech synthesis APIs
type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig

	// rate limiter
	limiter *util.RateLimiter

	// report queue template and last report day
	queueFileTemplate string
	lastReportDay     time.Time
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
	if err := reg.Register("speak", func() any { return &Event{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register speak alias: %w", err)
		}
	}
	if err := reg.Register("service.speak.v1", func() any { return &Event{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register service.speak.v1: %w", err)
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
		// This service only functions on macOS, but don't fail ValidateConfig during
		// cross-platform testing or builds; emit a warning instead.
		logger.Warn("speak: this service only supports macOS; functionality will be limited on this OS")
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
	// read optional report_queue_file config
	if qf, ok := svc.Cfg.Config["report_queue_file"].(string); ok && qf != "" {
		// expand ~ to home directory
		if strings.HasPrefix(qf, "~") {
			if home, herr := svc.Deps.MustGetOsProvider().UserHomeDir(); herr == nil {
				if strings.HasPrefix(qf, "~/") {
					qf = filepath.Join(home, qf[2:])
				} else {
					qf = filepath.Join(home, qf[1:])
				}
			}
		}
		svc.queueFileTemplate = qf
	} else {
		svc.queueFileTemplate = ""
	}

	if svc.queueFileTemplate != "" {
		// load state
		var state ServiceState
		if err := svc.Deps.MustGetStateStore().Load(svc.Cfg.Name, &state); err == nil {
			svc.lastReportDay = state.LastReportDay
		}
		// if lastReportDay not set and queue config provided, send initial report
		if svc.lastReportDay.IsZero() {
			if err := svc.maybeSendSpeakReport(messenger, time.Now(), true); err != nil {
				svc.Deps.MustGetLogger().Warn("speak: initial report failed", "error", err)
			}
		}
	}

	return messenger.Subscribe(svc.Deps.MustGetContext(), svc.Cfg.Name, svc.Cfg.Subs["alerts"].Name, svc.Cfg.Type, svc.Cfg.Name, svc.Cfg.Subs["alerts"].MaxAge, svc.messageHandler)
}

func (svc *Service) messageHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	// compute text early so it can be included in any emitted speak events
	var text string
	if aePtr, ok := core.AsType[*core.AlertEvent](msg.Data); ok && aePtr != nil && aePtr.Summary != "" {
		text = aePtr.Summary
	} else if aeVal, ok := core.AsType[core.AlertEvent](msg.Data); ok && aeVal.Summary != "" {
		text = aeVal.Summary
	} else if msg.Summary != "" {
		text = msg.Summary
	} else if msg.Text != "" {
		text = msg.Text
	} else {
		text = ""
	}

	// compute correlation identifier for emitted events
	correlation := ""
	if msg.Correlation != "" {
		correlation = msg.Correlation
	} else if msg.Uuid != "" {
		correlation = msg.Uuid
	}

	// If the message contains a core.AlertEvent with Level == "info", do not speak it.
	// Still emit a speak event indicating it was not spoken due to the info level.
	if isAlertLevelInfo(msg.Data) {
		svc.sendSpeakSkippedEvent(messenger, correlation, text)
		return nil
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
			// emit speak_rate_limit event for the summary including configured limit
			limitDetails := fmt.Sprintf("speak_rate_limit: %d", svc.limiter.Limit())
			e := Event{Now: time.Now(), Summary: summary, Sent: false, Details: limitDetails}
			if sendErr := messenger.Send(core.Message{
				Correlation: correlation,
				ChannelName: svc.Cfg.Name,
				ServiceName: svc.Cfg.Name,
				ServiceType: svc.Cfg.Type,
				Event:       "speak_rate_limit",
				Summary:     summary,
				Text:        summary,
				Data:        e,
			}); sendErr != nil {
				logger.Error("Failed to send speak_rate_limit event", "error", sendErr)
			}
			// also emit unified speak event indicating suppression
			if sendErr := messenger.Send(core.Message{
				Correlation: correlation,
				ChannelName: svc.Cfg.Name,
				ServiceName: svc.Cfg.Name,
				ServiceType: svc.Cfg.Type,
				Event:       "speak",
				Text:        summary,
				Data:        e,
			}); sendErr != nil {
				logger.Error("Failed to send speak event for rate-limited message", "error", sendErr)
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
		// send speak_error event with the error message
		if sendErr := messenger.Send(core.Message{
			Correlation: correlation,
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "speak_error",
			Status:      "error",
			Text:        err.Error(),
		}); sendErr != nil {
			logger.Error("Failed to send speak_error event", "error", sendErr)
		}
		// also emit unified speak event indicating failure
		e := Event{Now: time.Now(), Summary: text, Sent: false, Details: err.Error()}
		if sendErr := messenger.Send(core.Message{
			Correlation: correlation,
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "speak",
			Text:        text,
			Data:        e,
		}); sendErr != nil {
			logger.Error("Failed to send speak event for errored speak", "error", sendErr)
		}
		return err
	}

	// On success, emit a minimal speak event with the spoken text in the Text.
	e := Event{Now: time.Now(), Summary: text, Sent: true}
	if sendErr := messenger.Send(core.Message{
		Correlation: correlation,
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "speak",
		Text:        text,
		Data:        e,
	}); sendErr != nil {
		logger.Error("Failed to send speak event", "error", sendErr)
	}

	return nil
}

// isAlertLevelInfo returns true when data represents an AlertEvent with Level == "info".
func isAlertLevelInfo(data any) bool {
	if ae, ok := core.ExtractAlertEvent(data); ok && ae != nil {
		return ae.Level == "info"
	}
	return false
}

// sendSpeakSkippedEvent emits a speak event indicating the incoming alert was skipped due to info level.
func (svc *Service) sendSpeakSkippedEvent(messenger core.MessengerApi, correlation, text string) {
	e := Event{Now: time.Now(), Summary: text, Sent: false, Details: "skipped: alert level info"}
	if sendErr := messenger.Send(core.Message{
		Correlation: correlation,
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "speak",
		Text:        text,
		Data:        e,
	}); sendErr != nil {
		svc.Deps.MustGetLogger().Error("Failed to send speak event for info-level alert", "error", sendErr)
	}
}

// Check is a no-op for this service, it only reacts to incoming messages from a subscription.
func (svc *Service) Check() error {
	messenger := svc.Deps.MustGetMessenger()
	_ = svc.maybeSendSpeakReport(messenger, time.Now(), false)
	return nil
}
