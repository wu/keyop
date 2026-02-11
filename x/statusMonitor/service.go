package statusMonitor

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"sync"
)

type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig

	states      map[string]string
	statesMutex sync.RWMutex
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps:   deps,
		Cfg:    cfg,
		states: make(map[string]string),
	}
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
	err := stateStore.Load(svc.Cfg.Name, &svc.states)
	if err != nil {
		logger.Debug("no previous state found or failed to load", "error", err)
		svc.states = make(map[string]string)
	}

	// Subscribe to status channel
	statusChan, ok := svc.Cfg.Subs["status"]
	if !ok {
		return fmt.Errorf("status subscription not configured")
	}
	return messenger.Subscribe(svc.Cfg.Name, statusChan.Name, statusChan.MaxAge, svc.messageHandler)
}

func (svc *Service) messageHandler(msg core.Message) error {
	if msg.Status == "" {
		return nil
	}

	svc.statesMutex.Lock()
	defer svc.statesMutex.Unlock()

	key := fmt.Sprintf("%s:%s", msg.ServiceType, msg.ServiceName)
	oldStatus, exists := svc.states[key]

	if exists && oldStatus == msg.Status {
		return nil
	}

	svc.states[key] = msg.Status
	svc.saveState()

	isProblem := func(s string) bool {
		return s == "warning" || s == "critical"
	}
	isOk := func(s string) bool {
		return s == "ok"
	}

	messenger := svc.Deps.MustGetMessenger()
	alertsChanInfo, ok := svc.Cfg.Pubs["alerts"]
	if !ok {
		// Should have been caught by ValidateConfig, but just in case
		return fmt.Errorf("alerts publication not configured")
	}
	alertsChan := alertsChanInfo.Name

	if (!exists || isOk(oldStatus)) && isProblem(msg.Status) {
		return messenger.Send(core.Message{
			ChannelName: alertsChan,
			ServiceName: msg.ServiceName,
			ServiceType: msg.ServiceType,
			Status:      msg.Status,
			Text:        fmt.Sprintf("ALERT: %s (%s) is in %s state: %s", msg.ServiceName, msg.ServiceType, msg.Status, msg.Text),
		})
	} else if exists && isProblem(oldStatus) && isProblem(msg.Status) {
		return messenger.Send(core.Message{
			ChannelName: alertsChan,
			ServiceName: msg.ServiceName,
			ServiceType: msg.ServiceType,
			Status:      msg.Status,
			Text:        fmt.Sprintf("ALERT: %s (%s) status changed from %s to %s: %s", msg.ServiceName, msg.ServiceType, oldStatus, msg.Status, msg.Text),
		})
	} else if exists && isProblem(oldStatus) && isOk(msg.Status) {
		return messenger.Send(core.Message{
			ChannelName: alertsChan,
			ServiceName: msg.ServiceName,
			ServiceType: msg.ServiceType,
			Status:      "ok",
			Text:        fmt.Sprintf("RECOVERY: %s (%s) is back to ok state", msg.ServiceName, msg.ServiceType),
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
