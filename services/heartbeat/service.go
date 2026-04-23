// Package heartbeat implements a service that emits heartbeat uptime events.
package heartbeat

import (
	"context"
	"fmt"
	"github.com/wu/keyop/core"
	"sync"
	"time"
)

// Compile-time interface assertion.

// Service implements the heartbeat service which emits uptime events and a
// one-time restart alert per instance.
type Service struct {
	Deps            core.Dependencies
	Cfg             core.ServiceConfig
	ctx             context.Context
	startedAt       time.Time
	restartNotified bool
	mu              sync.Mutex
}

// NewService creates a new heartbeat service instance with an instance-scoped runtime state.
func NewService(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) core.Service {
	return &Service{
		Deps:      deps,
		Cfg:       cfg,
		ctx:       ctx,
		startedAt: time.Now(),
	}
}

// ValidateConfig returns configuration validation errors (none required for heartbeat).
func (svc *Service) ValidateConfig() []error {
	return nil
}

// Initialize registers payload types with the new messenger.
func (svc *Service) Initialize() error {
	newMsgr := svc.Deps.MustGetMessenger()
	if newMsgr == nil {
		return fmt.Errorf("new messenger not initialized; messenger.yaml must be present")
	}
	return RegisterPayloadTypes(newMsgr, svc.Deps.MustGetLogger())
}

// Check emits heartbeat events and a one-time restart event per instance.
func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	newMsgr := svc.Deps.MustGetMessenger()
	if newMsgr == nil {
		return fmt.Errorf("new messenger not initialized; messenger.yaml must be present")
	}

	uptime := time.Since(svc.startedAt)

	metricName, _ := svc.Cfg.Config["metricName"].(string)
	if metricName == "" {
		metricName = svc.Cfg.Name
	}

	hostname := newMsgr.InstanceName()

	now := time.Now()
	heartbeat := HeartbeatEvent{
		Hostname:      hostname,
		Now:           now,
		Uptime:        uptime.Round(time.Second).String(),
		UptimeSeconds: int64(uptime / time.Second),
	}
	logger.Debug("heartbeat", "data", heartbeat)

	// Send a restart event only once per service instance.
	svc.mu.Lock()
	shouldSendRestart := !svc.restartNotified
	if shouldSendRestart {
		svc.restartNotified = true
	}
	svc.mu.Unlock()

	if shouldSendRestart {
		alert := core.AlertEvent{
			Timestamp: now,
			Hostname:  hostname,
			Text:      fmt.Sprintf("%s restarted", hostname),
			Summary:   fmt.Sprintf("%s restarted", hostname),
			Level:     "info",
		}
		err := newMsgr.Publish(
			svc.ctx,
			"alerts",
			"core.alert.v1",
			&alert,
		)
		if err != nil {
			logger.Error("Failed to publish restart alert", "error", err)
		}
	}

	eventErr := newMsgr.Publish(
		svc.ctx,
		"heartbeat",
		"service.heartbeat.v1",
		&heartbeat,
	)
	if eventErr != nil {
		return eventErr
	}

	// Also publish as a metric event for metric aggregation
	metricErr := newMsgr.Publish(
		svc.ctx,
		"metrics",
		"core.metric.v1",
		&core.MetricEvent{Hostname: hostname, Name: metricName, Value: float64(heartbeat.UptimeSeconds)},
	)
	return metricErr
}
