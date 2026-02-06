package heartbeat

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"time"
)

var startTime time.Time
var restartNotified bool

func init() {
	// capture the service start time for reporting uptime
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
	logger := svc.Deps.MustGetLogger()
	return util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"events", "metrics", "errors", "alerts"}, logger)
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

	metricPrefix, _ := svc.Cfg.Config["metricPrefix"].(string)
	metricName := svc.Cfg.Name
	if metricPrefix != "" {
		metricName = metricPrefix + svc.Cfg.Name
	}

	now := time.Now()
	heartbeat := Event{
		Now:           now,
		Uptime:        uptime.Round(time.Second).String(),
		UptimeSeconds: int64(uptime / time.Second),
	}
	logger.Debug("heartbeat", "data", heartbeat)

	if !restartNotified {
		// send an alert on service startup
		hostname, _ := util.GetShortHostname(svc.Deps.MustGetOsProvider())
		messenger.Send(core.Message{
			ChannelName: svc.Cfg.Pubs["alerts"].Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Text:        fmt.Sprintf("%s restarted", hostname),
		})
		restartNotified = true
	}

	eventErr := messenger.Send(core.Message{
		ChannelName: svc.Cfg.Pubs["events"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		MetricName:  metricName,
		Text:        fmt.Sprintf("heartbeat: uptime %s", heartbeat.Uptime),
		Metric:      float64(heartbeat.UptimeSeconds),
		Data:        heartbeat,
	})
	if eventErr != nil {
		return eventErr
	}

	metricErr := messenger.Send(core.Message{
		ChannelName: svc.Cfg.Pubs["metrics"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		MetricName:  metricName,
		Text:        fmt.Sprintf("heartbeat metric: uptime_seconds %d", heartbeat.UptimeSeconds),
		Metric:      float64(heartbeat.UptimeSeconds),
	})
	return metricErr
}
