package core

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// Config rules match messages as they are published or received and overwrite
// fields on the payload. They exist so that delivery decisions -- which alerts are
// spoken, texted, or shown persistently -- can be expressed in the YAML of either
// the producing or the consuming service, instead of being hard-coded in Go by
// whichever service happens to build the event.
//
// Rules mutate only. They cannot drop a message, and because they operate on the
// payload rather than the envelope they cannot change its channel: routing stays
// the sole responsibility of the typed pubs map.
//
// The engine here is pure. Parsing happens in ParseRules, validation in
// ValidateRules, and application in ApplyRules; nothing in this file touches the
// messenger.

// Condition operators. eq and in compare values; contains and matches operate on
// the string form of a field; gt/gte/lt/lte are numeric.
const (
	OpEq       = "eq"
	OpIn       = "in"
	OpContains = "contains"
	OpMatches  = "matches"
	OpGt       = "gt"
	OpGte      = "gte"
	OpLt       = "lt"
	OpLte      = "lte"
)

// Condition is a single test against one field of a payload.
type Condition struct {
	// Op is one of the Op* constants.
	Op string
	// Value holds the comparison operand for every operator except OpIn.
	Value any
	// Values holds the candidates for OpIn.
	Values []any

	// re is the compiled pattern for OpMatches, built once at parse time.
	re *regexp.Regexp
}

// Rule is one matching-and-mutating rule from a service's pub_rules or sub_rules.
//
// Set writes a field only when it is still unset, so a rule supplies a default
// without overriding an opinion the sender already expressed. Force writes
// unconditionally. The split is what lets producer-side and consumer-side rules
// compose: the consumer fills gaps by default, and says so explicitly when it
// needs to win.
type Rule struct {
	// PayloadType is the payload the rule applies to, e.g. "core.alert.v1". It is
	// required: it makes the field paths checkable at startup, keeps evaluation
	// cheap, and stops a rule matching an unrelated payload that happens to have a
	// field of the same name.
	PayloadType string
	// When holds the conditions, keyed by dotted field path. All must match.
	When map[string]Condition
	// Set writes each value at its dotted field path, but only where the field is
	// currently the zero value.
	Set map[string]any
	// Force writes each value at its dotted field path unconditionally.
	Force map[string]any
}

// RuleSpec is the decoded YAML form of a Rule, before parsing. Condition values
// are left as `any` because they are heterogeneous: a scalar means equality, a
// list means any-of, and a single-key map names an operator.
type RuleSpec struct {
	Type  string         `yaml:"type" json:"type"`
	When  map[string]any `yaml:"when" json:"when"`
	Set   map[string]any `yaml:"set" json:"set"`
	Force map[string]any `yaml:"force" json:"force"`
}

// RuleChange records one field a rule actually altered. The messenger wrapper logs
// these: with rules spread across several config files on several hosts, "why did
// this alert get spoken?" is otherwise unanswerable.
type RuleChange struct {
	RuleIndex int
	Path      string
	From      any
	To        any
	Forced    bool
}

// PrototypeLookup resolves a payload type string to the Go type registered for it.
// ValidateRules uses it to check field paths before any message is processed.
type PrototypeLookup func(payloadType string) (reflect.Type, bool)

// ParseCondition converts one decoded YAML condition value into a Condition.
//
// Accepted forms:
//
//	level: critical               -> eq
//	level: [warning, critical]    -> in
//	summary: {contains: "disk"}   -> contains
//	summary: {matches: "^ssl "}   -> matches
//	value:   {gt: 90}             -> gt / gte / lt / lte
func ParseCondition(raw any) (Condition, error) {
	switch v := raw.(type) {
	case nil:
		return Condition{}, fmt.Errorf("condition value must not be empty")

	case []any:
		if len(v) == 0 {
			return Condition{}, fmt.Errorf("list of candidates must not be empty")
		}
		return Condition{Op: OpIn, Values: v}, nil

	default:
		// A single-key map names an operator; anything else is an equality test.
		if m, ok := normalizeMap(raw); ok {
			if len(m) != 1 {
				return Condition{}, fmt.Errorf("operator map must have exactly one key, got %d", len(m))
			}
			for op, operand := range m {
				return newOperatorCondition(op, operand)
			}
		}
		return Condition{Op: OpEq, Value: raw}, nil
	}
}

