// Package core implements the core service for keyop and provides ValidateConfig, Initialize and Check hooks.
package core

import (
	"fmt"
	"reflect"
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
	if len(conditions) == 0 {
		return results
	}

	working := msg
	for _, cond := range conditions {
		if evaluateConditionReflect(working, cond) {
			_ = applyUpdatesToMessage(&working, cond.Updates)
			results = append(results, working)
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

	working := msg
	matched := false
	for _, cond := range conditions {
		if evaluateConditionReflect(working, cond) {
			matched = true
			_ = applyUpdatesToMessage(&working, cond.Updates)
		}
	}
	if !matched {
		return Message{}, false
	}
	return working, true
}

// evaluateConditionReflect evaluates a ConditionConfig against the provided message
// using reflection to read message fields (including nested fields inside Data).
func evaluateConditionReflect(msg Message, cond ConditionConfig) bool {
	val, ok := getValueByJSONPath(msg, cond.Field)
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

// getValueByJSONPath reads a value from the message using a dot-separated path where
// segments correspond to JSON field names (e.g. "data.level" or "metricName").
func getValueByJSONPath(root interface{}, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	segments := strings.Split(path, ".")
	cur := reflect.ValueOf(root)
	for _, seg := range segments {
		// dereference pointers/interfaces
		for cur.Kind() == reflect.Ptr || cur.Kind() == reflect.Interface {
			if cur.IsNil() {
				return nil, false
			}
			cur = cur.Elem()
		}

		switch cur.Kind() {
		case reflect.Struct:
			f, ok := findFieldByJSONTag(cur, seg)
			if !ok {
				return nil, false
			}
			cur = f
		case reflect.Map:
			if cur.Type().Key().Kind() != reflect.String {
				return nil, false
			}
			mv := cur.MapIndex(reflect.ValueOf(seg))
			if !mv.IsValid() {
				return nil, false
			}
			cur = mv
		default:
			return nil, false
		}
	}

	// final dereference
	for cur.Kind() == reflect.Ptr || cur.Kind() == reflect.Interface {
		if cur.IsNil() {
			return nil, false
		}
		cur = cur.Elem()
	}
	if !cur.IsValid() {
		return nil, false
	}
	return cur.Interface(), true
}

// findFieldByJSONTag locates a struct field by its json tag (first segment before a comma)
// or by common fallbacks (lower-cased field name).
func findFieldByJSONTag(rv reflect.Value, name string) (reflect.Value, bool) {
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		ft := rt.Field(i)
		// skip unexported fields
		if ft.PkgPath != "" {
			continue
		}
		tag := ft.Tag.Get("json")
		tagName := strings.Split(tag, ",")[0]
		if tagName == "-" {
			continue
		}
		if tagName == "" {
			// fallback: lower-first field name
			tagName = lowerFirst(ft.Name)
		}
		if tagName == name || ft.Name == name {
			return rv.Field(i), true
		}
	}
	return reflect.Value{}, false
}

// applyUpdatesToMessage applies the updates map onto the provided Message using reflection.
// It supports top-level fields, setting Data and DataType, and single-segment nested
// updates under Data (e.g. "data.level").
func applyUpdatesToMessage(msg *Message, updates map[string]any) error {
	if updates == nil {
		return nil
	}
	rv := reflect.ValueOf(msg).Elem()
	for k, v := range updates {
		if k == "data" {
			msg.Data = v
			continue
		}
		if k == "data-type" || k == "dataType" {
			msg.DataType = fmt.Sprintf("%v", v)
			continue
		}
		if strings.HasPrefix(k, "data.") {
			sub := strings.TrimPrefix(k, "data.")
			setNestedDataField(msg, sub, v)
			continue
		}
		// top-level message fields
		field, ok := findFieldByJSONTag(rv, k)
		if !ok {
			// unknown field — skip
			continue
		}
		setReflectValue(field, v)
	}
	return nil
}

// setNestedDataField sets a shallow field inside msg.Data while preserving typed payloads
// where possible. If Data is nil or not addressable, it is replaced with a map[string]any.
func setNestedDataField(msg *Message, key string, value any) {
	if msg.Data == nil {
		m := map[string]any{key: value}
		msg.Data = m
		return
	}

	orig := reflect.ValueOf(msg.Data)
	isPtr := orig.Kind() == reflect.Ptr
	// dereference
	for orig.Kind() == reflect.Interface || orig.Kind() == reflect.Ptr {
		if orig.IsNil() {
			m := map[string]any{key: value}
			msg.Data = m
			return
		}
		orig = orig.Elem()
	}

	switch orig.Kind() {
	case reflect.Map:
		// prefer map[string]any for easy mutation
		if m, ok := msg.Data.(map[string]any); ok {
			m[key] = value
			msg.Data = m
			return
		}
		// otherwise construct a new map of same type
		newMap := reflect.MakeMap(orig.Type())
		for _, mapKey := range orig.MapKeys() {
			newMap.SetMapIndex(mapKey, orig.MapIndex(mapKey))
		}
		newMap.SetMapIndex(reflect.ValueOf(key), reflect.ValueOf(value))
		msg.Data = newMap.Interface()
		return
	case reflect.Struct:
		// create a new addressable copy so we can set fields
		newPtr := reflect.New(orig.Type())
		newPtr.Elem().Set(orig)
		f, ok := findFieldByJSONTag(newPtr.Elem(), key)
		if ok && f.CanSet() {
			setReflectValue(f, value)
			// restore pointerness if original was a pointer
			if isPtr {
				msg.Data = newPtr.Interface()
			} else {
				msg.Data = newPtr.Elem().Interface()
			}
			return
		}
		// fallback to map
		m := map[string]any{key: value}
		msg.Data = m
		return
	default:
		// cannot set nested field on primitive types — replace with a map
		m := map[string]any{key: value}
		msg.Data = m
		return
	}
}

// setReflectValue attempts to set field to value converting simple scalar types and
// populating structs from map[string]any when needed.
func setReflectValue(field reflect.Value, value any) {
	if !field.CanSet() {
		return
	}
	v := reflect.ValueOf(value)
	// dereference interfaces/pointers on incoming value for convenience
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Ptr {
		if v.IsNil() {
			// set zero value
			field.Set(reflect.Zero(field.Type()))
			return
		}
		v = v.Elem()
	}

	switch field.Kind() {
	case reflect.String:
		field.SetString(fmt.Sprintf("%v", value))
	case reflect.Float32, reflect.Float64:
		if f, ok := toFloat(value); ok {
			field.SetFloat(f)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if f, ok := toFloat(value); ok {
			field.SetInt(int64(f))
		}
	case reflect.Bool:
		if b, ok := value.(bool); ok {
			field.SetBool(b)
		} else if s, ok := value.(string); ok {
			if s == "true" || s == "1" {
				field.SetBool(true)
			} else {
				field.SetBool(false)
			}
		}
	case reflect.Interface:
		field.Set(v)
	case reflect.Struct:
		// If incoming is a map[string]any, try to populate struct fields
		if m, ok := value.(map[string]any); ok {
			// create new struct and populate
			newVal := reflect.New(field.Type()).Elem()
			for mk, mv := range m {
				if f, ok := findFieldByJSONTag(newVal, mk); ok && f.CanSet() {
					setReflectValue(f, mv)
				}
			}
			field.Set(newVal)
			return
		}
		// if types are assignable, set directly
		if v.Type().AssignableTo(field.Type()) {
			field.Set(v)
		}
	case reflect.Ptr:
		// allocate a new value and set
		eleType := field.Type().Elem()
		newPtr := reflect.New(eleType)
		if v.Type().AssignableTo(eleType) {
			newPtr.Elem().Set(v)
			field.Set(newPtr)
			return
		}
		if m, ok := value.(map[string]any); ok && eleType.Kind() == reflect.Struct {
			for mk, mv := range m {
				if f, ok := findFieldByJSONTag(newPtr.Elem(), mk); ok && f.CanSet() {
					setReflectValue(f, mv)
				}
			}
			field.Set(newPtr)
		}
	default:
		// try direct conversion if possible
		if v.Type().ConvertibleTo(field.Type()) {
			field.Set(v.Convert(field.Type()))
		}
	}
}

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

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
