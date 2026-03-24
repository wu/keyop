// Package metricmon evaluates numeric metrics against configured thresholds and publishes threshold status events.
//
// It parses threshold definitions from the service configuration, listens for metric messages on configured
// subscriptions, and emits threshold_status messages when thresholds are crossed or recovered.
package metricmon

import (
	"encoding/json"
	"fmt"
	"keyop/core"
	"keyop/util"
)

// Threshold represents a configured metric threshold and associated actions.
//
// MetricName, Value, Condition and optional RecoveryThreshold determine when the threshold
// is triggered. Updates contains any message updates to apply when the threshold triggers.
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

// Service monitors incoming metric messages and evaluates them against configured thresholds.
// When thresholds trigger, Service publishes threshold status updates to the message bus.
type Service struct {
	Deps       core.Dependencies
	Cfg        core.ServiceConfig
	Thresholds []Threshold
	lastStatus map[string]string
}

// NewService creates a new service using the provided dependencies and configuration.
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

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	var errs []error
	if len(svc.Cfg.Subs) == 0 {
		errs = append(errs, fmt.Errorf("metricsMonitor service requires at least one subscription in 'subs'"))
		return errs
	}

	logger := svc.Deps.MustGetLogger()
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

	return errs
}

// Initialize performs one-time startup required by the service (resource loading or connectivity checks).
func (svc *Service) Initialize() error {
	m := svc.Deps.MustGetMessenger()
	ctx := svc.Deps.MustGetContext()
	for _, subInfo := range svc.Cfg.Subs {
		if subInfo.Name == "" {
			return fmt.Errorf("metricsMonitor: subscription entry missing 'Name'")
		}
		if err := m.Subscribe(ctx, svc.Cfg.Name, subInfo.Name, svc.Cfg.Type, svc.Cfg.Name, subInfo.MaxAge, svc.messageHandler); err != nil {
			return fmt.Errorf("metricsMonitor: failed to subscribe to %s: %w", subInfo.Name, err)
		}
	}
	return nil
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
		switch t.Condition {
		case "above":
			if msg.Metric > t.Value {
				triggered = true
			} else if t.RecoveryThreshold != nil && lastStatus != "ok" && msg.Metric > *t.RecoveryThreshold {
				// We haven't recovered yet
				triggered = true
			}
		case "below":
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
			} else if t.Status == "warning" {
				currentStatus = "warning"
				triggeredThreshold = &t
			} else if t.Updates != nil {
				// If it's an updates threshold, we check if it specifies a status
				if status, ok := t.Updates["status"].(string); ok {
					if status == "critical" {
						currentStatus = "critical"
						triggeredThreshold = &t
						break
					} else if status == "warning" {
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
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "threshold_status",
		MetricName:  msg.MetricName,
		Metric:      msg.Metric,
		Status:      currentStatus,
		Text:        msg.Text,
		Summary:     msg.Summary,
		Data: &core.StatusEvent{
			Name:    msg.MetricName,
			Status:  currentStatus,
			Level:   currentStatus,
			Details: fmt.Sprintf("%s: %0.2f", msg.MetricName, msg.Metric),
		},
	}

	// Set the status on the message and publish to status channel
	if triggeredThreshold != nil {
		if triggeredThreshold.Status != "" {
			newMessage.Text = fmt.Sprintf("%s: %0.2f", msg.MetricName, msg.Metric)
			newMessage.Summary = fmt.Sprintf("%s: %s", msg.Status, msg.Summary)
			if se, ok := newMessage.Data.(*core.StatusEvent); ok {
				se.Details = fmt.Sprintf("%s: %0.2f", msg.MetricName, msg.Metric)
			}
		} else if triggeredThreshold.Updates != nil {
			// Save the StatusEvent before the JSON round-trip
			statusEvent := newMessage.Data.(*core.StatusEvent)

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

			// Restore the StatusEvent after JSON round-trip
			newMessage.Data = statusEvent
		}
	}

	return messenger.Send(newMessage)
}

// Check performs the service's periodic work: collect data, evaluate state, and publish messages/metrics.
func (svc *Service) Check() error {
	return nil
}
