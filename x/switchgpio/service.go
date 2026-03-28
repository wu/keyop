//go:build linux

package switchgpio

import (
	"fmt"
	"keyop/core"
	"keyop/util"

	"github.com/google/uuid"
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
	logger := svc.Deps.MustGetLogger()
	logger.Info("switch: opening rpio", "device", svc.Cfg.Name, "pin", svc.pinNum)
	if err := rpio.Open(); err != nil {
		svc.sendErrorEvent(fmt.Sprintf("switch: failed to open rpio: %v", err), "error")
		return fmt.Errorf("switch: failed to open rpio: %w", err)
	}
	svc.pin = rpio.Pin(svc.pinNum)
	svc.pin.Output()
	logger.Info("switch: pin initialised", "device", svc.Cfg.Name, "pin", svc.pinNum, "state", pinState(svc.pin.Read()))

	messenger := svc.Deps.MustGetMessenger()
	logger.Info("switch: subscribing to channel", "device", svc.Cfg.Name, "channel", svc.Cfg.Subs["switch"].Name)
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
		logger.Warn("switch: received message with no SwitchCommand payload", "device", svc.Cfg.Name, "event", msg.Event)
		return nil
	}
	if cmd.DeviceName != svc.Cfg.Name {
		logger.Debug("switch: ignoring command for other device", "device", svc.Cfg.Name, "target", cmd.DeviceName)
		return nil
	}

	currentState := pinState(svc.pin.Read())
	targetState := cmd.State

	logger.Info("switch: received command", "device", svc.Cfg.Name, "pin", svc.pinNum, "current", currentState, "target", targetState)

	if currentState != targetState {
		if targetState == "ON" {
			svc.pin.High()
		} else {
			svc.pin.Low()
		}
		logger.Info("switch: pin state changed", "device", svc.Cfg.Name, "pin", svc.pinNum, "state", targetState)
	} else {
		logger.Info("switch: pin already in target state, no write needed", "device", svc.Cfg.Name, "pin", svc.pinNum, "state", currentState)
	}

	svc.emitEvent(msg, targetState)
	return nil
}

func (svc *Service) sendErrorEvent(summary, level string) {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()
	errEvent := core.ErrorEvent{
		Summary: summary,
		Text:    summary,
		Level:   level,
	}
	if err := messenger.Send(core.Message{
		Correlation: uuid.New().String(),
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "switch_error",
		Text:        summary,
		Data:        errEvent,
	}); err != nil {
		logger.Error("switch: failed to send error event", "err", err)
	}
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