func newOperatorCondition(op string, operand any) (Condition, error) {
	switch op {
	case OpEq, OpContains:
		return Condition{Op: op, Value: operand}, nil

	case OpIn:
		list, ok := operand.([]any)
		if !ok {
			return Condition{}, fmt.Errorf("operator %q needs a list, got %T", op, operand)
		}
		if len(list) == 0 {
			return Condition{}, fmt.Errorf("operator %q needs a non-empty list", op)
		}
		return Condition{Op: OpIn, Values: list}, nil

	case OpMatches:
		pattern, ok := operand.(string)
		if !ok {
			return Condition{}, fmt.Errorf("operator %q needs a string pattern, got %T", op, operand)
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return Condition{}, fmt.Errorf("operator %q: invalid pattern %q: %w", op, pattern, err)
		}
		return Condition{Op: OpMatches, Value: pattern, re: re}, nil

	case OpGt, OpGte, OpLt, OpLte:
		if _, ok := toFloat(operand); !ok {
			return Condition{}, fmt.Errorf("operator %q needs a number, got %T", op, operand)
		}
		return Condition{Op: op, Value: operand}, nil

	default:
		return Condition{}, fmt.Errorf("unknown operator %q (valid: %s)", op, strings.Join(validOperators(), ", "))
	}
}

func validOperators() []string {
	return []string{OpEq, OpIn, OpContains, OpMatches, OpGt, OpGte, OpLt, OpLte}
}

// ParseRules converts decoded YAML rule specs into Rules, returning every problem
// found rather than stopping at the first. A spec that fails to parse is omitted
// from the returned rules, so a caller that ignores the errors runs fewer rules
// rather than wrong ones -- but callers are expected to fail startup instead.
func ParseRules(key string, specs []RuleSpec) ([]Rule, []error) {
	var rules []Rule
	var errs []error

	for i, spec := range specs {
		rule := Rule{
			PayloadType: spec.Type,
			Set:         spec.Set,
			Force:       spec.Force,
		}

		failed := false
		if len(spec.When) > 0 {
			rule.When = make(map[string]Condition, len(spec.When))
			for _, path := range sortedKeys(spec.When) {
				cond, err := ParseCondition(spec.When[path])
				if err != nil {
					errs = append(errs, fmt.Errorf("%s: rule %d: when %q: %w", key, i, path, err))
					failed = true
					continue
				}
				rule.When[path] = cond
			}
		}
		if failed {
			continue
		}
		rules = append(rules, rule)
	}

	return rules, errs
}

// ValidateRules checks rules against the payload prototypes they declare and
// returns every problem found. A rule that names an unknown field, writes a value
// the field cannot hold, or changes nothing at all is a configuration error: the
// previous implementation of this feature treated all three as silent no-ops, so a
// typo became a rule that never fired and never complained.
//
// lookup may be nil, in which case type-specific checks are skipped and only the
// structural ones run.
func ValidateRules(key string, rules []Rule, lookup PrototypeLookup) []error {
	var errs []error

	for i, rule := range rules {
		prefix := fmt.Sprintf("%s: rule %d", key, i)

		if rule.PayloadType == "" {
			errs = append(errs, fmt.Errorf("%s: 'type' is required (e.g. type: core.alert.v1)", prefix))
			continue
		}
		if len(rule.Set) == 0 && len(rule.Force) == 0 {
			errs = append(errs, fmt.Errorf("%s: has no 'set' or 'force' and would change nothing", prefix))
		}
		for _, path := range sortedKeys(rule.Set) {
			if _, dup := rule.Force[path]; dup {
				errs = append(errs, fmt.Errorf("%s: %q appears in both 'set' and 'force'", prefix, path))
			}
		}

		if lookup == nil {
			continue
		}
		protoType, ok := lookup(rule.PayloadType)
		if !ok {
			errs = append(errs, fmt.Errorf("%s: unknown payload type %q; no service registers it", prefix, rule.PayloadType))
			continue
		}

		errs = append(errs, validateConditions(prefix, rule, protoType)...)
		errs = append(errs, validateWrites(prefix, rule, protoType)...)
	}

	return errs
}

// ValidateConditions checks the When clauses of match-only rules: rules used to
// select messages rather than to mutate them, which therefore carry no Set or
// Force. Everything else is as ValidateRules — an unknown payload type, or a field
// path that is not part of that payload, is a configuration error rather than
// something to discover on the first message that silently fails to match.
//
// The absent writes are the whole reason this exists apart from ValidateRules,
// which reports a rule that writes nothing as an error. That is right for
// pub_rules and sub_rules, whose only effect is the write, and wrong for a rule
// whose only job is to decide whether a message is interesting.
//
// A rule with no conditions at all is accepted: it selects every payload of its
// type, which is a legitimate thing to ask for.
//
// lookup may be nil, in which case the type-specific checks are skipped and only
// the structural ones run.
func ValidateConditions(key string, rules []Rule, lookup PrototypeLookup) []error {
	var errs []error

	for i, rule := range rules {
		prefix := fmt.Sprintf("%s: rule %d", key, i)

		if rule.PayloadType == "" {
			errs = append(errs, fmt.Errorf("%s: 'type' is required (e.g. type: core.alert.v1)", prefix))
			continue
		}

		if lookup == nil {
			continue
		}
		protoType, ok := lookup(rule.PayloadType)
		if !ok {
			errs = append(errs, fmt.Errorf("%s: unknown payload type %q; no service registers it", prefix, rule.PayloadType))
			continue
		}

		errs = append(errs, validateConditions(prefix, rule, protoType)...)
	}

	return errs
}

