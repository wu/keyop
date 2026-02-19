package metricsMonitor

import (
	"fmt"
	"keyop/core"
	"keyop/util"
)

type Threshold struct {
	MetricName        string   `json:"metricName"`
	Value             float64  `json:"value"`
	RecoveryThreshold *float64 `json:"recoveryThreshold,omitempty"`
	Condition         string   `json:"condition"` // "above" or "below"
	Status            string   `json:"status"`
	Hostname          string   `json:"hostname,omitempty"`
	ServiceName       string   `json:"serviceName,omitempty"`
}

type Service struct {
	Deps       core.Dependencies
	Cfg        core.ServiceConfig
	Thresholds []Threshold
	lastStatus map[string]string
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps:       deps,
		Cfg:        cfg,
		lastStatus: make(map[string]string),
	}

	hostname, err := util.GetShortHostname(deps.MustGetOsProvider())
	if err != nil {
		hostname = "unknown"
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
				} else if v, ok := tMap["value"].(int); ok {
					t.Value = float64(v)
				}
				if v, ok := tMap["recoveryThreshold"].(float64); ok {
					t.RecoveryThreshold = &v
				} else if v, ok := tMap["recoveryThreshold"].(int); ok {
					fv := float64(v)
					t.RecoveryThreshold = &fv
				}
				if v, ok := tMap["condition"].(string); ok {
					t.Condition = v
				}
				if v, ok := tMap["status"].(string); ok {
					t.Status = v
				}
				t.Hostname = hostname
				t.ServiceName = svc.Cfg.Name
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
	if thresholdsRaw, ok := svc.Cfg.Config["thresholds"].([]interface{}); !ok {
		logger.Warn("metricsMonitor: 'thresholds' not found or not an array in config")
	} else {
		for i, tRaw := range thresholdsRaw {
			if tMap, ok := tRaw.(map[string]interface{}); ok {
				if v, ok := tMap["value"]; ok {
					if _, ok := v.(float64); !ok {
						if _, ok := v.(int); !ok {
							errs = append(errs, fmt.Errorf("metricsMonitor: threshold %d 'value' must be a number, got %T", i, v))
						}
					}
				}
				if v, ok := tMap["recoveryThreshold"]; ok {
					if _, ok := v.(float64); !ok {
						if _, ok := v.(int); !ok {
							errs = append(errs, fmt.Errorf("metricsMonitor: threshold %d 'recoveryThreshold' must be a number, got %T", i, v))
						}
					}
				}
			}
		}
	}

	// status pub is required to work with statusMonitor
	if _, ok := svc.Cfg.Pubs["status"]; !ok {
		errs = append(errs, fmt.Errorf("metricsMonitor: required pubs channel 'status' is missing"))
	}

	return errs
}

func (svc *Service) Initialize() error {
	messenger := svc.Deps.MustGetMessenger()
	return messenger.Subscribe(svc.Deps.MustGetContext(), svc.Cfg.Name, svc.Cfg.Subs["metrics"].Name, svc.Cfg.Type, svc.Cfg.Name, svc.Cfg.Subs["metrics"].MaxAge, svc.messageHandler)
}

func (svc *Service) messageHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()

	logger.Info("metricsMonitor: received message", "metricName", msg.MetricName, "metricValue", msg.Metric)

	lastStatus := svc.lastStatus[msg.MetricName]
	if lastStatus == "" {
		lastStatus = "ok"
	}

	currentStatus := "ok"
	var triggeredThreshold *Threshold

	matchedMetricName := false

	// Find the "highest" status triggered.
	// Order of severity: critical > warning > ok
	for _, t := range svc.Thresholds {
		if t.MetricName != "" && msg.MetricName != t.MetricName {
			continue
		}
		matchedMetricName = true

		triggered := false
		if t.Condition == "above" {
			if msg.Metric > t.Value {
				triggered = true
			} else if t.RecoveryThreshold != nil && lastStatus != "ok" && msg.Metric > *t.RecoveryThreshold {
				// We haven't recovered yet
				triggered = true
			}
		} else if t.Condition == "below" {
			if msg.Metric < t.Value {
				triggered = true
			} else if t.RecoveryThreshold != nil && lastStatus != "ok" && msg.Metric < *t.RecoveryThreshold {
				// We haven't recovered yet
				triggered = true
			}
		}

		if triggered {
			if t.Status == "critical" {
				currentStatus = "critical"
				triggeredThreshold = &t
				break // Highest found
			} else if t.Status == "warning" && currentStatus != "critical" {
				currentStatus = "warning"
				triggeredThreshold = &t
			}
		}
	}

	if !matchedMetricName {
		logger.Warn("metricsMonitor: no thresholds matched for metric", "metricName", msg.MetricName)
		return nil
	}

	if currentStatus != lastStatus {
		logger.Info("Status changed", "metric", msg.MetricName, "old", lastStatus, "new", currentStatus)
		svc.lastStatus[msg.MetricName] = currentStatus
	}

	// Set the status on the message and publish to status channel
	msg.Status = currentStatus
	if triggeredThreshold != nil {
		msg.Text = fmt.Sprintf("%s: %0.2f", msg.MetricName, msg.Metric)
		msg.Summary = fmt.Sprintf("%s: %s", msg.Status, msg.Summary)
		msg.Data = map[string]interface{}{
			"threshold": *triggeredThreshold,
		}
	}

	messenger := svc.Deps.MustGetMessenger()
	statusChan, ok := svc.Cfg.Pubs["status"]
	if !ok {
		return fmt.Errorf("metricsMonitor: status publication not configured")
	}

	msg.ChannelName = statusChan.Name
	return messenger.Send(msg)
}

func (svc *Service) Check() error {
	return nil
}
