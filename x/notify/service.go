package notify

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

// Event represents a notification event emitted by the 'notify' service.
// It contains a timestamp, a short summary, whether it was sent, and optional details.
type Event struct {
	Now     time.Time `json:"now"`
	Summary string    `json:"summary"`
	Sent    bool      `json:"sent"`
	Details string    `json:"details,omitempty"`
}

// PayloadType returns the canonical payload type for notify events.
func (e Event) PayloadType() string { return "service.notify.v1" }

// Service converts text payloads into native macOS user notifications.
// It subscribes to the configured 'alerts' channel and emits notification events on success.
// When the rate limit is exceeded, a 'rate_limit' event is emitted with a short summary.
type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig

	// rate limiter
	limiter *util.RateLimiter

	// report queue template and last report day
	queueFileTemplate string
	lastReportDay     time.Time
}

// NewService creates a new 'notify' service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

// RegisterPayloads registers the notification payload types with the provided registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	if err := reg.Register("notify", func() any { return &Event{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register notify alias: %w", err)
		}
	}
	if err := reg.Register("service.notify.v1", func() any { return &Event{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register service.notify.v1: %w", err)
		}
	}
	return nil
}

// Name returns the canonical name of the notify service type.
func (svc *Service) Name() string { return "notify" }

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

// Initialize subscribes to the configured 'alerts' channel and sets up the message handler for incoming messages.
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
			if err := svc.maybeSendNotifyReport(messenger, time.Now(), true); err != nil {
				svc.Deps.MustGetLogger().Warn("notify: initial report failed", "error", err)
			}
		}
	}

	return messenger.Subscribe(svc.Deps.MustGetContext(), svc.Cfg.Name, svc.Cfg.Subs["alerts"].Name, svc.Cfg.Type, svc.Cfg.Name, svc.Cfg.Subs["alerts"].MaxAge, svc.messageHandler)
}

