package thermostat

import (
	"fmt"
	"keyop/core"
	"keyop/util"
)

type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

func (svc Service) ValidateConfig() []error {
	return util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"events", "heater", "cooler"})
}

func (svc Service) Initialize() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	tempChanInfo, exists := svc.Cfg.Subs["temp"]
	if !exists {
		logger.Error("thermostat: No temp channel configured in subs, nothing to do")
		return fmt.Errorf("no temp channel configured in subs")
	}
	err := messenger.Subscribe(svc.Cfg.Name, tempChanInfo.Name, svc.tempHandler)
	if err != nil {
		logger.Error("thermostat: Error subscribing to temp channel", "channel", tempChanInfo.Name, "error", err)
		return err
	}

	return nil
}

type Event struct {
	HeaterTargetState string  `json:"heaterTargetState"`
	CoolerTargetState string  `json:"coolerTargetState"`
	Temp              float64 `json:"temp"`
	MinTemp           float64 `json:"minTemp"`
	MaxTemp           float64 `json:"maxTemp"`
}

func (svc Service) tempHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	// process incoming message
	logger.Info("thermostat received temp message", "message", msg)

	event := svc.updateState(msg, logger)

	logger.Debug("Sending to heater channel", "channel", svc.Cfg.Pubs["heater"].Name)
	//goland:noinspection GoUnhandledErrorResult
	messenger.Send(svc.Cfg.Pubs["heater"].Name, core.Message{
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Text:        fmt.Sprintf("%s target state is %s", svc.Cfg.Name, event.HeaterTargetState),
		State:       event.HeaterTargetState,
	}, event)

	logger.Debug("Sending to cooler channel", "channel", svc.Cfg.Pubs["cooler"].Name)
	//goland:noinspection GoUnhandledErrorResult
	messenger.Send(svc.Cfg.Pubs["cooler"].Name, core.Message{
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Text:        fmt.Sprintf("%s target state is %s", svc.Cfg.Name, event.CoolerTargetState),
		State:       event.CoolerTargetState,
	}, event)

	return nil
}

func (svc Service) updateState(msg core.Message, logger core.Logger) Event {
	//minTem := svc.Cfg.Config["minTemp"].(float64)
	minTemp := 50.0
	maxTemp := 75.0
	heaterTargetState := "OFF"
	coolerTargetState := "OFF"

	if msg.Value < minTemp {
		logger.Info("thermostat: temp below min threshold, turning on heat", "temp", msg.Value, "minTemp", minTemp)
		heaterTargetState = "ON"
	} else if msg.Value > maxTemp {
		logger.Info("thermostat: temp above max threshold, turning off heat", "temp", msg.Value, "maxTemp", maxTemp)
		coolerTargetState = "ON"
	} else {
		logger.Info("thermostat: temp above min threshold, turning off heat", "temp", msg.Value, "minTemp", minTemp)
	}

	thermostatEvent := Event{
		Temp:              msg.Value,
		MinTemp:           minTemp,
		MaxTemp:           maxTemp,
		HeaterTargetState: heaterTargetState,
		CoolerTargetState: coolerTargetState,
	}
	return thermostatEvent
}

func (svc Service) Check() error {
	return nil
}
