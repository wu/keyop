// Package heartbeat implements a service that emits heartbeat uptime events.
package heartbeat

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Compile-time interface assertion.
var _ core.PayloadProvider = (*Service)(nil)

// HeartbeatEvent represents a heartbeat from a service.
//
// Note: type name is intentionally HeartbeatEvent for clarity when used across tests
// and payload registration.
//
//nolint:revive
//goland:noinspection GoNameStartsWithPackageName
type HeartbeatEvent struct {
	Now           time.Time `json:"now"`
	Uptime        string    `json:"uptime"`
	UptimeSeconds int64     `json:"uptimeSeconds"`
}

// PayloadType returns the canonical payload type for heartbeat events.
func (h HeartbeatEvent) PayloadType() string { return "service.heartbeat.v1" }

// Service implements the heartbeat service which emits uptime events and a
// one-time restart alert per instance.
type Service struct {
	Deps            core.Dependencies
	Cfg             core.ServiceConfig
	startedAt       time.Time
	restartNotified bool
	mu              sync.Mutex
}

// NewService creates a new heartbeat service instance with an instance-scoped runtime state.
// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps:      deps,
		Cfg:       cfg,
		startedAt: time.Now(),
	}
}

// Name returns the canonical service name.
func (svc *Service) Name() string {
	return "heartbeat"
}

// RegisterPayloads registers the heartbeat payload types with the provided registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
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

// ValidateConfig returns configuration validation errors (none required for heartbeat).
func (svc *Service) ValidateConfig() []error {
	return nil
}

// Initialize performs any initialization for the service (no-op for heartbeat).
func (svc *Service) Initialize() error {
	return nil
}

// Check emits heartbeat events and a one-time restart event per instance.
func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	uptime := time.Since(svc.startedAt)

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

	// Send a restart event only once per service instance.
	svc.mu.Lock()
	shouldSendRestart := !svc.restartNotified
	if shouldSendRestart {
		svc.restartNotified = true
	}
	svc.mu.Unlock()

	if shouldSendRestart {
		hostname, _ := util.GetShortHostname(svc.Deps.MustGetOsProvider())
		alert := core.AlertEvent{
			Summary: fmt.Sprintf("%s restarted", hostname),
			Text:    fmt.Sprintf("Service %s on host %s restarted", svc.Cfg.Name, hostname),
			Level:   "info",
		}
		err := messenger.Send(core.Message{
			Correlation: correlationID,
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "restart",
			Text:        fmt.Sprintf("%s restarted", hostname),
			Data:        alert,
		})
		if err != nil {
			logger.Error("Failed to send restart alert", "error", err)
		}
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
