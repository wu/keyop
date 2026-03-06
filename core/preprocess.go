// Package core implements the core service for keyop and provides ValidateConfig, Initialize and Check hooks.
package core

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ConditionConfig defines a single conditional rule used in sub_preprocess and pub_preprocess.
//
// A condition matches a named field in the message using the given operator and value.
// When matched, the Updates map is merged into the message fields.
//
// Operator reference:
//
//	eq / ne         — string equality / inequality
//	== / !=         — numeric equality / inequality  (both sides coerced to float64)
//	> / >= / < / <= — numeric comparisons
//	contains        — field string value contains the substring
//	matches         — field string value matches the /regexp/ literal
type ConditionConfig struct {
	Field    string         `json:"field"`
	Operator string         `json:"operator"`
	Value    interface{}    `json:"value"`
	Updates  map[string]any `json:"updates"`
}

// ParseConditions parses a raw []interface{} (as decoded from YAML/JSON config) into a
// slice of ConditionConfig.  Each element may be one of two shapes:
//
//  1. A plain string — compact condition expression, e.g. `status eq "warn"`.
//     No updates; useful for sub_preprocess filtering.
//
//  2. A map with a "when" key — compact expression plus optional updates:
//     { when: 'metric > 90', updates: { channelName: alerts } }
func ParseConditions(raw []interface{}) []ConditionConfig {
	var conditions []ConditionConfig
	for _, cRaw := range raw {
		switch v := cRaw.(type) {

		case string:
			// Shape 1 — plain condition string, no updates.
			c, err := ParseConditionString(v)
			if err != nil {
				continue
			}
			conditions = append(conditions, c)

		case map[string]interface{}:
			// Shape 2 — { when: "...", updates: {...} }
			whenStr, ok := v["when"].(string)
			if !ok {
				continue
			}
			c, err := ParseConditionString(whenStr)
			if err != nil {
				continue
			}
			if updates, ok := v["updates"].(map[string]interface{}); ok {
				c.Updates = updates
			}
			conditions = append(conditions, c)
		}
	}
	return conditions
}

// ValidConditionOperators is the complete set of recognised operator strings.
var ValidConditionOperators = map[string]bool{
	"eq": true, "ne": true,
	"==": true, "!=": true,
	">": true, ">=": true, "<": true, "<=": true,
	"contains": true,
	"matches":  true,
}

// ValidateConditions validates a raw conditions slice decoded from config and returns any errors found.
func ValidateConditions(key string, raw []interface{}) []error {
	var errs []error
	for i, cRaw := range raw {
		switch v := cRaw.(type) {
		case string:
			if _, err := ParseConditionString(v); err != nil {
				errs = append(errs, fmt.Errorf("%s: condition %d: %w", key, i, err))
			}
		case map[string]interface{}:
			whenStr, ok := v["when"].(string)
			if !ok {
				errs = append(errs, fmt.Errorf("%s: condition %d: map must have a 'when' string key", key, i))
				continue
			}
			if _, err := ParseConditionString(whenStr); err != nil {
				errs = append(errs, fmt.Errorf("%s: condition %d 'when': %w", key, i, err))
			}
			if updates, ok := v["updates"]; ok {
				if _, ok := updates.(map[string]interface{}); !ok {
					errs = append(errs, fmt.Errorf("%s: condition %d 'updates' must be a map", key, i))
				}
			}
		default:
			errs = append(errs, fmt.Errorf("%s: condition %d is not a string or map", key, i))
		}
	}
	return errs
}

// ApplyConditions evaluates each condition against msg. For every condition that matches,
// its Updates are merged into the message fields and the result is appended to the returned
// slice. If no conditions match, an empty slice is returned.
//
// Used by pub_preprocess: each matching condition yields one outgoing message.
func ApplyConditions(msg Message, conditions []ConditionConfig) []Message {
	var results []Message

	msgMap, err := messageToMap(msg)
	if err != nil {
		return results
	}

	workingMap := copyMap(msgMap)

	for _, cond := range conditions {
		if evaluateCondition(workingMap, cond) {
			for k, v := range cond.Updates {
				workingMap[k] = v
			}
			if newMsg, err := mapToMessage(workingMap); err == nil {
				results = append(results, newMsg)
			}
		}
	}

	return results
}

