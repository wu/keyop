package pingMonitor

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"regexp"
	"strconv"
)

type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	var errs []error

	errs = append(errs, util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"alerts", "events", "metrics"}, logger)...)

	host, _ := svc.Cfg.Config["host"].(string)
	if host == "" {
		err := fmt.Errorf("host is required in pingMonitor config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	return errs
}

func (svc *Service) Initialize() error {
	return nil
}

func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()
	osProvider := svc.Deps.MustGetOsProvider()

	host, _ := svc.Cfg.Config["host"].(string)
	if host == "" {
		return fmt.Errorf("host not configured")
	}

	cmd := osProvider.Command("ping", "-c", "1", "-W", "2", host)
	output, err := cmd.CombinedOutput()

	if err != nil {
		logger.Warn("Network outage detected", "host", host, "error", err, "output", string(output))

		alertErr := messenger.Send(core.Message{
			ChannelName: svc.Cfg.Pubs["alerts"].Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Text:        fmt.Sprintf("host %s is unreachable", host),
		})
		if alertErr != nil {
			logger.Error("Failed to send network outage alert", "error", alertErr)
			return alertErr
		}
	} else {
		logger.Debug("Network check passed", "host", host)

		pingTime := extractPingTime(string(output))

		eventErr := messenger.Send(core.Message{
			ChannelName: svc.Cfg.Pubs["events"].Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Text:        fmt.Sprintf("Ping to %s was successful. Time: %s", host, pingTime),
		})
		if eventErr != nil {
			logger.Error("Failed to send success event", "error", eventErr)
		}

		if pingTime != "" {
			floatTime, err := strconv.ParseFloat(pingTime, 64)
			if err == nil {
				metricName, _ := svc.Cfg.Config["metric_name"].(string)
				if metricName == "" {
					metricName = fmt.Sprintf("%s.ping_time", svc.Cfg.Name)
				}
				metricErr := messenger.Send(core.Message{
					ChannelName: svc.Cfg.Pubs["metrics"].Name,
					ServiceName: svc.Cfg.Name,
					ServiceType: svc.Cfg.Type,
					MetricName:  metricName,
					Metric:      floatTime,
					Text:        fmt.Sprintf("Ping time to %s: %s ms", host, pingTime),
				})
				if metricErr != nil {
					logger.Error("Failed to send ping metric", "error", metricErr)
				}
			}
		}
	}

	return nil
}

func extractPingTime(output string) string {
	// macOS/Linux format usually includes "time=XX.XX ms" or round-trip line
	// e.g., "64 bytes from ...: icmp_seq=1 ttl=64 time=10.9 ms"
	// or "round-trip min/avg/max/stddev = 10.977/10.977/10.977/nan ms"
	re := regexp.MustCompile(`time=([0-9.]+)`)
	match := re.FindStringSubmatch(output)
	if len(match) > 1 {
		return match[1]
	}

	// try round-trip format
	reRT := regexp.MustCompile(`min/avg/max/.*? = [0-9.]+/([0-9.]+)/[0-9.]+`)
	matchRT := reRT.FindStringSubmatch(output)
	if len(matchRT) > 1 {
		return matchRT[1]
	}

	return ""
}
