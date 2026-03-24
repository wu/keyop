// Package statusmon monitors the status field in messages and sends alerts when services
// enter problem states or recover. It implements a backoff strategy for repeated alerts.
package statusmon

import (
	"database/sql"
	"fmt"
	"keyop/core"
	"sync"
	"time"
)

type serviceState struct {
	Status        string    `json:"status"`
	Details       string    `json:"details,omitempty"`
	Level         string    `json:"level,omitempty"`
	LastSeen      time.Time `json:"lastSeen,omitempty"`
	ProblemSince  time.Time `json:"problemSince,omitempty"`
	AlertSent     bool      `json:"alertSent,omitempty"`
	LastAlertTime time.Time `json:"lastAlertTime,omitempty"`
	AlertCount    int       `json:"alertCount,omitempty"`
	Acknowledged  bool      `json:"acknowledged,omitempty"`
	Removed       bool      `json:"removed,omitempty"`
}

// Service collects per-service status events and computes an aggregate health score for dashboards and alerts.
type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig

	states            map[string]serviceState
	statesMutex       sync.RWMutex
	notificationDelay time.Duration
	initialInterval   time.Duration
	multiplier        float64
	maxInterval       time.Duration
	db                **sql.DB
	dbPath            string
}

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps:   deps,
		Cfg:    cfg,
		states: make(map[string]serviceState),
	}

	if delayStr, ok := cfg.Config["notificationDelay"].(string); ok {
		if d, err := time.ParseDuration(delayStr); err == nil {
			svc.notificationDelay = d
		}
	}

	svc.initialInterval = time.Hour
	if intervalStr, ok := cfg.Config["initialInterval"].(string); ok {
		if d, err := time.ParseDuration(intervalStr); err == nil {
			svc.initialInterval = d
		}
	}

	svc.multiplier = 2.0
	if m, ok := cfg.Config["multiplier"].(float64); ok {
		svc.multiplier = m
	} else if m, ok := cfg.Config["multiplier"].(int); ok {
		svc.multiplier = float64(m)
	}

	svc.maxInterval = 24 * time.Hour
	if maxStr, ok := cfg.Config["maxInterval"].(string); ok {
		if d, err := time.ParseDuration(maxStr); err == nil {
			svc.maxInterval = d
		}
	}

	return svc
}

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	var errs []error
	if len(svc.Cfg.Subs) == 0 {
		errs = append(errs, fmt.Errorf("statusMonitor service requires at least one subscription in 'subs'"))
	}
	return errs
}

// PayloadTypes returns the list of payload types that this service can handle.
func (svc *Service) PayloadTypes() []string {
	return []string{"core.status.v1"}
}

// Initialize performs one-time startup required by the service (resource loading or connectivity checks).
func (svc *Service) Initialize() error {
	logger := svc.Deps.MustGetLogger()
	stateStore := svc.Deps.MustGetStateStore()
	messenger := svc.Deps.MustGetMessenger()

	// Load persisted state
	var rawStates map[string]interface{}
	err := stateStore.Load(svc.Cfg.Name, &rawStates)
	if err != nil {
		logger.Debug("no previous state found or failed to load", "error", err)
		svc.states = make(map[string]serviceState)
	} else {
		svc.states = make(map[string]serviceState)
		for k, v := range rawStates {
			if s, ok := v.(string); ok {
				// Old format: map[string]string
				svc.states[k] = serviceState{Status: s}
			} else if m, ok := v.(map[string]interface{}); ok {
				// New format: map[string]serviceState
				st := serviceState{}
				if status, ok := m["status"].(string); ok {
					st.Status = status
				}
				if ps, ok := m["problemSince"].(string); ok {
					if t, err := time.Parse(time.RFC3339, ps); err == nil {
						st.ProblemSince = t
					}
				}
				if as, ok := m["alertSent"].(bool); ok {
					st.AlertSent = as
				}
				if lat, ok := m["lastAlertTime"].(string); ok {
					if t, err := time.Parse(time.RFC3339, lat); err == nil {
						st.LastAlertTime = t
					}
				}
				if ac, ok := m["alertCount"].(float64); ok {
					st.AlertCount = int(ac)
				}
				if ack, ok := m["acknowledged"].(bool); ok {
					st.Acknowledged = ack
				}
				if removed, ok := m["removed"].(bool); ok {
					st.Removed = removed
				}
				svc.states[k] = st
			}
		}
	}

	// Subscribe to all channels listed in the 'subs' section
	ctx := svc.Deps.MustGetContext()
	for _, subInfo := range svc.Cfg.Subs {
		if subInfo.Name == "" {
			return fmt.Errorf("statusMonitor: subscription entry missing 'Name'")
		}
		if err := messenger.Subscribe(ctx, svc.Cfg.Name, subInfo.Name, svc.Cfg.Type, svc.Cfg.Name, subInfo.MaxAge, svc.messageHandler); err != nil {
			return fmt.Errorf("failed to subscribe to %s: %w", subInfo.Name, err)
		}
	}
	return nil
}

