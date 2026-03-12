package txtmsg

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Service implements the txtmsg service for sending text messages via macOS Messages.
type Service struct {
	Deps    core.Dependencies
	Cfg     core.ServiceConfig
	Address string

	// rate limiter
	limiter *util.RateLimiter

	// latest idle status
	mu               sync.Mutex
	latestIdleStatus string
	latestIdleAt     time.Time

	// report queue template and last report day
	queueFileTemplate string
	lastReportDay     time.Time
}

// Event represents a text event emitted by the 'messages' service.
// It includes whether the text was sent and additional details when available.
type Event struct {
	Now     time.Time `json:"now"`
	Summary string    `json:"summary"`
	Sent    bool      `json:"sent"`
	Details string    `json:"details,omitempty"`
}

// PayloadType returns the canonical payload type for text events.
func (e Event) PayloadType() string { return "service.txtmsg.v1" }

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

// RegisterPayloads registers the messages payload types with the provided registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	if err := reg.Register("txtmsg", func() any { return &Event{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register txtmsg alias: %w", err)
		}
	}
	if err := reg.Register("service.txtmsg.v1", func() any { return &Event{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register service.txtmsg.v1: %w", err)
		}
	}
	return nil
}

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	// require both alerts (incoming messages) and idle (status updates)
	errs := util.ValidateConfig("subs", svc.Cfg.Subs, []string{"alerts"}, logger)

	address, _ := svc.Cfg.Config["address"].(string)
	if address == "" {
		err := fmt.Errorf("messages address is required in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	return errs
}

// Initialize performs the one-time startup required by the service (resource loading or connectivity checks).
func (svc *Service) Initialize() error {
	// create rate limiter from config
	limit := 10
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
	svc.Address = svc.Cfg.Config["address"].(string)
	// subscribe to alerts (incoming messages)
	if err := messenger.Subscribe(svc.Deps.MustGetContext(), svc.Cfg.Name, svc.Cfg.Subs["alerts"].Name, svc.Cfg.Type, svc.Cfg.Name, svc.Cfg.Subs["alerts"].MaxAge, svc.messageHandler); err != nil {
		return err
	}
	// optionally subscribe to idle status updates if configured
	if idleInfo, ok := svc.Cfg.Subs["idle"]; ok && idleInfo.Name != "" {
		if err := messenger.Subscribe(svc.Deps.MustGetContext(), svc.Cfg.Name+"-idle", idleInfo.Name, svc.Cfg.Type, svc.Cfg.Name, idleInfo.MaxAge, svc.idleStatusHandler); err != nil {
			return err
		}
	}

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
			if err := svc.maybeSendTxtmsgReport(messenger, time.Now(), true); err != nil {
				svc.Deps.MustGetLogger().Warn("txtmsg: initial report failed", "error", err)
			}
		}
	}

	return nil
}

