//go:build linux

package switchgpio

import (
	"fmt"
	"keyop/core"
	"keyop/util"

	rpio "github.com/stianeikeland/go-rpio/v4"
)

// Service controls a GPIO pin in response to switch commands from a configurable channel.
type Service struct {
	Deps   core.Dependencies
	Cfg    core.ServiceConfig
	pin    rpio.Pin
	pinNum int
}

// NewService creates a new switch service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps: deps,
		Cfg:  cfg,
	}

	if p, ok := cfg.Config["pin"].(int); ok {
		svc.pinNum = p
	} else if p, ok := cfg.Config["pin"].(float64); ok {
		svc.pinNum = int(p)
	}

	svc.pin = rpio.Pin(svc.pinNum)
	return svc
}

// ValidateConfig validates the service configuration.
func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("subs", svc.Cfg.Subs, []string{"switch"}, logger)

	if _, ok := svc.Cfg.Config["pin"]; !ok {
		err := fmt.Errorf("switch: pin not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}
	return errs
}

// Initialize opens RPIO and subscribes to the switch command channel.
func (svc *Service) Initialize() error {
	if err := rpio.Open(); err != nil {
		return fmt.Errorf("switch: failed to open rpio: %w", err)
	}
	svc.pin.Output()

	messenger := svc.Deps.MustGetMessenger()
	return messenger.Subscribe(
		svc.Deps.MustGetContext(),
		svc.Cfg.Name,
		svc.Cfg.Subs["switch"].Name,
		svc.Cfg.Type,
		svc.Cfg.Name,
		svc.Cfg.Subs["switch"].MaxAge,
		svc.commandHandler,
	)
}

func (svc *Service) commandHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()

	cmd, ok := core.AsType[*core.SwitchCommand](msg.Data)
	if !ok {
		if cv, ok2 := core.AsType[core.SwitchCommand](msg.Data); ok2 {
			cmd = &cv
		}
	}
	if cmd == nil {
		return nil
	}
	if cmd.DeviceName != svc.Cfg.Name {
		return nil
	}

	currentState := pinState(svc.pin.Read())
	targetState := cmd.State

	logger.Info("switch: received command", "device", svc.Cfg.Name, "current", currentState, "target", targetState)

	if currentState != targetState {
		if targetState == "ON" {
			svc.pin.High()
		} else {
			svc.pin.Low()
		}
		logger.Info("switch: pin state changed", "device", svc.Cfg.Name, "state", targetState)
	}

	newState := pinState(svc.pin.Read())
	svc.emitEvent(msg, newState)
	return nil
}

func (svc *Service) emitEvent(trigger core.Message, state string) {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	event := Event{DeviceName: svc.Cfg.Name, State: state}
	if err := messenger.Send(core.Message{
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "switch_event",
		Text:        fmt.Sprintf("%s state is %s", svc.Cfg.Name, state),
		State:       state,
		Data:        &event,
		Correlation: trigger.Uuid,
	}); err != nil {
		logger.Warn("switch: failed to emit switch_event", "err", err)
	}
}

// Check is a no-op; the switch service is event-driven.
func (svc *Service) Check() error {
	return nil
}

func pinState(s rpio.State) string {
	if s == rpio.High {
		return "ON"
	}
	return "OFF"
}
