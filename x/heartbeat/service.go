package heartbeat

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"time"

	"github.com/google/uuid"
)

var startTime time.Time
var restartNotified bool

func init() {
	// capture the service start time for reporting uptime
	startTime = time.Now()
}

// HeartbeatEvent represents a heartbeat from a service.
type HeartbeatEvent struct {
	Now           time.Time `json:"now"`
	Uptime        string    `json:"uptime"`
	UptimeSeconds int64     `json:"uptimeSeconds"`
}

func (h HeartbeatEvent) PayloadType() string { return "service.heartbeat.v1" }

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

func (svc Service) Name() string {
	return "heartbeat"
}

func (svc Service) RegisterPayloads(reg core.PayloadRegistry) error {
	if err := reg.Register("heartbeat", func() any { return &HeartbeatEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register heartbeat alias: %w", err)
		}
	}
	if err := reg.Register("service.heartbeat.v1", func() any { return &HeartbeatEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register service.heartbeat.v1: %w", err)
		}
	}
	return nil
}

func (svc Service) ValidateConfig() []error {
	return nil
}

func (svc Service) Initialize() error {
	return nil
}

func (svc Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	uptime := time.Since(startTime)

	metricName, _ := svc.Cfg.Config["metricName"].(string)
	if metricName == "" {
		metricName = svc.Cfg.Name
	}

	now := time.Now()
	heartbeat := HeartbeatEvent{
		Now:           now,
		Uptime:        uptime.Round(time.Second).String(),
		UptimeSeconds: int64(uptime / time.Second),
	}
	logger.Debug("heartbeat", "data", heartbeat)

	// generate correlation ID for this check to tie together the events and metrics in the backend
	correlationID := uuid.New().String()
	if !restartNotified {
		// send an alert on service startup
		hostname, _ := util.GetShortHostname(svc.Deps.MustGetOsProvider())
		err := messenger.Send(core.Message{
			Correlation: correlationID,
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "restart",
			Text:        fmt.Sprintf("%s restarted", hostname),
		})
		if err != nil {
			logger.Error("Failed to send restart alert", "error", err)
		}
		restartNotified = true
	}

	eventErr := messenger.Send(core.Message{
		Correlation: correlationID,
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "uptime_check",
		MetricName:  metricName,
		Text:        fmt.Sprintf("heartbeat: uptime %s", heartbeat.Uptime),
		Metric:      float64(heartbeat.UptimeSeconds),
		Data:        heartbeat,
	})
	if eventErr != nil {
		return eventErr
	}

	metricErr := messenger.Send(core.Message{
		Correlation: correlationID,
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "uptime_metric",
		MetricName:  metricName,
		Text:        fmt.Sprintf("heartbeat metric: uptime_seconds %d", heartbeat.UptimeSeconds),
		Metric:      float64(heartbeat.UptimeSeconds),
	})
	return metricErr
}
