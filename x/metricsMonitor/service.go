package metricsMonitor

import (
	"fmt"
	"keyop/core"
	"keyop/util"
)

type Threshold struct {
	MetricName string  `json:"metricName"`
	Value      float64 `json:"value"`
	Condition  string  `json:"condition"` // "above" or "below"
	Status     string  `json:"status"`
	AlertText  string  `json:"alertText"`
}

type Service struct {
	Deps       core.Dependencies
	Cfg        core.ServiceConfig
	Thresholds []Threshold
	lastStatus map[string]bool
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps:       deps,
		Cfg:        cfg,
		lastStatus: make(map[string]bool),
	}

	if thresholdsRaw, ok := cfg.Config["thresholds"].([]interface{}); ok {
		for _, tRaw := range thresholdsRaw {
			if tMap, ok := tRaw.(map[string]interface{}); ok {
				t := Threshold{}
				if v, ok := tMap["metricName"].(string); ok {
					t.MetricName = v
				}
				if v, ok := tMap["value"].(float64); ok {
					t.Value = v
				}
				if v, ok := tMap["condition"].(string); ok {
					t.Condition = v
				}
				if v, ok := tMap["status"].(string); ok {
					t.Status = v
				}
				if v, ok := tMap["alertText"].(string); ok {
					t.AlertText = v
				}
				svc.Thresholds = append(svc.Thresholds, t)
			}
		}
	}

	return svc
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("subs", svc.Cfg.Subs, []string{"metrics"}, logger)

	// thresholds is optional but recommended
	if _, ok := svc.Cfg.Config["thresholds"].([]interface{}); !ok {
		logger.Warn("metricsMonitor: 'thresholds' not found or not an array in config")
	}

	// alerts pub is required if we want to send alerts
	if _, ok := svc.Cfg.Pubs["alerts"]; !ok {
		errs = append(errs, fmt.Errorf("metricsMonitor: required pubs channel 'alerts' is missing"))
	}

	return errs
}

func (svc *Service) Initialize() error {
	messenger := svc.Deps.MustGetMessenger()
	return messenger.Subscribe(svc.Cfg.Name, svc.Cfg.Subs["metrics"].Name, svc.messageHandler)
}

func (svc *Service) messageHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()

	anyTriggered := false
	for i, t := range svc.Thresholds {
		if t.MetricName != "" && msg.MetricName != t.MetricName {
			continue
		}

		triggered := false
		if t.Condition == "above" {
			if msg.Metric > t.Value {
				triggered = true
			}
		} else if t.Condition == "below" {
			if msg.Metric < t.Value {
				triggered = true
			}
		}

		stateKey := fmt.Sprintf("%s_%d", msg.MetricName, i)
		lastTriggered := svc.lastStatus[stateKey]

		if triggered != lastTriggered {
			svc.lastStatus[stateKey] = triggered

			if triggered && !anyTriggered {
				logger.Info("Threshold triggered", "metric", msg.MetricName, "value", msg.Metric, "condition", t.Condition, "threshold", t.Value)

				messenger := svc.Deps.MustGetMessenger()
				alertMsg := core.Message{
					ChannelName: svc.Cfg.Pubs["alerts"].Name,
					ServiceName: svc.Cfg.Name,
					ServiceType: svc.Cfg.Type,
					Text:        fmt.Sprintf("ALERT: %s (%.2f) is %s %.2f: %s", msg.MetricName, msg.Metric, t.Condition, t.Value, t.AlertText),
					MetricName:  msg.MetricName,
					Metric:      msg.Metric,
					Status:      t.Status,
					Data: map[string]interface{}{
						"originalMessage": msg,
						"threshold":       t,
					},
				}
				if err := messenger.Send(alertMsg); err != nil {
					return err
				}
			}
		}
		if triggered {
			anyTriggered = true
		}
	}

	return nil
}

func (svc *Service) Check() error {
	return nil
}
