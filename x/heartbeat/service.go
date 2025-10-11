package heartbeat

import (
	"keyop/core"
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
	return &Service{Deps: deps, Cfg: cfg}
}

type Event struct {
	Now           time.Time
	Uptime        string
	UptimeSeconds int64
	Hostname      string
}

func (svc Service) Check() error {
	logger := svc.Deps.Logger
	logger.Debug("heartbeat called")

	uptime := time.Since(startTime)

	heartbeat := Event{
		Now:           time.Now(),
		Uptime:        uptime.Round(time.Second).String(),
		UptimeSeconds: int64(uptime / time.Second),
		Hostname:      svc.Deps.Hostname,
	}
	logger.Info("heartbeat", "data", heartbeat)

	// TODO: send to svc.Cfg.Pubs["events"].Name)

	return nil
}
