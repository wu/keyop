package messengerStats

import (
	"fmt"
	"keyop/core"
	"keyop/util"

	"github.com/google/uuid"
)

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
	return util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"events", "metrics"}, logger)
}

func (svc Service) Initialize() error {
	return nil
}

func (svc Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	stats := messenger.GetStats()
	logger.Debug("messenger stats", "stats", stats)

	msgUuid := uuid.New().String()

	// send to events channel
	eventErr := messenger.Send(core.Message{
		Uuid:        msgUuid,
		ChannelName: svc.Cfg.Pubs["events"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Text:        fmt.Sprintf("messenger stats: total %d, failures %d, retries %d", stats.TotalMessageCount, stats.TotalFailureCount, stats.TotalRetryCount),
		Data:        stats,
	})
	if eventErr != nil {
		logger.Error("Failed to send messenger stats to events channel", "error", eventErr)
	}

	// send total to metrics channel
	metricName, _ := svc.Cfg.Config["metric_name"].(string)
	if metricName == "" {
		metricName = "total_messages"
	}

	metricErr := messenger.Send(core.Message{
		Uuid:        msgUuid,
		ChannelName: svc.Cfg.Pubs["metrics"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		MetricName:  metricName,
		Metric:      float64(stats.TotalMessageCount),
		Text:        fmt.Sprintf("total messages: %d", stats.TotalMessageCount),
	})
	if metricErr != nil {
		logger.Error("Failed to send messenger stats to metrics channel", "error", metricErr)
	}

	if eventErr != nil {
		return eventErr
	}
	return metricErr
}
