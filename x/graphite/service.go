//nolint:revive
package graphite

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"strings"
	"time"

	graphiteClient "github.com/marpaia/graphite-golang"
)

func init() {
}

type Service struct {
	Deps     core.Dependencies
	Cfg      core.ServiceConfig
	Graphite *graphiteClient.Graphite
	Host     string
	Port     int
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
	logger := svc.Deps.MustGetLogger()

	var errs []error

	subErrs := util.ValidateConfig("subs", svc.Cfg.Subs, []string{"graphite"}, logger)
	if len(subErrs) > 0 {
		errs = append(errs, subErrs...)
	}

	// check port
	port, portExists := svc.Cfg.Config["port"].(int)
	if !portExists {
		err := fmt.Errorf("graphite: port not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	// check hostname
	hostname, hostnameExists := svc.Cfg.Config["hostname"].(string)
	if !hostnameExists {
		err := fmt.Errorf("graphite: hostname not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errs
	}

	// set host and port
	svc.Host = hostname
	svc.Port = port

	logger.Info("Graphite configured to connect to", "host", svc.Host, "port", svc.Port)

	return errs
}

// Initialize performs one-time startup required by the service (resource loading or connectivity checks).
func (svc *Service) Initialize() error {
	messenger := svc.Deps.MustGetMessenger()
	return messenger.Subscribe(svc.Deps.MustGetContext(), svc.Cfg.Name, svc.Cfg.Subs["graphite"].Name, svc.Cfg.Type, svc.Cfg.Name, svc.Cfg.Subs["graphite"].MaxAge, svc.messageHandler)
}

func (svc *Service) messageHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()
	logger.Debug("graphite message", "msg", msg)

	value := fmt.Sprintf("%2.2f", msg.Metric)

	metricName := msg.MetricName
	if metricName == "" {
		metricName = msg.ServiceName
	}
	if !strings.HasPrefix(metricName, "weewx.") {
		metricName = fmt.Sprintf("keyop.%s", metricName)
	}

	unixTime := msg.Timestamp.Unix()
	logger.Info("Sending to Graphite",
		"time", time.Unix(unixTime, 0).Format("2006-01-02 15:04:05"),
		"service", msg.ServiceName,
		"plugin", msg.ServiceType,
		"metric", metricName,
		"value", value,
	)

	metric := graphiteClient.NewMetric(metricName, fmt.Sprintf("%v", value), unixTime)

	if svc.Graphite == nil {
		logger.Info("Graphite connection is nil, attempting to connect to Graphite", "host", svc.Host, "port", svc.Port)
		var err error
		svc.Graphite, err = graphiteClient.NewGraphite(svc.Host, svc.Port)
		if err != nil {
			logger.Error("ERROR: failed to connect to Graphite", "err", err.Error())
			svc.Graphite = nil
			return err
		}
		logger.Info("Graphite connection established", "host", svc.Host, "port", svc.Port)
	}

	err := svc.Graphite.SendMetric(metric)
	if err != nil {
		logger.Error("ERROR: failed sending metric to Graphite, resetting connection", "err", err.Error())
		// Discard the broken connection; the next message will trigger a fresh reconnect.
		svc.Graphite = nil
		return err
	}

	return nil
}

// Check performs the service's periodic work: collect data, evaluate state, and publish messages/metrics.
func (svc *Service) Check() error {
	return nil
}