func (svc *Service) messageHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	if msg.Text == "" {
		return nil
	}

	text := msg.Text
	logger.Info("Sending notification", "text", text)
	// osascript -e 'display notification "message" with the title "KeyOp"'
	title := fmt.Sprintf("%s - %s", msg.ServiceName, msg.Hostname)
	text = fmt.Sprintf("[%s] %s", msg.Timestamp.Format("3:04pm"), text)
	logger.Warn("Executing notify command", "title", title, "body", text)

	now := time.Now()
	allowed, firstDrop := svc.limiter.AddEventAt(now)
	if !allowed {
		logger.Warn("Rate limit exceeded", "count", svc.limiter.Total())
		if firstDrop {
			summary := "Too many notifications; some alerts have been skipped."
			// attempt to notify the user about dropped messages using the helper binary
			osProvider := svc.Deps.MustGetOsProvider()
			notifyCmd := "keyop-notify"
			if svc.Cfg.Config != nil {
				if v, ok := svc.Cfg.Config["notify_command"]; ok {
					if s, ok := v.(string); ok && s != "" {
						notifyCmd = s
					}
				}
			}
			// include configured icon if present
			icon := ""
			if svc.Cfg.Config != nil {
				if v, ok := svc.Cfg.Config["notification_icon"]; ok {
					if s, ok := v.(string); ok && s != "" {
						icon = s
					}
				}
			}
			args := []string{"--title", title, "--body", summary}
			if icon != "" {
				args = append(args, "--icon", icon)
			}
			cmd := osProvider.Command(notifyCmd, args...)
			if err := cmd.Run(); err != nil {
				logger.Error("Failed to execute notify helper for rate-limit summary", "error", err)
			}
			// emit notify_rate_limit event for the summary including configured limit
			limitDetails := fmt.Sprintf("notify_rate_limit: %d", svc.limiter.Limit())
			e := Event{Now: time.Now(), Summary: summary, Sent: false, Details: limitDetails}
			if sendErr := messenger.Send(core.Message{
				ChannelName: svc.Cfg.Name,
				ServiceName: svc.Cfg.Name,
				ServiceType: svc.Cfg.Type,
				Event:       "notify_rate_limit",
				Text:        summary,
				Data:        e,
			}); sendErr != nil {
				logger.Error("Failed to send notify_rate_limit event", "error", sendErr)
			}
			// also emit unified notify event indicating suppression
			correlation := ""
			if msg.Correlation != "" {
				correlation = msg.Correlation
			} else if msg.Uuid != "" {
				correlation = msg.Uuid
			}
			if sendErr := messenger.Send(core.Message{
				Correlation: correlation,
				ChannelName: svc.Cfg.Name,
				ServiceName: svc.Cfg.Name,
				ServiceType: svc.Cfg.Type,
				Event:       "notify",
				Text:        text,
				Data:        e,
			}); sendErr != nil {
				logger.Error("Failed to send notify event for rate-limited message", "error", sendErr)
			}
		}
		return nil
	}

	logger.Warn("Executing notify command", "title", title, "body", text)
	osProvider := svc.Deps.MustGetOsProvider()
	// allow overriding the helper command and icon via config
	notifyCmd := "keyop-notify"
	if svc.Cfg.Config != nil {
		if v, ok := svc.Cfg.Config["notify_command"]; ok {
			if s, ok := v.(string); ok && s != "" {
				notifyCmd = s
			}
		}
	}
	icon := ""
	if svc.Cfg.Config != nil {
		if v, ok := svc.Cfg.Config["notification_icon"]; ok {
			if s, ok := v.(string); ok && s != "" {
				icon = s
			}
		}
	}
	args := []string{"--title", title, "--body", text}
	if icon != "" {
		args = append(args, "--icon", icon)
	}

	// Prefer the installed app bundle in /Applications if present; otherwise fall back to plain executable or osascript.
	// Declare cmd and err here so they are available in the branches below.
	var cmd core.CommandApi
	var err error
	appPath := "/Applications/keyop-notify.app"
	if _, statErr := osProvider.Stat(appPath); statErr == nil {
		// run the app bundle using open to ensure it's registered with the system
		// append can't be used directly on CommandApi; build args slice for open
		openArgs := []string{appPath, "--args"}
		openArgs = append(openArgs, args...)
		cmd = osProvider.Command("open", openArgs...)
	} else {
		// try executable name first
		cmd = osProvider.Command(notifyCmd, args...)
		if runErr := cmd.Run(); runErr != nil {
			// final fallback: use osascript to display a basic notification
			script := fmt.Sprintf("display notification \"%s\" with title \"%s\"", strings.ReplaceAll(text, "\"", "\\\""), strings.ReplaceAll(title, "\"", "\\\""))
			logger.Warn("Falling back to osascript", "script", script)
			osaScriptCmd := osProvider.Command("osascript", "-e", script)
			if osaErr := osaScriptCmd.Run(); osaErr != nil {
				logger.Error("Failed to execute osascript fallback", "error", osaErr)
			}
			// continue; the osascript fallback is best-effort and not considered an error
		}
		return nil
	}

	// If we got here, cmd is the 'open' command for the app bundle
	err = cmd.Run()

	correlation := ""
	if msg.Correlation != "" {
		correlation = msg.Correlation
	} else if msg.Uuid != "" {
		correlation = msg.Uuid
	}

	if err != nil {
		logger.Error("Failed to execute osascript command", "error", err)
		// send notify_error event with the error message
		if sendErr := messenger.Send(core.Message{
			Correlation: correlation,
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "notify_error",
			Status:      "error",
			Text:        err.Error(),
		}); sendErr != nil {
			logger.Error("Failed to send notify_error event", "error", sendErr)
		}
		// also emit unified notify event indicating failure
		e := Event{Now: time.Now(), Summary: text, Sent: false, Details: err.Error()}
		if sendErr := messenger.Send(core.Message{
			Correlation: correlation,
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "notify",
			Text:        text,
			Data:        e,
		}); sendErr != nil {
			logger.Error("Failed to send notify event for errored notification", "error", sendErr)
		}
		return err
	}

	// On success, emit a minimal notify event with the notification text in Text.
	e := Event{Now: time.Now(), Summary: text, Sent: true}
	if sendErr := messenger.Send(core.Message{
		Correlation: correlation,
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "notify",
		Text:        text,
		Data:        e,
	}); sendErr != nil {
		logger.Error("Failed to send notify event", "error", sendErr)
	}

	return nil
}

// Check is a no-op for this service, it only reacts to incoming messages from a subscription.
func (svc *Service) Check() error {
	messenger := svc.Deps.MustGetMessenger()
	_ = svc.maybeSendNotifyReport(messenger, time.Now(), false)
	return nil
}
