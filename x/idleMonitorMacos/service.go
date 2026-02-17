package idleMonitorMacos

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

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
}

type ServiceState struct {
	IsIdle         bool      `json:"is_idle"`
	LastTransition time.Time `json:"last_transition"`
	LastAlertHours int       `json:"last_alert_hours"`
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"metrics", "alerts", "events"}, logger)

	if _, ok := svc.Cfg.Config["threshold"].(string); !ok {
		logger.Warn("no threshold provided, using default 5m")
	}

	return errs
}

func (svc *Service) Initialize() error {
	logger := svc.Deps.MustGetLogger()
	osProvider := svc.Deps.MustGetOsProvider()

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

	host, err := osProvider.Hostname()
	if err != nil {
		logger.Error("failed to get hostname", "error", err)
		svc.hostname = "unknown"
	} else {
		if idx := strings.Index(host, "."); idx != -1 {
			host = host[:idx]
		}
		svc.hostname = host
	}

	// Attempt to load state
	stateStore := svc.Deps.MustGetStateStore()
	var state ServiceState
	if err := stateStore.Load(svc.Cfg.Name, &state); err == nil {
		svc.isIdle = state.IsIdle
		svc.lastTransition = state.LastTransition
		svc.lastAlertHours = state.LastAlertHours
		logger.Info("loaded state", "isIdle", svc.isIdle, "lastTransition", svc.lastTransition, "lastAlertHours", svc.lastAlertHours)
	}
	if svc.lastTransition.IsZero() {
		svc.lastTransition = time.Now()
		err = stateStore.Save(svc.Cfg.Name, ServiceState{IsIdle: svc.isIdle, LastTransition: svc.lastTransition, LastAlertHours: svc.lastAlertHours})
		if err != nil {
			logger.Error("failed to save state", "error", err)
		}

	}

	return nil
}

func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()
	stateStore := svc.Deps.MustGetStateStore()

	if runtime.GOOS != "darwin" {
		return fmt.Errorf("idleMonitorMacos only supported on macOS")
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
	if svc.isIdle {
		activeDuration = 0
	} else {
		activeDuration = now.Sub(svc.lastTransition)
	}

	// Send event message on every check
	err = messenger.Send(core.Message{
		ChannelName: svc.Cfg.Pubs["events"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Status:      status,
		Text:        fmt.Sprintf("Host %s is %s. Idle: %s, Active: %s, Time since last status change: %s", svc.hostname, status, formatHumanDuration(idleDuration), formatHumanDuration(activeDuration), formatHumanDuration(timeSinceLastStatusChange)),
		Data: map[string]interface{}{
			"idle_duration_seconds":            idleDuration.Seconds(),
			"active_duration_seconds":          activeDuration.Seconds(),
			"is_idle":                          svc.isIdle,
			"time_since_status_change_seconds": timeSinceLastStatusChange.Seconds(),
		},
	})
	if err != nil {
		logger.Error("failed to send event message", "error", err)
	}

	// Send idle duration metric
	err = messenger.Send(core.Message{
		ChannelName: svc.Cfg.Pubs["metrics"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		MetricName:  svc.idleMetricName,
		Metric:      idleDuration.Seconds(),
		Text:        fmt.Sprintf("Status: %s, Time since status change: %s, Idle duration: %s", status, formatHumanDuration(timeSinceLastStatusChange), formatHumanDuration(idleDuration)),
	})
	if err != nil {
		logger.Error("failed to send idle duration metric", "error", err)
	}

	// Send active duration metric
	err = messenger.Send(core.Message{
		ChannelName: svc.Cfg.Pubs["metrics"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
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
		activeTime := now.Sub(svc.lastTransition)
		svc.lastTransition = now.Add(-idleDuration) // backdate to when it actually became idle
		svc.lastAlertHours = 0                      // Reset alert counter
		timeSinceLastStatusChange = now.Sub(svc.lastTransition)

		err = messenger.Send(core.Message{
			ChannelName: svc.Cfg.Pubs["alerts"].Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Status:      "idle",
			Summary:     fmt.Sprintf("Idle on %s", svc.hostname),
			Text:        fmt.Sprintf("Host %s has gone idle. Idle for: %s, previously active for: %s. Time since last status change: %s", svc.hostname, formatHumanDuration(idleDuration), formatHumanDuration(activeTime), formatHumanDuration(timeSinceLastStatusChange)),
		})
		if err != nil {
			logger.Error("failed to send idle alert", "error", err)
		}

		// Save state
		logger.Error("transitioned to idle, saving state", "isIdle", svc.isIdle, "lastTransition", svc.lastTransition, svc.Cfg.Name)
		err = stateStore.Save(svc.Cfg.Name, ServiceState{IsIdle: svc.isIdle, LastTransition: svc.lastTransition, LastAlertHours: svc.lastAlertHours})
		if err != nil {
			logger.Error("failed to save state", "error", err)
		}
	} else if wasIdle && !svc.isIdle {
		// Transitioned to ACTIVE
		idleTime := now.Sub(svc.lastTransition)
		svc.lastTransition = now
		svc.lastAlertHours = 0
		timeSinceLastStatusChange = 0

		err = messenger.Send(core.Message{
			ChannelName: svc.Cfg.Pubs["alerts"].Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Status:      "active",
			Summary:     fmt.Sprintf("Active on %s", svc.hostname),
			Text:        fmt.Sprintf("Host %s is active again. Was idle for: %s. Time since last status change: %s", svc.hostname, formatHumanDuration(idleTime), formatHumanDuration(timeSinceLastStatusChange)),
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
				ChannelName: svc.Cfg.Pubs["alerts"].Name,
				ServiceName: svc.Cfg.Name,
				ServiceType: svc.Cfg.Type,
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