func statusKey(hostname, name string) string {
	if hostname != "" {
		return hostname + ":" + name
	}
	return name
}

func (svc *Service) handleStatusEvent(statusEvent *core.StatusEvent) error {
	if statusEvent.Name == "" {
		return nil
	}

	key := statusKey(statusEvent.Hostname, statusEvent.Name)

	svc.statesMutex.Lock()
	defer svc.statesMutex.Unlock()

	state := svc.states[key]
	state.Status = statusEvent.Status
	state.Details = statusEvent.Details
	state.Level = statusEvent.Level
	state.LastSeen = time.Now()
	state.Removed = false // re-show if a new event arrives after removal
	svc.states[key] = state

	// Persist the updated state
	svc.saveState()

	return nil
}

func (svc *Service) messageHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()

	// Handle StatusEvent data
	if msg.Data != nil {
		if statusEvent, ok := msg.Data.(*core.StatusEvent); ok {
			return svc.handleStatusEvent(statusEvent)
		}
	}

	if msg.Status == "" {
		return nil
	}

	// For legacy status messages (Status field set), create a StatusEvent
	// and forward it so sqlite can capture it
	name := fmt.Sprintf("%s:%s", msg.ServiceType, msg.ServiceName)
	statusEvent := &core.StatusEvent{
		Name:     name,
		Hostname: msg.Hostname,
		Status:   msg.Status,
		Details:  msg.Text,
		Level:    msg.Status, // Derive level from status
	}

	// Send the status event back so sqlite can capture it
	messenger := svc.Deps.MustGetMessenger()
	channelName := msg.ChannelName
	if channelName == "" {
		channelName = "status"
	}
	forwardMsg := core.Message{
		Timestamp:   msg.Timestamp,
		Uuid:        msg.Uuid,
		Correlation: msg.Correlation,
		Hostname:    msg.Hostname,
		ChannelName: channelName,
		ServiceType: msg.ServiceType,
		ServiceName: msg.ServiceName,
		Event:       msg.Event,
		Status:      msg.Status,
		Text:        msg.Text,
		Data:        statusEvent,
	}
	if err := messenger.Send(forwardMsg); err != nil {
		logger.Warn("failed to forward status event", "error", err, "name", name)
	}

	// Also handle it locally for alerts
	svc.statesMutex.Lock()
	defer svc.statesMutex.Unlock()

	key := statusKey(msg.Hostname, name)
	state, exists := svc.states[key]

	isProblem := func(s string) bool {
		return s == "warning" || s == "critical"
	}
	isOk := func(s string) bool {
		return s == "ok"
	}

	if exists && state.Status == msg.Status && !isProblem(msg.Status) {
		return nil
	}

	now := time.Now()
	shouldAlert := false
	alertText := ""
	alertSummary := msg.Summary
	if alertSummary == "" {
		alertSummary = fmt.Sprintf("%s is %s", msg.ServiceName, msg.Status)
	}
	alertStatus := msg.Status

	if isProblem(msg.Status) {
		if !exists || isOk(state.Status) {
			// Newly entered problem state
			state.Status = msg.Status
			state.ProblemSince = now
			state.AlertSent = false
			state.AlertCount = 0
			state.Acknowledged = false
			if svc.notificationDelay == 0 {
				shouldAlert = true
				state.AlertSent = true
				state.AlertCount = 1
				state.LastAlertTime = now
				alertText = fmt.Sprintf("ALERT: %s is in %s state: %s", msg.ServiceName, msg.Status, msg.Text)
				alertSummary = fmt.Sprintf("ALERT: %s", alertSummary)
			}
		} else if isProblem(state.Status) {
			// Stayed in problem state (possibly changed warning <-> critical)
			oldStatus := state.Status
			state.Status = msg.Status

			// Clear acknowledgement if severity increased (warning -> critical)
			if oldStatus == "warning" && msg.Status == "critical" {
				state.Acknowledged = false
			}

			if !state.AlertSent {
				if now.Sub(state.ProblemSince) >= svc.notificationDelay {
					shouldAlert = true
					state.AlertSent = true
					state.AlertCount = 1
					state.LastAlertTime = now
					alertText = fmt.Sprintf("ALERT: %s is in %s state (for %s): %s", msg.ServiceName, msg.Status, svc.notificationDelay, msg.Text)
					alertSummary = fmt.Sprintf("ALERT: %s", alertSummary)
				} else {
					timeRemaining := svc.notificationDelay - now.Sub(state.ProblemSince)
					logger.Warn("Service in problem state, waiting before alerting",
						"serviceName", msg.ServiceName,
						"serviceType", msg.ServiceType,
						"status", msg.Status,
						"notificationDelay", svc.notificationDelay,
						"timeRemaining", timeRemaining)
				}
			} else if oldStatus != msg.Status {
				// Alert already sent, but status changed between problem states
				shouldAlert = true
				state.AlertCount = 1
				state.LastAlertTime = now
				alertText = fmt.Sprintf("ALERT: %s status changed from %s to %s: %s", msg.ServiceName, oldStatus, msg.Status, msg.Text)
				alertSummary = fmt.Sprintf("ALERT: %s", alertSummary)
			} else {
				// Stayed in the same problem state, check backoff
				multiplier := 1.0
				for i := 0; i < state.AlertCount-1; i++ {
					multiplier *= svc.multiplier
				}

				interval := time.Duration(float64(svc.initialInterval) * multiplier)
				if interval > svc.maxInterval {
					interval = svc.maxInterval
				}

				if now.Sub(state.LastAlertTime) >= interval {
					shouldAlert = true
					state.AlertCount++
					state.LastAlertTime = now
					alertText = fmt.Sprintf("ALERT: %s: %s", msg.Status, msg.Text)
					alertSummary = fmt.Sprintf("ALERT: %s", alertSummary)
				}
			}
		}
	} else if isOk(msg.Status) {
		if exists && isProblem(state.Status) {
			if state.AlertSent {
				shouldAlert = true
				alertStatus = "ok"
				alertText = fmt.Sprintf("RECOVERY: %s: %s", msg.ServiceName, msg.Text)
				alertSummary = fmt.Sprintf("RECOVERY: %s", alertSummary)
			}
		}
		state.Status = msg.Status
		state.ProblemSince = time.Time{}
		state.AlertSent = false
		state.AlertCount = 0
		state.LastAlertTime = time.Time{}
		state.Acknowledged = false
	}

	svc.states[key] = state
	svc.saveState()

	if shouldAlert {
		messenger := svc.Deps.MustGetMessenger()
		return messenger.Send(core.Message{
			Correlation: msg.Uuid,
			ChannelName: svc.Cfg.Name,
			ServiceName: msg.ServiceName,
			ServiceType: msg.ServiceType,
			Event:       "status_alert",
			MetricName:  msg.MetricName,
			Status:      alertStatus,
			Text:        alertText,
			Summary:     alertSummary,
			Data: core.AlertEvent{
				Summary: alertSummary,
				Text:    alertText,
				Level:   alertStatus,
			},
		})
	}

	return nil
}

func (svc *Service) saveState() {
	logger := svc.Deps.MustGetLogger()
	stateStore := svc.Deps.MustGetStateStore()
	err := stateStore.Save(svc.Cfg.Name, svc.states)
	if err != nil {
		logger.Error("failed to save state", "error", err)
	}
}

// Check performs the service's periodic work: collect data, evaluate state, and publish messages/metrics.
func (svc *Service) Check() error {
	return nil
}
