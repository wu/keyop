package statusMonitor

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"sync"
	"time"
)

type serviceState struct {
	Status        string    `json:"status"`
	ProblemSince  time.Time `json:"problemSince,omitempty"`
	AlertSent     bool      `json:"alertSent,omitempty"`
	LastAlertTime time.Time `json:"lastAlertTime,omitempty"`
	AlertCount    int       `json:"alertCount,omitempty"`
}

type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig

	states            map[string]serviceState
	statesMutex       sync.RWMutex
	notificationDelay time.Duration
	initialInterval   time.Duration
	multiplier        float64
	maxInterval       time.Duration
}

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

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("subs", svc.Cfg.Subs, []string{"status"}, logger)
	errs = append(errs, util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"alerts"}, logger)...)
	return errs
}

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
				svc.states[k] = st
			}
		}
	}

	// Subscribe to status channel
	statusChan, ok := svc.Cfg.Subs["status"]
	if !ok {
		return fmt.Errorf("status subscription not configured")
	}
	return messenger.Subscribe(svc.Deps.MustGetContext(), svc.Cfg.Name, statusChan.Name, svc.Cfg.Type, svc.Cfg.Name, statusChan.MaxAge, svc.messageHandler)
}

func (svc *Service) messageHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()

	if msg.Status == "" {
		return nil
	}

	svc.statesMutex.Lock()
	defer svc.statesMutex.Unlock()

	key := fmt.Sprintf("%s:%s", msg.ServiceType, msg.ServiceName)
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
	alertStatus := msg.Status

	if isProblem(msg.Status) {
		if !exists || isOk(state.Status) {
			// Newly entered problem state
			state.Status = msg.Status
			state.ProblemSince = now
			state.AlertSent = false
			state.AlertCount = 0
			if svc.notificationDelay == 0 {
				shouldAlert = true
				state.AlertSent = true
				state.AlertCount = 1
				state.LastAlertTime = now
				alertText = fmt.Sprintf("ALERT: %s (%s) is in %s state: %s", msg.ServiceName, msg.ServiceType, msg.Status, msg.Text)
				alertSummary = fmt.Sprintf("ALERT: %s", alertSummary)
			}
		} else if isProblem(state.Status) {
			// Stayed in problem state (possibly changed warning <-> critical)
			oldStatus := state.Status
			state.Status = msg.Status

			if !state.AlertSent {
				if now.Sub(state.ProblemSince) >= svc.notificationDelay {
					shouldAlert = true
					state.AlertSent = true
					state.AlertCount = 1
					state.LastAlertTime = now
					alertText = fmt.Sprintf("ALERT: %s (%s) is in %s state (for %s): %s", msg.ServiceName, msg.ServiceType, msg.Status, svc.notificationDelay, msg.Text)
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
				alertText = fmt.Sprintf("ALERT: %s (%s) status changed from %s to %s: %s", msg.ServiceName, msg.ServiceType, oldStatus, msg.Status, msg.Text)
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
				alertText = fmt.Sprintf("RECOVERY: %s (%s): %s", msg.ServiceName, msg.ServiceType, msg.Text)
				alertSummary = fmt.Sprintf("RECOVERY: %s", alertSummary)
			}
		}
		state.Status = msg.Status
		state.ProblemSince = time.Time{}
		state.AlertSent = false
		state.AlertCount = 0
		state.LastAlertTime = time.Time{}
	}

	svc.states[key] = state
	svc.saveState()

	if shouldAlert {
		messenger := svc.Deps.MustGetMessenger()
		alertsChanInfo, ok := svc.Cfg.Pubs["alerts"]
		if !ok {
			return fmt.Errorf("alerts publication not configured")
		}
		alertsChan := alertsChanInfo.Name

		return messenger.Send(core.Message{
			ChannelName: alertsChan,
			ServiceName: msg.ServiceName,
			ServiceType: msg.ServiceType,
			MetricName:  msg.MetricName,
			Status:      alertStatus,
			Text:        alertText,
			Summary:     alertSummary,
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

func (svc *Service) Check() error {
	return nil
}
