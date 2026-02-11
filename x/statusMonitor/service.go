package statusMonitor

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"sync"
	"time"
)

type serviceState struct {
	Status       string    `json:"status"`
	ProblemSince time.Time `json:"problemSince,omitempty"`
	AlertSent    bool      `json:"alertSent,omitempty"`
}

type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig

	states            map[string]serviceState
	statesMutex       sync.RWMutex
	notificationDelay time.Duration
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
				svc.states[k] = st
			}
		}
	}

	// Subscribe to status channel
	statusChan, ok := svc.Cfg.Subs["status"]
	if !ok {
		return fmt.Errorf("status subscription not configured")
	}
	return messenger.Subscribe(svc.Cfg.Name, statusChan.Name, statusChan.MaxAge, svc.messageHandler)
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

	if exists && state.Status == msg.Status && (state.AlertSent || !isProblem(msg.Status)) {
		return nil
	}

	now := time.Now()
	shouldAlert := false
	alertText := ""
	alertStatus := msg.Status

	if isProblem(msg.Status) {
		if !exists || isOk(state.Status) {
			// Newly entered problem state
			state.Status = msg.Status
			state.ProblemSince = now
			state.AlertSent = false
			if svc.notificationDelay == 0 {
				shouldAlert = true
				state.AlertSent = true
				alertText = fmt.Sprintf("ALERT: %s (%s) is in %s state: %s", msg.ServiceName, msg.ServiceType, msg.Status, msg.Text)
			}
		} else if isProblem(state.Status) {
			// Stayed in problem state (possibly changed warning <-> critical)
			oldStatus := state.Status
			state.Status = msg.Status

			if !state.AlertSent {
				if now.Sub(state.ProblemSince) >= svc.notificationDelay {
					logger.Warn("Service %s (%s) has been in %s state for %s, sending alert", msg.ServiceName, msg.ServiceType, msg.Status, svc.notificationDelay)
					shouldAlert = true
					state.AlertSent = true
					alertText = fmt.Sprintf("ALERT: %s (%s) is in %s state (for %s): %s", msg.ServiceName, msg.ServiceType, msg.Status, svc.notificationDelay, msg.Text)
				} else {
					logger.Warn("Service %s (%s) entered %s state, waiting for %s before alerting", msg.ServiceName, msg.ServiceType, msg.Status, svc.notificationDelay)
				}
			} else if oldStatus != msg.Status {
				// Alert already sent, but status changed between problem states
				shouldAlert = true
				alertText = fmt.Sprintf("ALERT: %s (%s) status changed from %s to %s: %s", msg.ServiceName, msg.ServiceType, oldStatus, msg.Status, msg.Text)
			}
		}
	} else if isOk(msg.Status) {
		if exists && isProblem(state.Status) {
			if state.AlertSent {
				shouldAlert = true
				alertStatus = "ok"
				alertText = fmt.Sprintf("RECOVERY: %s (%s) is back to ok state", msg.ServiceName, msg.ServiceType)
			}
		}
		state.Status = msg.Status
		state.ProblemSince = time.Time{}
		state.AlertSent = false
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
			Status:      alertStatus,
			Text:        alertText,
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
