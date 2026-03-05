//nolint:revive
package messengerStats

import (
	"fmt"
	"time"

	"keyop/core"

	"github.com/google/uuid"
)

type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig

	lastTotalMessageCount int64
	lastCheckTime         time.Time
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

func (svc *Service) ValidateConfig() []error {
	return nil
}

func (svc *Service) Initialize() error {
	return nil
}

func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	stats := messenger.GetStats()
	logger.Debug("messenger stats", "stats", stats)

	currentTime := time.Now()
	correlationId := uuid.New().String()

	eventErr := messenger.Send(core.Message{
		Correlation: correlationId,
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
				Correlation: correlationId,
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