func validateConditions(prefix string, rule Rule, protoType reflect.Type) []error {
	var errs []error
	for _, path := range sortedKeys(rule.When) {
		cond := rule.When[path]
		fieldType, ok := resolveFieldType(protoType, path)
		if !ok {
			errs = append(errs, fmt.Errorf("%s: when: %q is not a field of %s", prefix, path, protoType))
			continue
		}
		switch cond.Op {
		case OpGt, OpGte, OpLt, OpLte:
			if !isNumericKind(baseType(fieldType).Kind()) {
				errs = append(errs, fmt.Errorf("%s: when: operator %q needs a numeric field, but %q is %s",
					prefix, cond.Op, path, fieldType))
			}
		case OpContains, OpMatches:
			if baseType(fieldType).Kind() != reflect.String {
				errs = append(errs, fmt.Errorf("%s: when: operator %q needs a string field, but %q is %s",
					prefix, cond.Op, path, fieldType))
			}
		}
	}
	return errs
}

// validateWrites checks each set/force path by performing the write against a
// throwaway zero value of the payload type. Using the same code as the runtime is
// the point: anything that would fail later fails here instead.
func validateWrites(prefix string, rule Rule, protoType reflect.Type) []error {
	var errs []error
	for _, which := range []struct {
		label  string
		writes map[string]any
	}{{"set", rule.Set}, {"force", rule.Force}} {
		for _, path := range sortedKeys(which.writes) {
			probe := reflect.New(protoType).Elem()
			if _, _, err := setPath(probe, path, which.writes[path], false); err != nil {
				errs = append(errs, fmt.Errorf("%s: %s: %q: %w", prefix, which.label, path, err))
			}
		}
	}
	return errs
}

// Match reports whether the rule applies to this payload. Every condition must
// match; a condition naming a field that is absent or nil does not match.
func (r Rule) Match(payloadType string, payload any) bool {
	if r.PayloadType != payloadType || payload == nil {
		return false
	}
	for path, cond := range r.When {
		value, ok := getPath(payload, path)
		if !ok || !cond.matches(value) {
			return false
		}
	}
	return true
}

func (c Condition) matches(value any) bool {
	switch c.Op {
	case OpEq:
		return valuesEqual(value, c.Value)
	case OpIn:
		for _, candidate := range c.Values {
			if valuesEqual(value, candidate) {
				return true
			}
		}
		return false
	case OpContains:
		return strings.Contains(fmt.Sprint(value), fmt.Sprint(c.Value))
	case OpMatches:
		return c.re != nil && c.re.MatchString(fmt.Sprint(value))
	case OpGt, OpGte, OpLt, OpLte:
		got, ok1 := toFloat(value)
		want, ok2 := toFloat(c.Value)
		if !ok1 || !ok2 {
			return false
		}
		switch c.Op {
		case OpGt:
			return got > want
		case OpGte:
			return got >= want
		case OpLt:
			return got < want
		default:
			return got <= want
		}
	}
	return false
}

