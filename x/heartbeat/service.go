package heartbeat

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"time"
)

var startTime time.Time

func init() {
	startTime = time.Now()
}

type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

func (svc Service) ValidateConfig() []error {
	return util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"events"})
}

func (svc Service) Initialize() error {
	return nil
}

type Event struct {
	Now           time.Time
	Uptime        string
	UptimeSeconds int64
}

func (svc Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	uptime := time.Since(startTime)

	heartbeat := Event{
		Now:           time.Now(),
		Uptime:        uptime.Round(time.Second).String(),
		UptimeSeconds: int64(uptime / time.Second),
	}
	logger.Info("heartbeat", "data", heartbeat)

	msg := core.Message{
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Text:        fmt.Sprintf("heartbeat: uptime %s", heartbeat.Uptime),
		Value:       float64(heartbeat.UptimeSeconds),
	}
	logger.Debug("Sending to events channel", "msg", msg, "data", heartbeat)
	return messenger.Send(svc.Cfg.Pubs["events"].Name, msg, heartbeat)

}
