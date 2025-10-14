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
	svc := &Service{
		Deps: deps,
		Cfg:  cfg,
	}

	minTemp, minTempExists := svc.Cfg.Config["minTemp"]
	if minTempExists {
		svc.MinTemp = minTemp.(float64)
	}

	maxTemp, maxTempExists := svc.Cfg.Config["maxTemp"]
	if maxTempExists {
		svc.MaxTemp = maxTemp.(float64)
	}

	return svc
}

func (svc Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()

	pubErrs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"events", "heater", "cooler"}, logger)
	subErrs := util.ValidateConfig("subs", svc.Cfg.Subs, []string{"temp"}, logger)
	errs := append(pubErrs, subErrs...)

	// check min/max temps
	_, minTempExists := svc.Cfg.Config["minTemp"].(float64)
	if !minTempExists {
		err := fmt.Errorf("thermostat: minTemp not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	_, maxTempExists := svc.Cfg.Config["maxTemp"].(float64)
	if !maxTempExists {
		err := fmt.Errorf("thermostat: maxTemp not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

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

	heaterTargetState := "OFF"
	coolerTargetState := "OFF"

	logger.Info("thermostat: current temp", "temp", msg.Value, "minTemp", svc.MinTemp, "maxTemp", svc.MaxTemp)

	if msg.Value < svc.MinTemp {
		logger.Info("thermostat: temp below min threshold, heating", "temp", msg.Value, "minTemp", svc.MinTemp)
		heaterTargetState = "ON"
	} else if msg.Value > svc.MaxTemp {
		logger.Info("thermostat: temp above max threshold, cooling", "temp", msg.Value, "maxTemp", svc.MaxTemp)
		coolerTargetState = "ON"
	} else {
		logger.Info("thermostat: temp between thresholds, turning off", "temp", msg.Value, "minTemp", svc.MinTemp, "maxTemp", svc.MaxTemp)
	}

	thermostatEvent := Event{
		Temp:              msg.Value,
		MinTemp:           svc.MinTemp,
		MaxTemp:           svc.MaxTemp,
		HeaterTargetState: heaterTargetState,
		CoolerTargetState: coolerTargetState,
	}
	return thermostatEvent
}

func (svc Service) Check() error {
	return nil
}
