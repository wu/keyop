package thermostat

import (
	"fmt"
	"keyop/core"
	"keyop/util"
)

type Service struct {
	Deps    core.Dependencies
	Cfg     core.ServiceConfig
	MinTemp float64
	MaxTemp float64
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

func (svc Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	logger.Warn("thermostat: ValidateConfig called")

	pubErrs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"events", "heater", "cooler"}, logger)
	subErrs := util.ValidateConfig("subs", svc.Cfg.Subs, []string{"temp"}, logger)
	errs := append(pubErrs, subErrs...)

	// set default min/max temps if not set in config
	minTemp, minTempExists := svc.Cfg.Config["minTemp"].(float64)
	if !minTempExists {
		err := fmt.Errorf("thermostat: minTemp not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}
	svc.MinTemp = minTemp
	logger.Info("thermostat: minTemp", "minTemp", svc.MinTemp)

	maxTemp, maxTempExists := svc.Cfg.Config["maxTemp"].(float64)
	if !maxTempExists {
		err := fmt.Errorf("thermostat: maxTemp not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}
	svc.MaxTemp = maxTemp
	logger.Info("thermostat: maxTemp", "maxTemp", svc.MaxTemp)

	if svc.MinTemp >= svc.MaxTemp {
		err := fmt.Errorf("thermostat: minTemp must be less than maxTemp (minTemp: %f, maxTemp: %f)", svc.MinTemp, svc.MaxTemp)
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	return errs
}

func (svc Service) Initialize() error {
	messenger := svc.Deps.MustGetMessenger()
	return messenger.Subscribe(svc.Cfg.Name, svc.Cfg.Subs["temp"].Name, svc.tempHandler)
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
