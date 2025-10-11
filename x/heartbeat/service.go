package heartbeat

import (
	"keyop/core"
	"keyop/util"
	"time"
)

var startTime time.Time

func init() {
	startTime = time.Now()
}

type Service struct {
	Deps          core.Dependencies
	Cfg           core.ServiceConfig
	ShortHostname string
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	logger := deps.MustGetLogger()
	os := deps.MustGetOsProvider()
	hostname, err := util.GetShortHostname(os)
	if err != nil {
		logger.Error("Error getting hostname", "error", err)
	}
	if hostname == "" {
		logger.Error("Error getting hostname", "error", "hostname was empty")
	}

	return &Service{Deps: deps, Cfg: cfg, ShortHostname: hostname}
}

type Event struct {
	Now           time.Time
	Uptime        string
	UptimeSeconds int64
	Hostname      string
}

func (svc Service) Check() error {
	logger := svc.Deps.MustGetLogger()

	uptime := time.Since(startTime)

	heartbeat := Event{
		Now:           time.Now(),
		Uptime:        uptime.Round(time.Second).String(),
		UptimeSeconds: int64(uptime / time.Second),
		Hostname:      svc.ShortHostname,
	}
	logger.Info("heartbeat", "data", heartbeat)

	// TODO: send to svc.Cfg.Pubs["events"].Name)

	return nil
}