// ApplyRules returns payload with every matching rule's writes applied, along with
// the changes made and any write that failed.
//
// The returned payload has the same concrete type as the input -- a value in gives
// a value out, a pointer in gives a new pointer out -- because subscribers type
// assert on it directly. The caller's data is never mutated: the payload is copied
// before any write, and pointers along a written path are cloned rather than
// followed.
//
// When nothing matches, or every matching rule leaves the payload unchanged, the
// original payload is returned as-is.
func ApplyRules(payloadType string, payload any, rules []Rule) (any, []RuleChange, []error) {
	if payload == nil || len(rules) == 0 {
		return payload, nil, nil
	}

	var matched []int
	for i, rule := range rules {
		if rule.Match(payloadType, payload) {
			matched = append(matched, i)
		}
	}
	if len(matched) == 0 {
		return payload, nil, nil
	}

	orig := reflect.ValueOf(payload)
	isPtr := orig.Kind() == reflect.Pointer
	if isPtr {
		if orig.IsNil() {
			return payload, nil, nil
		}
		orig = orig.Elem()
	}
	if orig.Kind() != reflect.Struct {
		return payload, nil, []error{fmt.Errorf("rules: payload %q is %s, not a struct", payloadType, orig.Kind())}
	}

	working := reflect.New(orig.Type())
	working.Elem().Set(orig)

	var changes []RuleChange
	var errs []error
	for _, i := range matched {
		rule := rules[i]
		for _, which := range []struct {
			writes map[string]any
			forced bool
		}{{rule.Set, false}, {rule.Force, true}} {
			for _, path := range sortedKeys(which.writes) {
				from, changed, err := setPath(working.Elem(), path, which.writes[path], !which.forced)
				if err != nil {
					errs = append(errs, fmt.Errorf("rule %d: %q: %w", i, path, err))
					continue
				}
				if changed {
					changes = append(changes, RuleChange{
						RuleIndex: i, Path: path, From: from, To: which.writes[path], Forced: which.forced,
					})
				}
			}
		}
	}

	if len(changes) == 0 {
		return payload, nil, errs
	}
	if isPtr {
		return working.Interface(), changes, errs
	}
	return working.Elem().Interface(), changes, errs
}

// getPath reads the value at a dotted field path. Pointers are followed, and a nil
// pointer anywhere along the path means the value is absent rather than zero.
func getPath(root any, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	cur := reflect.ValueOf(root)
	for _, seg := range strings.Split(path, ".") {
		var ok bool
		if cur, ok = deref(cur); !ok {
			return nil, false
		}
		switch cur.Kind() {
		case reflect.Struct:
			field, found := fieldByJSONTag(cur, seg)
			if !found {
				return nil, false
			}
			cur = field
		case reflect.Map:
			if cur.Type().Key().Kind() != reflect.String {
				return nil, false
			}
			entry := cur.MapIndex(reflect.ValueOf(seg))
			if !entry.IsValid() {
				return nil, false
			}
			cur = entry
		default:
			return nil, false
		}
	}
	final, ok := deref(cur)
	if !ok {
		return nil, false
	}
	return final.Interface(), true
}

// setPath writes value at a dotted field path within the addressable value root,
// returning the previous value and whether anything changed.
//
// When onlyIfUnset is true the write is skipped unless the field currently holds
// its zero value. Note that a plain bool field is indistinguishable from unset
// when false; that ambiguity is why the delivery hints use *bool.
func setPath(root reflect.Value, path string, value any, onlyIfUnset bool) (any, bool, error) {
	if path == "" {
		return nil, false, fmt.Errorf("empty field path")
	}
	segs := strings.Split(path, ".")

	cur := root
	for i, seg := range segs {
		var err error
		if cur, err = derefForWrite(cur); err != nil {
			return nil, false, err
		}
		if cur.Kind() != reflect.Struct {
			return nil, false, fmt.Errorf("%q is %s, not a struct", strings.Join(segs[:i], "."), cur.Kind())
		}
		field, found := fieldByJSONTag(cur, seg)
		if !found {
			return nil, false, fmt.Errorf("no such field %q", seg)
		}
		if !field.CanSet() {
			return nil, false, fmt.Errorf("field %q cannot be set", seg)
		}
		cur = field
	}

	if onlyIfUnset && !cur.IsZero() {
		return nil, false, nil
	}
	before := cur.Interface()
	if err := assignValue(cur, value); err != nil {
		return nil, false, err
	}
	if reflect.DeepEqual(before, cur.Interface()) {
		return nil, false, nil
	}
	return before, true, nil
}

// derefForWrite follows pointers, allocating a nil one and cloning a non-nil one
// so that writing through the path never reaches memory the caller still holds.
func derefForWrite(v reflect.Value) (reflect.Value, error) {
	for v.Kind() == reflect.Pointer {
		if !v.CanSet() {
			return v, fmt.Errorf("cannot write through unaddressable %s", v.Type())
		}
		clone := reflect.New(v.Type().Elem())
		if !v.IsNil() {
			clone.Elem().Set(v.Elem())
		}
		v.Set(clone)
		v = v.Elem()
	}
	return v, nil
}

