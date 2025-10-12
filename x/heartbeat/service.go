package heartbeat

import (
	"fmt"
	"keyop/core"
	"os"
	"time"
)

var startTime time.Time

func init() {
	startTime = time.Now()
}

type Service struct {
	Deps      core.Dependencies
	Cfg       core.ServiceConfig
	messenger core.MessengerApi
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	logger := deps.MustGetLogger()
	messenger := deps.MustGetMessenger()

	svc := Service{Deps: deps, Cfg: cfg, messenger: messenger}

	fmt.Fprintf(os.Stderr, "Starting heartbeat: %+v\n", cfg)
	errs := svc.validateConfig()
	if len(errs) > 0 {
		for _, err := range errs {
			logger.Error("Configuration error(s)", "error", err.Error())
		}
		panic("Configuration error(s) detected, see log for details")
	}

	return svc

	return svc
}

func (svc Service) validateConfig() []error {

	var errors []error

	if svc.Cfg.Name == "" {
		errors = append(errors, fmt.Errorf("required field 'name' is empty"))
	}
	if svc.Cfg.Type == "" {
		errors = append(errors, fmt.Errorf("required field 'type' is empty"))
	}
	if svc.Cfg.Pubs == nil {
		errors = append(errors, fmt.Errorf("required field 'pubs' is empty"))
	} else {
		_, eventsChanExists := svc.Cfg.Pubs["events"]
		if !eventsChanExists {
			errors = append(errors, fmt.Errorf("required publish channel 'events' is missing"))
		} else {
			// Ensure events channel has a name
			if svc.Cfg.Pubs["events"].Name == "" {
				errors = append(errors, fmt.Errorf("required publish channel 'events' is missing a name"))
			}
		}
	}

	return errors
}

type Event struct {
	Now           time.Time
	Uptime        string
	UptimeSeconds int64
}

func (svc Service) Check() error {
	logger := svc.Deps.MustGetLogger()

	uptime := time.Since(startTime)

	heartbeat := Event{
		Now:           time.Now(),
		Uptime:        uptime.Round(time.Second).String(),
		UptimeSeconds: int64(uptime / time.Second),
	}
	logger.Info("heartbeat", "data", heartbeat)

	eventsChan, eventsChanExists := svc.Cfg.Pubs["events"]
	if eventsChanExists {
		logger.Debug("Sending to events channel", "channel", eventsChan.Name)
		msg := core.Message{
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Text:        fmt.Sprintf("heartbeat: uptime %s", heartbeat.Uptime),
			Value:       float64(heartbeat.UptimeSeconds),
		}
		logger.Debug("Sending to events channel", "msg", msg, "data", heartbeat)
		err := svc.messenger.Send(eventsChan.Name, msg, heartbeat)
		if err != nil {
			logger.Error("Error sending to events channel %s: %s", eventsChan.Name, err.Error())
			return err
		}
	}

	return nil
}
