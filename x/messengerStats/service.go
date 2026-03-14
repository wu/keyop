// Package messengerStats implements the messengerStats service for keyop and provides ValidateConfig, Initialize and Check hooks.
//
//nolint:revive
package messengerStats

import (
	"fmt"
	"time"

	"keyop/core"

	"github.com/google/uuid"
)

// Service represents the messengerstats service which tracks message statistics.
type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig

	lastTotalMessageCount int64
	lastCheckTime         time.Time
}

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	return nil
}

// Initialize performs one-time startup required by the service (resource loading or connectivity checks).
func (svc *Service) Initialize() error {
	return nil
}

// Check performs the service's periodic work: collect data, evaluate state, and publish messages/metrics.
func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	stats := messenger.GetStats()
	logger.Debug("messenger stats", "stats", stats)

	currentTime := time.Now()
	correlationID := uuid.New().String()

	eventErr := messenger.Send(core.Message{
		Correlation: correlationID,
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "stats",
		Text:        fmt.Sprintf("messenger stats: total %d, failures %d, retries %d", stats.TotalMessageCount, stats.TotalFailureCount, stats.TotalRetryCount),
		Data:        stats,
	})
	if eventErr != nil {
		logger.Error("Failed to send messenger stats", "error", eventErr)
	}

	metricName, _ := svc.Cfg.Config["metric_name"].(string)
	if metricName == "" {
		metricName = svc.Cfg.Name
	}

	var metricErr error
	if !svc.lastCheckTime.IsZero() {
		deltaMessages := stats.TotalMessageCount - svc.lastTotalMessageCount
		deltaTime := currentTime.Sub(svc.lastCheckTime).Seconds()
		if deltaTime > 0 {
			msgsPerMinute := float64(deltaMessages) / (deltaTime / 60.0)

			metricErr = messenger.Send(core.Message{
				Correlation: correlationID,
				ChannelName: svc.Cfg.Name,
				ServiceName: svc.Cfg.Name,
				ServiceType: svc.Cfg.Type,
				Event:       "message_rate",
				MetricName:  metricName,
				Metric:      msgsPerMinute,
				Text:        fmt.Sprintf("message rate per minute: %.2f", msgsPerMinute),
			})
			if metricErr != nil {
				logger.Error("Failed to send messenger mpm metric", "error", metricErr)
			}
		}
	}

	svc.lastTotalMessageCount = stats.TotalMessageCount
	svc.lastCheckTime = currentTime

	if eventErr != nil {
		return eventErr
	}
	return metricErr
}