// assignValue writes a decoded YAML value into a field, converting only where the
// conversion is unambiguous. Anything else is an error, so a mistyped rule is
// caught by ValidateRules instead of silently doing nothing.
func assignValue(field reflect.Value, value any) error {
	if value == nil {
		field.Set(reflect.Zero(field.Type()))
		return nil
	}

	if field.Kind() == reflect.Pointer {
		elem := reflect.New(field.Type().Elem())
		if err := assignValue(elem.Elem(), value); err != nil {
			return err
		}
		field.Set(elem)
		return nil
	}

	switch field.Kind() {
	case reflect.String:
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("cannot assign %T to string field", value)
		}
		field.SetString(s)
		return nil

	case reflect.Bool:
		b, ok := value.(bool)
		if !ok {
			return fmt.Errorf("cannot assign %T to bool field", value)
		}
		field.SetBool(b)
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		f, ok := toFloat(value)
		if !ok || f != math.Trunc(f) {
			return fmt.Errorf("cannot assign %v (%T) to %s field", value, value, field.Kind())
		}
		if field.OverflowInt(int64(f)) {
			return fmt.Errorf("%v overflows %s field", value, field.Kind())
		}
		field.SetInt(int64(f))
		return nil

	case reflect.Float32, reflect.Float64:
		f, ok := toFloat(value)
		if !ok {
			return fmt.Errorf("cannot assign %T to %s field", value, field.Kind())
		}
		field.SetFloat(f)
		return nil

	case reflect.Struct:
		if m, ok := normalizeMap(value); ok {
			for _, key := range sortedKeys(m) {
				sub, found := fieldByJSONTag(field, key)
				if !found {
					return fmt.Errorf("no such field %q on %s", key, field.Type())
				}
				if err := assignValue(sub, m[key]); err != nil {
					return fmt.Errorf("%s: %w", key, err)
				}
			}
			return nil
		}
	}

	rv := reflect.ValueOf(value)
	if rv.Type().AssignableTo(field.Type()) {
		field.Set(rv)
		return nil
	}
	return fmt.Errorf("cannot assign %T to %s field", value, field.Type())
}

// resolveFieldType walks a dotted path over a type, returning the type of the
// field it names. Used to check rules before any message exists.
func resolveFieldType(t reflect.Type, path string) (reflect.Type, bool) {
	if path == "" {
		return nil, false
	}
	cur := t
	for _, seg := range strings.Split(path, ".") {
		cur = baseType(cur)
		if cur.Kind() != reflect.Struct {
			return nil, false
		}
		field, ok := structFieldByJSONTag(cur, seg)
		if !ok {
			return nil, false
		}
		cur = field.Type
	}
	return cur, true
}

// fieldByJSONTag finds a struct field by its json tag name, falling back to the
// lower-cased Go field name. Paths in config are written the way the payload looks
// on the wire, so the json tag is the name that matters.
func fieldByJSONTag(v reflect.Value, name string) (reflect.Value, bool) {
	field, ok := structFieldByJSONTag(v.Type(), name)
	if !ok {
		return reflect.Value{}, false
	}
	// FieldByIndexErr rather than FieldByIndex: a nil embedded pointer along the
	// index path panics in the latter, and a malformed rule must not take the
	// process down.
	found, err := v.FieldByIndexErr(field.Index)
	if err != nil {
		return reflect.Value{}, false
	}
	return found, true
}

func structFieldByJSONTag(t reflect.Type, name string) (reflect.StructField, bool) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue // unexported
		}
		tagName := strings.Split(field.Tag.Get("json"), ",")[0]
		switch tagName {
		case "-":
			continue
		case "":
			tagName = lowerFirst(field.Name)
		}
		if tagName == name || field.Name == name {
			return field, true
		}
		// Anonymous embedded structs are flattened by encoding/json, so a path
		// written against the wire format has to see through them too.
		if field.Anonymous {
			if embedded, ok := structFieldByJSONTag(baseType(field.Type), name); ok {
				embedded.Index = append(append([]int{}, field.Index...), embedded.Index...)
				return embedded, true
			}
		}
	}
	return reflect.StructField{}, false
}

// deref follows pointers and interfaces for reading; a nil at any level means the
// value is absent.
func deref(v reflect.Value) (reflect.Value, bool) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Value{}, false
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return reflect.Value{}, false
	}
	return v, true
}

func baseType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func isNumericKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// valuesEqual compares numerically when both sides are numbers and by string form
// otherwise, so that YAML's loose typing does not make an obvious rule fail.
func valuesEqual(a, b any) bool {
	if fa, ok1 := toFloat(a); ok1 {
		if fb, ok2 := toFloat(b); ok2 {
			return fa == fb
		}
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	}
	return 0, false
}

// normalizeMap accepts the map shapes a YAML decoder can produce.
func normalizeMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			key, ok := k.(string)
			if !ok {
				return nil, false
			}
			out[key] = val
		}
		return out, true
	}
	return nil, false
}

// sortedKeys gives map iteration a stable order, so that two rules writing the
// same field always resolve the same way and log output is reproducible.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
