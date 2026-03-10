// Package idle monitors macOS user idle/active state and emits events used for presence and automation.
package idle

import (
	"database/sql"
	"fmt"
	"keyop/core"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// localMidnight returns the start of the calendar day for t in t's location.
func localMidnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// ActivePeriod represents a single active period within a day file.
type ActivePeriod struct {
	Hostname        string    `yaml:"hostname" json:"hostname"`
	Start           time.Time `yaml:"start" json:"start"`
	Stop            time.Time `yaml:"stop" json:"stop"`
	DurationSeconds float64   `yaml:"durationSeconds" json:"durationSeconds"`
}

// Event represents a typed payload for idle events.
type Event struct {
	Now                          time.Time `json:"now"`
	Hostname                     string    `json:"hostname"`
	Status                       string    `json:"status"`
	IdleDurationSeconds          float64   `json:"idleDurationSeconds"`
	ActiveDurationSeconds        float64   `json:"activeDurationSeconds"`
	IsIdle                       bool      `json:"isIdle"`
	TimeSinceStatusChangeSeconds float64   `json:"timeSinceStatusChangeSeconds"`
}

// PayloadType returns the canonical payload type for idle events.
func (e Event) PayloadType() string { return "service.idle.v1" }

// Service monitors the macOS idle APIs to detect user activity and publishes idle/active events.
type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig

	isIdle           bool
	lastTransition   time.Time
	lastAlertHours   int
	threshold        time.Duration
	hostname         string
	idleMetricName   string
	activeMetricName string

	db            **sql.DB
	lastReportDay time.Time
}

// ServiceState holds persistent runtime state for the idle service (for example, last idle timestamp and alerting state).
type ServiceState struct {
	IsIdle         bool      `json:"is_idle"`
	LastTransition time.Time `json:"last_transition"`
	LastAlertHours int       `json:"last_alert_hours"`
	LastReportDay  time.Time `json:"last_report_day"`
}

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

// RegisterPayloads registers the idle payload types with the provided registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	if err := reg.Register("idle", func() any { return &Event{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register idle alias: %w", err)
		}
	}
	if err := reg.Register("service.idle.v1", func() any { return &Event{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register service.idle.v1: %w", err)
		}
	}
	return nil
}

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()

	if _, ok := svc.Cfg.Config["threshold"].(string); !ok {
		logger.Warn("no threshold provided, using default 5m")
	}

	return nil
}

// Initialize performs one-time startup required by the service (resource loading or connectivity checks).
func (svc *Service) Initialize() error {
	logger := svc.Deps.MustGetLogger()
	stateStore := svc.Deps.MustGetStateStore()

	thresholdStr, _ := svc.Cfg.Config["threshold"].(string)
	if thresholdStr == "" {
		svc.threshold = 5 * time.Minute
	} else {
		var err error
		svc.threshold, err = time.ParseDuration(thresholdStr)
		if err != nil {
			logger.Error("failed to parse threshold, using default 5m", "error", err)
			svc.threshold = 5 * time.Minute
		}
	}

	svc.idleMetricName, _ = svc.Cfg.Config["idle_metric_name"].(string)
	if svc.idleMetricName == "" {
		svc.idleMetricName = fmt.Sprintf("%s.idle_duration", svc.Cfg.Name)
	}
	svc.activeMetricName, _ = svc.Cfg.Config["active_metric_name"].(string)
	if svc.activeMetricName == "" {
		svc.activeMetricName = fmt.Sprintf("%s.active_duration", svc.Cfg.Name)
	}

	// Attempt to load state (including last report day)
	var state ServiceState
	if err := stateStore.Load(svc.Cfg.Name, &state); err == nil {
		svc.isIdle = state.IsIdle
		svc.lastTransition = state.LastTransition
		svc.lastAlertHours = state.LastAlertHours
		svc.lastReportDay = state.LastReportDay
		logger.Info("loaded state", "isIdle", svc.isIdle, "lastTransition", svc.lastTransition, "lastAlertHours", svc.lastAlertHours, "lastReportDay", svc.lastReportDay)
	}
	if svc.lastTransition.IsZero() {
		svc.lastTransition = time.Now()
		_ = stateStore.Save(svc.Cfg.Name, ServiceState{IsIdle: svc.isIdle, LastTransition: svc.lastTransition, LastAlertHours: svc.lastAlertHours, LastReportDay: svc.lastReportDay})
	}

	// If lastReportDay not set, generate report for previous day immediately.
	if svc.lastReportDay.IsZero() {
		messenger := svc.Deps.MustGetMessenger()
		if _, err := svc.maybeSendIdleReport(messenger, time.Now(), time.Time{}, time.Time{}, true); err != nil {
			logger.Warn("idle: initial report failed", "error", err)
		}
	}

	return nil
}

