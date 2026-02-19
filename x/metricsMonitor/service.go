package metricsMonitor

import (
	"encoding/json"
	"fmt"
	"keyop/core"
	"keyop/util"
)

type Threshold struct {
	MetricName        string         `json:"metricName"`
	Value             float64        `json:"value"`
	RecoveryThreshold *float64       `json:"recoveryThreshold,omitempty"`
	Condition         string         `json:"condition"` // "above" or "below"
	Status            string         `json:"status"`
	Updates           map[string]any `json:"updates"`
	Hostname          string         `json:"hostname,omitempty"`
	ServiceName       string         `json:"serviceName,omitempty"`
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
				if updates, ok := tMap["updates"].(map[string]any); ok {
					t.Updates = updates
				} else if updatesRaw, ok := tMap["updates"].(map[string]interface{}); ok {
					t.Updates = updatesRaw
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

				status, _ := tMap["status"].(string)
				updates, _ := tMap["updates"].(map[string]interface{})

				if status == "" && updates == nil {
					errs = append(errs, fmt.Errorf("metricsMonitor: threshold %d must have either 'status' or 'updates'", i))
				}
				if status != "" && updates != nil {
					errs = append(errs, fmt.Errorf("metricsMonitor: threshold %d cannot have both 'status' and 'updates'", i))
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
	messenger := svc.Deps.MustGetMessenger()

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
			} else if t.Updates != nil {
				// If it's an updates threshold, we check if it specifies a status
				if status, ok := t.Updates["status"].(string); ok {
					if status == "critical" {
						currentStatus = "critical"
						triggeredThreshold = &t
						break
					} else if status == "warning" && currentStatus != "critical" {
						currentStatus = "warning"
						triggeredThreshold = &t
					} else if currentStatus == "ok" {
						// Custom status or no priority, but still triggered
						currentStatus = status
						triggeredThreshold = &t
					}
				} else {
					// No status in updates, just use it if nothing else is triggered
					if triggeredThreshold == nil {
						triggeredThreshold = &t
					}
				}
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

	newMessage := core.Message{
		Correlation: msg.Correlation,
		ChannelName: svc.Cfg.Pubs["status"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		MetricName:  msg.MetricName,
		Metric:      msg.Metric,
		Status:      currentStatus,
		Text:        msg.Text,
		Summary:     msg.Summary,
		Data:        msg.Data,
	}

	// Set the status on the message and publish to status channel
	if triggeredThreshold != nil {
		if triggeredThreshold.Status != "" {
			newMessage.Text = fmt.Sprintf("%s: %0.2f", msg.MetricName, msg.Metric)
			newMessage.Summary = fmt.Sprintf("%s: %s", msg.Status, msg.Summary)
			newMessage.Data = map[string]interface{}{
				"threshold": *triggeredThreshold,
			}
		} else if triggeredThreshold.Updates != nil {
			// Apply updates to the message
			// Convert message to map for generic access, similar to condition service
			msgMap := make(map[string]any)
			data, err := json.Marshal(newMessage)
			if err != nil {
				return fmt.Errorf("failed to marshal message: %w", err)
			}
			if err := json.Unmarshal(data, &msgMap); err != nil {
				return fmt.Errorf("failed to unmarshal message: %w", err)
			}

			for k, v := range triggeredThreshold.Updates {
				msgMap[k] = v
			}

			// Convert back to message
			data, err = json.Marshal(msgMap)
			if err != nil {
				return fmt.Errorf("failed to marshal updated message map: %w", err)
			}
			if err := json.Unmarshal(data, &newMessage); err != nil {
				return fmt.Errorf("failed to unmarshal updated message: %w", err)
			}
		}
	}

	return messenger.Send(newMessage)
}

func (svc *Service) Check() error {
	return nil
}
