package thermostat

import (
	"database/sql"
	"fmt"
	"keyop/core"
	"keyop/util"
)

// Service orchestrates thermostat control loops, schedules, and integration with temperature sensors.
type Service struct {
	Deps       core.Dependencies
	Cfg        core.ServiceConfig
	MinTemp    float64
	MaxTemp    float64
	Mode       string // "heat", "cool", "auto", "off"
	Hysteresis float64
	HeaterName string
	CoolerName string
	lastEvent  *Event
	db         **sql.DB
}

var validModes = map[string]bool{
	"heat": true,
	"cool": true,
	"auto": true,
	"off":  true,
}

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps: deps,
		Cfg:  cfg,
	}

	if minTemp, ok := svc.Cfg.Config["minTemp"]; ok {
		svc.MinTemp = minTemp.(float64)
	}
	if maxTemp, ok := svc.Cfg.Config["maxTemp"]; ok {
		svc.MaxTemp = maxTemp.(float64)
	}
	if mode, ok := svc.Cfg.Config["mode"]; ok {
		svc.Mode = mode.(string)
	}
	if hysteresis, ok := svc.Cfg.Config["hysteresis"]; ok {
		svc.Hysteresis = hysteresis.(float64)
	}
	svc.HeaterName = "unnamed_heater"
	if name, ok := svc.Cfg.Config["heaterName"].(string); ok && name != "" {
		svc.HeaterName = name
	}
	svc.CoolerName = "unnamed_cooler"
	if name, ok := svc.Cfg.Config["coolerName"].(string); ok && name != "" {
		svc.CoolerName = name
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

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()

	errs := util.ValidateConfig("subs", svc.Cfg.Subs, []string{"temp"}, logger)

	if _, ok := svc.Cfg.Config["minTemp"].(float64); !ok {
		err := fmt.Errorf("thermostat: minTemp not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}
	if _, ok := svc.Cfg.Config["maxTemp"].(float64); !ok {
		err := fmt.Errorf("thermostat: maxTemp not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}
	if svc.MinTemp > svc.MaxTemp {
		err := fmt.Errorf("thermostat: minTemp must be less than or equal to maxTemp (minTemp: %f, maxTemp: %f)", svc.MinTemp, svc.MaxTemp)
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	mode, modeExists := svc.Cfg.Config["mode"].(string)
	if !modeExists {
		err := fmt.Errorf("thermostat: mode not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	} else if _, valid := validModes[mode]; !valid {
		err := fmt.Errorf("thermostat: invalid mode '%s' in config, must be one of %v", mode, getValidModes())
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	return errs
}

// Initialize performs one-time startup required by the service (resource loading or connectivity checks).
func (svc *Service) Initialize() error {
	messenger := svc.Deps.MustGetMessenger()
	return messenger.Subscribe(svc.Deps.MustGetContext(), svc.Cfg.Name, svc.Cfg.Subs["temp"].Name, svc.Cfg.Type, svc.Cfg.Name, svc.Cfg.Subs["temp"].MaxAge, svc.tempHandler)
}

func (svc *Service) tempHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	logger.Info("thermostat received temp message", "message", msg)

	var temp float64
	if tp, ok := core.AsType[*core.TempEvent](msg.Data); ok && tp != nil {
		temp = float64(tp.TempF)
	} else if tv, ok := core.AsType[core.TempEvent](msg.Data); ok {
		temp = float64(tv.TempF)
	} else {
		temp = msg.Metric
	}

	event := svc.updateState(temp, logger)
	logger.Info("thermostat state updated", "event", event)

	if err := messenger.Send(core.Message{
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "thermostat_event",
		Text:        fmt.Sprintf("%s target state is %s", svc.Cfg.Name, event.HeaterTargetState),
		State:       event.HeaterTargetState,
		Data:        &event,
		DataType:    event.PayloadType(),
		Correlation: msg.Uuid,
	}); err != nil {
		logger.Warn("failed to send thermostat_event message", "err", err)
	}

	heaterSwitch := core.SwitchEvent{DeviceName: svc.HeaterName, State: event.HeaterTargetState}
	if err := messenger.Send(core.Message{
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "switch_event",
		Text:        fmt.Sprintf("%s state is %s", svc.HeaterName, event.HeaterTargetState),
		State:       event.HeaterTargetState,
		Data:        &heaterSwitch,
		DataType:    heaterSwitch.PayloadType(),
		Correlation: msg.Uuid,
	}); err != nil {
		logger.Warn("failed to send heater switch_event message", "err", err)
	}

	coolerSwitch := core.SwitchEvent{DeviceName: svc.CoolerName, State: event.CoolerTargetState}
	if err := messenger.Send(core.Message{
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "switch_event",
		Text:        fmt.Sprintf("%s state is %s", svc.CoolerName, event.CoolerTargetState),
		State:       event.CoolerTargetState,
		Data:        &coolerSwitch,
		DataType:    coolerSwitch.PayloadType(),
		Correlation: msg.Uuid,
	}); err != nil {
		logger.Warn("failed to send cooler switch_event message", "err", err)
	}

	return nil
}

func (svc *Service) updateState(temp float64, logger core.Logger) Event {
	heaterTargetState := "OFF"
	coolerTargetState := "OFF"

	heaterOn := svc.lastEvent != nil && svc.lastEvent.HeaterTargetState == "ON"
	coolerOn := svc.lastEvent != nil && svc.lastEvent.CoolerTargetState == "ON"

	logger.Info("thermostat: current temp", "temp", temp, "minTemp", svc.MinTemp, "maxTemp", svc.MaxTemp, "mode", svc.Mode, "hysteresis", svc.Hysteresis, "heaterOn", heaterOn, "coolerOn", coolerOn)

	switch svc.Mode {
	case "heat":
		if temp < svc.MinTemp {
			logger.Info("thermostat: temp below min threshold, heating", "temp", temp, "minTemp", svc.MinTemp)
			heaterTargetState = "ON"
		} else if heaterOn && temp <= svc.MinTemp+svc.Hysteresis {
			logger.Info("thermostat: temp under min threshold + hysteresis", "temp", temp, "minTemp", svc.MinTemp, "hysteresis", svc.Hysteresis)
			heaterTargetState = "ON"
		}

	case "cool":
		if temp > svc.MaxTemp {
			logger.Info("thermostat: temp above max threshold, cooling", "temp", temp, "maxTemp", svc.MaxTemp)
			coolerTargetState = "ON"
		} else if coolerOn && temp >= svc.MaxTemp-svc.Hysteresis {
			logger.Info("thermostat: temp over max threshold + hysteresis", "temp", temp, "maxTemp", svc.MaxTemp, "hysteresis", svc.Hysteresis)
			coolerTargetState = "ON"
		}

	case "auto":
		if heaterOn && temp <= svc.MinTemp+svc.Hysteresis {
			logger.Info("thermostat: temp under min threshold + hysteresis", "temp", temp, "minTemp", svc.MinTemp, "hysteresis", svc.Hysteresis)
			heaterTargetState = "ON"
		} else if coolerOn && temp >= svc.MaxTemp-svc.Hysteresis {
			logger.Info("thermostat: temp over max threshold + hysteresis", "temp", temp, "maxTemp", svc.MaxTemp, "hysteresis", svc.Hysteresis)
			coolerTargetState = "ON"
		} else if temp < svc.MinTemp {
			logger.Info("thermostat: temp below min threshold, heating", "temp", temp, "minTemp", svc.MinTemp)
			heaterTargetState = "ON"
		} else if temp > svc.MaxTemp {
			logger.Info("thermostat: temp above max threshold, cooling", "temp", temp, "maxTemp", svc.MaxTemp)
			coolerTargetState = "ON"
		}

	default:
		logger.Error("thermostat: invalid mode, not heating or cooling", "mode", svc.Mode)
	}

	event := Event{
		Temp:              temp,
		MinTemp:           svc.MinTemp,
		MaxTemp:           svc.MaxTemp,
		Mode:              svc.Mode,
		HeaterTargetState: heaterTargetState,
		CoolerTargetState: coolerTargetState,
	}
	svc.lastEvent = &event
	logger.Warn("thermostat event", "event", event)
	return event
}

// Check performs the service's periodic work: collect data, evaluate state, and publish messages/metrics.
func (svc *Service) Check() error {
	return nil
}