// Check performs the periodic work for the idle service: sample idle state and emit events.
func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()
	stateStore := svc.Deps.MustGetStateStore()

	if runtime.GOOS != "darwin" {
		return fmt.Errorf("idle only supported on macOS")
	}

	idleDuration, err := svc.getMacosIdleTime()
	if err != nil {
		logger.Error("failed to get macos idle time", "error", err)
		return err
	}

	now := time.Now()
	wasIdle := svc.isIdle
	svc.isIdle = idleDuration >= svc.threshold

	status := "active"
	if svc.isIdle {
		status = "idle"
	}

	timeSinceLastStatusChange := now.Sub(svc.lastTransition)

	// Metrics
	var activeDuration time.Duration
	if wasIdle && !svc.isIdle {
		// Transitioned to ACTIVE - update lastTransition BEFORE calculation
		svc.lastTransition = now
		timeSinceLastStatusChange = 0
		logger.Info("transitioned to active", "isIdle", svc.isIdle, "lastTransition", svc.lastTransition, "service", svc.Cfg.Name)
	}

	if svc.isIdle {
		activeDuration = 0
	} else {
		activeDuration = now.Sub(svc.lastTransition)
	}

	// Send event message on every check
	err = messenger.Send(core.Message{
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "idle_status",
		Status:      status,
		Text:        fmt.Sprintf("Host %s is %s. Idle: %s, Active: %s", svc.hostname, status, formatHumanDuration(idleDuration), formatHumanDuration(activeDuration)),
		Data: &Event{
			Now:                          now,
			Hostname:                     svc.hostname,
			Status:                       status,
			IdleDurationSeconds:          idleDuration.Seconds(),
			ActiveDurationSeconds:        activeDuration.Seconds(),
			IsIdle:                       svc.isIdle,
			TimeSinceStatusChangeSeconds: timeSinceLastStatusChange.Seconds(),
		},
	})
	if err != nil {
		logger.Error("failed to send event message", "error", err)
	}

	// Send idle duration metric
	err = messenger.Send(core.Message{
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "idle_duration_metric",
		MetricName:  svc.idleMetricName,
		Metric:      idleDuration.Seconds(),
		Text:        fmt.Sprintf("Status: %s, Time since status change: %s, Idle duration: %s", status, formatHumanDuration(timeSinceLastStatusChange), formatHumanDuration(idleDuration)),
	})
	if err != nil {
		logger.Error("failed to send idle duration metric", "error", err)
	}

	// Send active duration metric
	err = messenger.Send(core.Message{
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "active_duration_metric",
		MetricName:  svc.activeMetricName,
		Metric:      activeDuration.Seconds(),
		Text:        fmt.Sprintf("Status: %s, Time since status change: %s, Active duration: %s", status, formatHumanDuration(timeSinceLastStatusChange), formatHumanDuration(activeDuration)),
	})
	if err != nil {
		logger.Error("failed to send active duration metric", "error", err)
	}

	// State transitions and alerts
	if !wasIdle && svc.isIdle {
		// Transitioned to IDLE
		prevTransition := svc.lastTransition
		idleStart := now.Add(-idleDuration)
		activeTime := idleStart.Sub(prevTransition)
		svc.lastTransition = idleStart // backdate to when it actually became idle
		svc.lastAlertHours = 0         // Reset alert counter

		err = messenger.Send(core.Message{
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "idle_alert",
			Status:      "idle",
			Summary:     fmt.Sprintf("Idle on %s", svc.hostname),
			Text:        fmt.Sprintf("Host %s has gone idle. Idle for: %s, previously active for: %s", svc.hostname, formatHumanDuration(idleDuration), formatHumanDuration(activeTime)),
		})
		if err != nil {
			logger.Error("failed to send idle alert", "error", err)
		}

		// Save state
		logger.Info("transitioned to idle, saving state", "isIdle", svc.isIdle, "lastTransition", svc.lastTransition, "service", svc.Cfg.Name)
		_ = stateStore.Save(svc.Cfg.Name, ServiceState{IsIdle: svc.isIdle, LastTransition: svc.lastTransition, LastAlertHours: svc.lastAlertHours, LastReportDay: svc.lastReportDay})

		// No-op: active periods are now derived from events queue files instead of local YAML files.
	} else if wasIdle && !svc.isIdle {
		// Transitioned to ACTIVE (lastTransition already updated above)
		svc.lastAlertHours = 0

		err = messenger.Send(core.Message{
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "active_alert",
			Status:      "active",
			Summary:     fmt.Sprintf("Active on %s", svc.hostname),
			Text:        fmt.Sprintf("Host %s is active again.", svc.hostname),
		})
		if err != nil {
			logger.Error("failed to send active alert", "error", err)
		}

		// Save state
		err = stateStore.Save(svc.Cfg.Name, ServiceState{IsIdle: svc.isIdle, LastTransition: svc.lastTransition, LastAlertHours: svc.lastAlertHours})
		if err != nil {
			logger.Error("failed to save state", "error", err)
		}
	} else if !svc.isIdle {
		// Stayed ACTIVE - check for hour-by-hour alert
		activeHours := int(activeDuration.Hours())
		if activeHours > svc.lastAlertHours {
			svc.lastAlertHours = activeHours
			err = messenger.Send(core.Message{
				ChannelName: svc.Cfg.Name,
				ServiceName: svc.Cfg.Name,
				ServiceType: svc.Cfg.Type,
				Event:       "active_reminder",
				Status:      "active_reminder",
				Summary:     fmt.Sprintf("break time for %s", svc.hostname),
				Text:        fmt.Sprintf("Active on %s for %s. Consider taking a break!", svc.hostname, formatHumanDuration(activeDuration)),
			})
			if err != nil {
				logger.Error("failed to send active reminder alert", "error", err)
			}

			// Save state
			err = stateStore.Save(svc.Cfg.Name, ServiceState{IsIdle: svc.isIdle, LastTransition: svc.lastTransition, LastAlertHours: svc.lastAlertHours})
			if err != nil {
				logger.Error("failed to save state", "error", err)
			}
		}
	}

	// Attempt to send nightly report between 00:00 and 01:00 local time
	if _, err := svc.maybeSendIdleReport(messenger, time.Now(), time.Time{}, time.Time{}, false); err != nil {
		logger.Warn("idle: failed to send nightly report", "error", err)
	}

	return nil
}

func formatHumanDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d == 0 {
		return "0s"
	}

	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
		if hours > 0 {
			parts = append(parts, fmt.Sprintf("%dh", hours))
		}
	} else if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
		if minutes > 0 {
			parts = append(parts, fmt.Sprintf("%dm", minutes))
		}
	} else if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
		if seconds > 0 {
			parts = append(parts, fmt.Sprintf("%ds", seconds))
		}
	} else if seconds > 0 {
		parts = append(parts, fmt.Sprintf("%ds", seconds))
	}

	if len(parts) == 0 {
		return "0s"
	}

	if len(parts) > 2 {
		parts = parts[:2]
	}

	return strings.Join(parts, " ")
}

func (svc *Service) getMacosIdleTime() (time.Duration, error) {
	osProvider := svc.Deps.MustGetOsProvider()
	cmd := osProvider.Command("ioreg", "-c", "IOHIDSystem")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, err
	}

	re := regexp.MustCompile(`"HIDIdleTime"\s*=\s*(\d+)`)
	matches := re.FindStringSubmatch(string(output))
	if len(matches) < 2 {
		return 0, fmt.Errorf("could not find HIDIdleTime in ioreg output")
	}

	nanos, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return 0, err
	}

	return time.Duration(nanos) * time.Nanosecond, nil
}
