package condition

import (
	"encoding/json"
	"fmt"
	"keyop/core"
	"keyop/util"
	"strings"
)

type ConditionConfig struct {
	Field    string         `json:"field"`
	Operator string         `json:"operator"` // "lt", "gt", "eq", "contains"
	Value    interface{}    `json:"value"`
	Updates  map[string]any `json:"updates"`
}

type Service struct {
	Deps       core.Dependencies
	Cfg        core.ServiceConfig
	Conditions []ConditionConfig
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps: deps,
		Cfg:  cfg,
	}

	if condsRaw, ok := cfg.Config["conditions"].([]interface{}); ok {
		for _, cRaw := range condsRaw {
			if cMap, ok := cRaw.(map[string]interface{}); ok {
				c := ConditionConfig{}
				if v, ok := cMap["field"].(string); ok {
					c.Field = v
				}
				if v, ok := cMap["operator"].(string); ok {
					c.Operator = v
				}
				if v, ok := cMap["value"]; ok {
					c.Value = v
				}
				if updates, ok := cMap["updates"].(map[string]any); ok {
					c.Updates = updates
				} else if updatesRaw, ok := cMap["updates"].(map[string]interface{}); ok {
					c.Updates = updatesRaw
				}
				svc.Conditions = append(svc.Conditions, c)
			}
		}
	}

	return svc
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("subs", svc.Cfg.Subs, []string{"source"}, logger)

	condsRaw, ok := svc.Cfg.Config["conditions"].([]interface{})
	if !ok || len(condsRaw) == 0 {
		errs = append(errs, fmt.Errorf("condition: 'conditions' must be a non-empty array"))
		return errs
	}

	validOperators := map[string]bool{"lt": true, "gt": true, "eq": true, "contains": true}

	for i, cRaw := range condsRaw {
		cMap, ok := cRaw.(map[string]interface{})
		if !ok {
			errs = append(errs, fmt.Errorf("condition: condition %d is not a map", i))
			continue
		}

		field, _ := cMap["field"].(string)
		if field == "" {
			errs = append(errs, fmt.Errorf("condition: condition %d 'field' is required", i))
		}

		op, _ := cMap["operator"].(string)
		if !validOperators[op] {
			errs = append(errs, fmt.Errorf("condition: condition %d 'operator' must be one of: lt, gt, eq, contains", i))
		}

		if _, ok := cMap["value"]; !ok {
			errs = append(errs, fmt.Errorf("condition: condition %d 'value' is required", i))
		}

		if updates, ok := cMap["updates"]; ok {
			if _, ok := updates.(map[string]interface{}); !ok {
				errs = append(errs, fmt.Errorf("condition: condition %d 'updates' must be a map", i))
			}
		}
	}

	// Always require 'target' pub channel
	if _, ok := svc.Cfg.Pubs["target"]; !ok {
		errs = append(errs, fmt.Errorf("condition: required pubs channel 'target' is missing"))
	}

	return errs
}

func (svc *Service) Initialize() error {
	messenger := svc.Deps.MustGetMessenger()
	return messenger.Subscribe(svc.Deps.MustGetContext(), svc.Cfg.Name, svc.Cfg.Subs["source"].Name, svc.Cfg.Type, svc.Cfg.Name, svc.Cfg.Subs["source"].MaxAge, svc.messageHandler)
}

func (svc *Service) messageHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	// Convert message to map for generic access
	msgMap := make(map[string]any)
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	if err := json.Unmarshal(data, &msgMap); err != nil {
		return fmt.Errorf("failed to unmarshal message: %w", err)
	}

	updatedMsgMap := make(map[string]any)
	// Start with a copy of the original message map
	for k, v := range msgMap {
		updatedMsgMap[k] = v
	}

	for _, cond := range svc.Conditions {
		if svc.evaluateCondition(updatedMsgMap, cond) {
			logger.Debug("Condition matched", "field", cond.Field, "operator", cond.Operator, "value", cond.Value)

			// Apply updates
			for k, v := range cond.Updates {
				updatedMsgMap[k] = v
			}

			// Always publish to 'target'
			if pubChan, ok := svc.Cfg.Pubs["target"]; ok {
				newMsg, err := svc.mapToMessage(updatedMsgMap)
				if err != nil {
					logger.Error("Failed to convert map to message for publishing", "error", err)
					continue
				}
				newMsg.ChannelName = pubChan.Name
				if err := messenger.Send(newMsg); err != nil {
					logger.Error("Failed to send updated message", "error", err)
				}
			}
		}
	}

	return nil
}

func (svc *Service) evaluateCondition(msgMap map[string]any, cond ConditionConfig) bool {
	val, ok := msgMap[cond.Field]
	if !ok {
		// Try case-insensitive or common variants? No, let's stick to JSON tags (which are usually camelCase)
		return false
	}

	switch cond.Operator {
	case "eq":
		return fmt.Sprintf("%v", val) == fmt.Sprintf("%v", cond.Value)
	case "contains":
		sVal := fmt.Sprintf("%v", val)
		sTarget := fmt.Sprintf("%v", cond.Value)
		return strings.Contains(sVal, sTarget)
	case "lt", "gt":
		fVal, ok1 := svc.toFloat(val)
		fTarget, ok2 := svc.toFloat(cond.Value)
		if !ok1 || !ok2 {
			return false
		}
		if cond.Operator == "lt" {
			return fVal < fTarget
		}
		return fVal > fTarget
	}

	return false
}

func (svc *Service) toFloat(v any) (float64, bool) {
	switch i := v.(type) {
	case float64:
		return i, true
	case float32:
		return float64(i), true
	case int:
		return float64(i), true
	case int64:
		return float64(i), true
	default:
		return 0, false
	}
}

func (svc *Service) mapToMessage(m map[string]any) (core.Message, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return core.Message{}, err
	}
	var msg core.Message
	err = json.Unmarshal(data, &msg)
	return msg, err
}

func (svc *Service) Check() error {
	return nil
}