// ApplySubPreprocess evaluates conditions against an incoming message and returns the
// (potentially modified) message and true if processing should continue, or the zero
// Message and false if the message should be dropped.
//
// When conditions is empty the original message is returned unchanged (pass-through).
// When conditions are present: all matching conditions' Updates are merged (last-write-wins)
// into a single message and returned. If no condition matches the message is dropped.
func ApplySubPreprocess(msg Message, conditions []ConditionConfig) (Message, bool) {
	if len(conditions) == 0 {
		return msg, true
	}

	msgMap, err := messageToMap(msg)
	if err != nil {
		return msg, true
	}

	workingMap := copyMap(msgMap)
	matched := false

	for _, cond := range conditions {
		if evaluateCondition(workingMap, cond) {
			matched = true
			for k, v := range cond.Updates {
				workingMap[k] = v
			}
		}
	}

	if !matched {
		return Message{}, false
	}

	newMsg, err := mapToMessage(workingMap)
	if err != nil {
		return msg, true
	}
	return newMsg, true
}

// evaluateCondition tests whether a single ConditionConfig matches the given message map.
func evaluateCondition(msgMap map[string]any, cond ConditionConfig) bool {
	val, ok := msgMap[cond.Field]
	if !ok {
		return false
	}

	switch cond.Operator {
	case "eq":
		return fmt.Sprintf("%v", val) == fmt.Sprintf("%v", cond.Value)
	case "ne":
		return fmt.Sprintf("%v", val) != fmt.Sprintf("%v", cond.Value)
	case "contains":
		return strings.Contains(fmt.Sprintf("%v", val), fmt.Sprintf("%v", cond.Value))
	case "matches":
		pattern := fmt.Sprintf("%v", cond.Value)
		if strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/") && len(pattern) >= 2 {
			pattern = pattern[1 : len(pattern)-1]
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return false
		}
		return re.MatchString(fmt.Sprintf("%v", val))
	case "==":
		fVal, ok1 := toFloat(val)
		fTarget, ok2 := toFloat(cond.Value)
		if !ok1 || !ok2 {
			return false
		}
		return fVal == fTarget
	case "!=":
		fVal, ok1 := toFloat(val)
		fTarget, ok2 := toFloat(cond.Value)
		if !ok1 || !ok2 {
			return false
		}
		return fVal != fTarget
	case ">":
		fVal, ok1 := toFloat(val)
		fTarget, ok2 := toFloat(cond.Value)
		if !ok1 || !ok2 {
			return false
		}
		return fVal > fTarget
	case "<":
		fVal, ok1 := toFloat(val)
		fTarget, ok2 := toFloat(cond.Value)
		if !ok1 || !ok2 {
			return false
		}
		return fVal < fTarget
	case ">=":
		fVal, ok1 := toFloat(val)
		fTarget, ok2 := toFloat(cond.Value)
		if !ok1 || !ok2 {
			return false
		}
		return fVal >= fTarget
	case "<=":
		fVal, ok1 := toFloat(val)
		fTarget, ok2 := toFloat(cond.Value)
		if !ok1 || !ok2 {
			return false
		}
		return fVal <= fTarget
	}

	return false
}

// toFloat coerces a value to float64, supporting numeric types and numeric strings.
func toFloat(v any) (float64, bool) {
	switch i := v.(type) {
	case float64:
		return i, true
	case float32:
		return float64(i), true
	case int:
		return float64(i), true
	case int64:
		return float64(i), true
	case string:
		f, err := strconv.ParseFloat(i, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func messageToMap(msg Message) (map[string]any, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func mapToMessage(m map[string]any) (Message, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return Message{}, err
	}
	var msg Message
	err = json.Unmarshal(data, &msg)
	return msg, err
}

func copyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