func (svc *Service) messageHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	// Prefer AlertEvent.Summary, then Message Summary/Text
	var rawText string
	if ae, ok := core.ExtractAlertEvent(msg.Data); ok && ae != nil && ae.Summary != "" {
		rawText = ae.Summary
	} else if msg.Summary != "" {
		rawText = msg.Summary
	} else if msg.Text != "" {
		rawText = msg.Text
	}
	if rawText == "" {
		return nil
	}

	logger.Info("Sending message", "text", rawText)

	content := fmt.Sprintf("%s-%s: %s", msg.ServiceName, msg.ServiceType, rawText)
	script := fmt.Sprintf(`tell application "Messages" to send "keyop: %s" to buddy "%s"`, content, svc.Address)
	logger.Warn("Executing osascript command", "script", script)
	osProvider := svc.Deps.MustGetOsProvider()

	// prepare correlation for emitted events
	correlation := ""
	if msg.Correlation != "" {
		correlation = msg.Correlation
	} else if msg.Uuid != "" {
		correlation = msg.Uuid
	}

	// Check latest idle status; if active, suppress
	svc.mu.Lock()
	latestStatus := svc.latestIdleStatus
	svc.mu.Unlock()
	if latestStatus == "active" {
		details := "suppressed: host reported active"
		// emit unified text event indicating suppression
		e := Event{Now: time.Now(), Summary: rawText, Sent: false, Details: details}
		if sendErr := messenger.Send(core.Message{
			Correlation: correlation,
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "txtmsg",
			Text:        rawText,
			Data:        e,
		}); sendErr != nil {
			logger.Error("Failed to send txtmsg event for suppressed message", "error", sendErr)
		}
		return nil
	}

	// rate limiting
	now := time.Now()
	allowed, firstDrop := svc.limiter.AddEventAt(now)
	if !allowed {
		logger.Warn("Rate limit exceeded", "count", svc.limiter.Total())
		if firstDrop {
			summary := "Too many text messages; some messages have been skipped."
			// emit txtmsg_rate_limit event including configured limit
			details := fmt.Sprintf("txtmsg_rate_limit: %d", svc.limiter.Limit())
			e := Event{Now: time.Now(), Summary: summary, Sent: false, Details: details}
			if sendErr := messenger.Send(core.Message{
				ChannelName: svc.Cfg.Name,
				ServiceName: svc.Cfg.Name,
				ServiceType: svc.Cfg.Type,
				Event:       "txtmsg_rate_limit",
				Text:        summary,
				Data:        e,
			}); sendErr != nil {
				logger.Error("Failed to send txtmsg_rate_limit event", "error", sendErr)
			}
		}
		// emit unified text event indicating suppression
		e := Event{Now: time.Now(), Summary: rawText, Sent: false, Details: fmt.Sprintf("txtmsg_rate_limit: %d", svc.limiter.Limit())}
		if sendErr := messenger.Send(core.Message{
			Correlation: correlation,
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "txtmsg",
			Text:        rawText,
			Data:        e,
		}); sendErr != nil {
			logger.Error("Failed to send text event for rate-limited message", "error", sendErr)
		}
		return nil
	}

	cmd := osProvider.Command("osascript", "-e", script)

	err := cmd.Run()
	if err != nil {
		logger.Error("Failed to execute osascript command", "error", err)
		// send txtmsg_error event with the failure reason
		if sendErr := messenger.Send(core.Message{
			Correlation: correlation,
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "txtmsg_error",
			Status:      "error",
			Text:        err.Error(),
		}); sendErr != nil {
			logger.Error("Failed to send txtmsg_error event", "error", sendErr)
		}
		// emit unified text event with error
		e := Event{Now: time.Now(), Summary: rawText, Sent: false, Details: err.Error()}
		if sendErr := messenger.Send(core.Message{
			Correlation: correlation,
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "txtmsg",
			Text:        rawText,
			Data:        e,
		}); sendErr != nil {
			logger.Error("Failed to send text event for errored message", "error", sendErr)
		}
		return err
	}

	// On success emit unified text event
	e := Event{Now: time.Now(), Summary: rawText, Sent: true}
	if sendErr := messenger.Send(core.Message{
		Correlation: correlation,
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "txtmsg",
		Text:        rawText,
		Data:        e,
	}); sendErr != nil {
		logger.Error("Failed to send text event for sent message", "error", sendErr)
	}

	return nil
}

// idleStatusHandler consumes idle_status messages from the idle channel to maintain the latest status.
func (svc *Service) idleStatusHandler(msg core.Message) error {
	if msg.Event != "idle_status" {
		return nil
	}
	status := msg.Status
	if status == "" {
		return nil
	}
	svc.mu.Lock()
	svc.latestIdleStatus = status
	if !msg.Timestamp.IsZero() {
		svc.latestIdleAt = msg.Timestamp
	} else {
		svc.latestIdleAt = time.Now()
	}
	svc.mu.Unlock()
	return nil
}

// Check is a no-op for this service, it only reacts to incoming messages from a subscription.
func (svc *Service) Check() error {
	// attempt nightly report between 00:00-01:00
	messenger := svc.Deps.MustGetMessenger()
	_ = svc.maybeSendTxtmsgReport(messenger, time.Now(), false)
	return nil
}
