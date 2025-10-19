package thermostat

import (
	"fmt"
	"keyop/core"
	"keyop/util"
)

type Service struct {
	Deps        core.Dependencies
	Cfg         core.ServiceConfig
	MinTemp     float64
	MaxTemp     float64
	Mode        string // "heat", "cool", "auto", "off"
	Hysteresis  float64
	HeaterState string
	CoolerState string
}

var validModes = map[string]bool{
	"heat": true,
	"cool": true,
	"auto": true,
	"off":  true,
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

	mode, modeExists := svc.Cfg.Config["mode"]
	if modeExists {
		svc.Mode = mode.(string)
	}

	hysteresis, hysteresisExists := svc.Cfg.Config["hysteresis"]
	if hysteresisExists {
		svc.Hysteresis = hysteresis.(float64)
	}

	return svc
}

func getValidModes() []string {
	modes := make([]string, 0, len(validModes))
	for mode := range validModes {
		modes = append(modes, mode)
	}
	return modes
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

	if svc.MinTemp > svc.MaxTemp {
		err := fmt.Errorf("thermostat: minTemp must be less than or equal to maxTemp (minTemp: %f, maxTemp: %f)", svc.MinTemp, svc.MaxTemp)
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	// check mode
	mode, modeExists := svc.Cfg.Config["mode"].(string)
	if !modeExists {
		err := fmt.Errorf("thermostat: mode not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	} else {
		if _, valid := validModes[mode]; !valid {
			err := fmt.Errorf("thermostat: invalid mode '%s' in config, must be one of %v", mode, getValidModes())
			logger.Error(err.Error())
			errs = append(errs, err)
		}
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
	Mode              string  `json:"mode"`
	Hysteresis        float64 `json:"hysteresis,omitempty"`
}

func (svc Service) tempHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	// process incoming message
	logger.Info("thermostat received temp message", "message", msg)

	event := svc.updateState(msg, logger)
	logger.Info("thermostat state updated", "event", event)
	svc.HeaterState = event.HeaterTargetState
	svc.CoolerState = event.CoolerTargetState

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

	heaterTargetState := "OFF"
	coolerTargetState := "OFF"

	logger.Info("thermostat: current temp", "temp", msg.Metric, "minTemp", svc.MinTemp, "maxTemp", svc.MaxTemp, "mode", svc.Mode, "hysteresis", svc.Hysteresis, "heaterState", svc.HeaterState, "coolerState", svc.CoolerState)

	// enforce mode
	switch svc.Mode {
	case "heat":
		if msg.Metric < svc.MinTemp {
			logger.Info("thermostat: temp below min threshold, heating", "temp", msg.Metric, "minTemp", svc.MinTemp)
			heaterTargetState = "ON"
		} else if svc.HeaterState == "ON" && msg.Metric <= svc.MinTemp+svc.Hysteresis {
			logger.Info("thermostat: temp under min threshold + hysteresis", "temp", msg.Metric, "minTemp", svc.MinTemp, "hysteresis", svc.Hysteresis)
			heaterTargetState = "ON"
		}

	case "cool":
		if msg.Metric > svc.MaxTemp {
			logger.Info("thermostat: temp above max threshold, cooling", "temp", msg.Metric, "maxTemp", svc.MaxTemp)
			coolerTargetState = "ON"
		} else if svc.CoolerState == "ON" && msg.Metric >= svc.MaxTemp-svc.Hysteresis {
			logger.Info("thermostat: temp over max threshold + hysteresis", "temp", msg.Metric, "maxTemp", svc.MaxTemp, "hysteresis", svc.Hysteresis)
			coolerTargetState = "ON"
		}

	case "auto":
		// determine if heat or cool is needed based on current temp, min/max temps, and hysteresis
		if svc.HeaterState == "ON" && msg.Metric <= svc.MinTemp+svc.Hysteresis {
			logger.Info("thermostat: temp under min threshold + hysteresis", "temp", msg.Metric, "minTemp", svc.MinTemp, "hysteresis", svc.Hysteresis)
			heaterTargetState = "ON"
		} else if svc.CoolerState == "ON" && msg.Metric >= svc.MaxTemp-svc.Hysteresis {
			logger.Info("thermostat: temp over max threshold + hysteresis", "temp", msg.Metric, "maxTemp", svc.MaxTemp, "hysteresis", svc.Hysteresis)
			coolerTargetState = "ON"
		} else if msg.Metric < svc.MinTemp {
			logger.Info("thermostat: temp below min threshold, heating", "temp", msg.Metric, "minTemp", svc.MinTemp)
			heaterTargetState = "ON"
		} else if msg.Metric > svc.MaxTemp {
			logger.Info("thermostat: temp above max threshold, cooling", "temp", msg.Metric, "maxTemp", svc.MaxTemp)
			coolerTargetState = "ON"
		}

	default:
		logger.Error("thermostat: invalid mode, not heating or cooling", "mode", svc.Mode)
	}

	thermostatEvent := Event{
		Temp:              msg.Metric,
		MinTemp:           svc.MinTemp,
		MaxTemp:           svc.MaxTemp,
		Mode:              svc.Mode,
		HeaterTargetState: heaterTargetState,
		CoolerTargetState: coolerTargetState,
	}
	logger.Warn("thermostat event", "event", thermostatEvent)
	return thermostatEvent
}

func (svc Service) Check() error {
	return nil
}
