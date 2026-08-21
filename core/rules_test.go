package core_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/wu/keyop/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const alertType = "core.alert.v1"

func alertLookup(payloadType string) (reflect.Type, bool) {
	if payloadType == alertType {
		return reflect.TypeOf(core.AlertEvent{}), true
	}
	return nil, false
}

// rule builds a Rule from the YAML-shaped spec, failing the test on a parse error.
func rule(t *testing.T, spec core.RuleSpec) core.Rule {
	t.Helper()
	rules, errs := core.ParseRules("test", []core.RuleSpec{spec})
	require.Empty(t, errs)
	require.Len(t, rules, 1)
	return rules[0]
}

// --- parsing ---------------------------------------------------------------

func TestParseCondition_Forms(t *testing.T) {
	tests := []struct {
		name   string
		raw    any
		wantOp string
	}{
		{"scalar string is equality", "critical", core.OpEq},
		{"scalar number is equality", 90, core.OpEq},
		{"scalar bool is equality", false, core.OpEq},
		{"list is any-of", []any{"warning", "critical"}, core.OpIn},
		{"contains map", map[string]any{"contains": "disk"}, core.OpContains},
		{"matches map", map[string]any{"matches": "^ssl "}, core.OpMatches},
		{"gt map", map[string]any{"gt": 90}, core.OpGt},
		{"lte map", map[string]any{"lte": 5}, core.OpLte},
		{"explicit eq map", map[string]any{"eq": "info"}, core.OpEq},
		{"yaml v2 style map keys", map[any]any{"contains": "disk"}, core.OpContains},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond, err := core.ParseCondition(tt.raw)
			require.NoError(t, err)
			assert.Equal(t, tt.wantOp, cond.Op)
		})
	}
}

func TestParseCondition_Errors(t *testing.T) {
	tests := []struct {
		name string
		raw  any
		want string
	}{
		{"nil", nil, "must not be empty"},
		{"empty list", []any{}, "must not be empty"},
		{"unknown operator", map[string]any{"startsWith": "x"}, `unknown operator "startsWith"`},
		{"two operators", map[string]any{"gt": 1, "lt": 2}, "exactly one key"},
		{"bad regexp", map[string]any{"matches": "([unclosed"}, "invalid pattern"},
		{"non-string pattern", map[string]any{"matches": 5}, "needs a string pattern"},
		{"non-numeric comparison", map[string]any{"gt": "ninety"}, "needs a number"},
		{"in without a list", map[string]any{"in": "warning"}, "needs a list"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := core.ParseCondition(tt.raw)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// A rule that fails to parse must be reported and dropped, not silently kept in a
// half-built state: the previous implementation's habit of continuing past parse
// errors is what made a typo invisible.
func TestParseRules_ReportsAndDropsBadRules(t *testing.T) {
	rules, errs := core.ParseRules("speak.yaml sub_rules", []core.RuleSpec{
		{Type: alertType, When: map[string]any{"level": "critical"}, Force: map[string]any{"notify.audio": "speak"}},
		{Type: alertType, When: map[string]any{"level": map[string]any{"bogus": 1}}, Set: map[string]any{"category": "x"}},
	})

	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "speak.yaml sub_rules: rule 1: when \"level\"")
	require.Len(t, rules, 1, "the valid rule should survive")
	assert.Equal(t, "speak", rules[0].Force["notify.audio"])
}

// --- matching --------------------------------------------------------------

func TestRule_Match(t *testing.T) {
	alert := core.AlertEvent{
		Summary:  "disk almost full on host1",
		Text:     "90% used",
		Level:    "warning",
		Category: "diskspace",
		Notify:   &core.NotifyHints{Audio: core.AudioSpeak, SMS: core.BoolPtr(false)},
	}

	tests := []struct {
		name string
		when map[string]any
		want bool
	}{
		{"no conditions matches everything", nil, true},
		{"equality", map[string]any{"level": "warning"}, true},
		{"equality mismatch", map[string]any{"level": "critical"}, false},
		{"any-of", map[string]any{"level": []any{"warning", "critical"}}, true},
		{"any-of mismatch", map[string]any{"level": []any{"info", "debug"}}, false},
		{"contains", map[string]any{"summary": map[string]any{"contains": "disk"}}, true},
		{"matches", map[string]any{"summary": map[string]any{"matches": "^disk"}}, true},
		{"matches anchored mismatch", map[string]any{"summary": map[string]any{"matches": "^full"}}, false},
		{"all conditions must hold", map[string]any{"level": "warning", "category": "diskspace"}, true},
		{"one failing condition fails the rule", map[string]any{"level": "warning", "category": "tide"}, false},
		{"nested path", map[string]any{"notify.audio": core.AudioSpeak}, true},
		{"nested bool pointer", map[string]any{"notify.sms": false}, true},
		{"nested bool pointer mismatch", map[string]any{"notify.sms": true}, false},
		{"unknown field never matches", map[string]any{"nosuchfield": "x"}, false},
		// A nil pointer means "no opinion", which is not the same as the zero
		// value: a rule asking for persist:false must not match an alert that
		// simply never expressed a preference.
		{"nil pointer field never matches", map[string]any{"notify.persist": false}, false},
		{"a present but empty field still compares", map[string]any{"notify.color": ""}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := rule(t, core.RuleSpec{Type: alertType, When: tt.when, Force: map[string]any{"category": "x"}})
			assert.Equal(t, tt.want, r.Match(alertType, alert))
		})
	}
}

// A rule only ever applies to the payload type it declares.
func TestRule_Match_PayloadTypeGates(t *testing.T) {
	r := rule(t, core.RuleSpec{Type: alertType, Force: map[string]any{"category": "x"}})

	assert.True(t, r.Match(alertType, core.AlertEvent{}))
	assert.False(t, r.Match("core.error.v1", core.AlertEvent{}))
	assert.False(t, r.Match(alertType, nil))
}

func TestRule_Match_NumericOperators(t *testing.T) {
	metric := core.MetricEvent{Name: "cpu", Value: 92.5}

	for _, tt := range []struct {
		op   string
		val  any
		want bool
	}{
		{core.OpGt, 90, true},
		{core.OpGt, 95, false},
		{core.OpGte, 92.5, true},
		{core.OpLt, 95, true},
		{core.OpLte, 90, false},
	} {
		t.Run(tt.op, func(t *testing.T) {
			r := rule(t, core.RuleSpec{
				Type:  "core.metric.v1",
				When:  map[string]any{"value": map[string]any{tt.op: tt.val}},
				Force: map[string]any{"unit": "percent"},
			})
			assert.Equal(t, tt.want, r.Match("core.metric.v1", metric))
		})
	}
}

// YAML types a bare number as an int and a decimal as a float, while payload
// fields are float64 or int -- equality has to compare numerically rather than by
// text, or an obvious rule silently never fires.
func TestRule_Match_NumericEqualityAcrossTypes(t *testing.T) {
	metric := core.MetricEvent{Name: "cpu", Value: 90}

	for _, operand := range []any{90, 90.0, int64(90)} {
		r := rule(t, core.RuleSpec{
			Type:  "core.metric.v1",
			When:  map[string]any{"value": operand},
			Force: map[string]any{"unit": "percent"},
		})
		assert.True(t, r.Match("core.metric.v1", metric), "operand %v (%T) should match 90", operand, operand)
	}

	mismatched := rule(t, core.RuleSpec{
		Type:  "core.metric.v1",
		When:  map[string]any{"value": 91},
		Force: map[string]any{"unit": "percent"},
	})
	assert.False(t, mismatched.Match("core.metric.v1", metric))
}

func TestRule_Match_PointerPayload(t *testing.T) {
	r := rule(t, core.RuleSpec{Type: alertType, When: map[string]any{"level": "warning"}, Force: map[string]any{"category": "x"}})
	assert.True(t, r.Match(alertType, &core.AlertEvent{Level: "warning"}))
}

// --- applying --------------------------------------------------------------

func TestApplyRules_SetOnlyFillsUnsetFields(t *testing.T) {
	r := rule(t, core.RuleSpec{Type: alertType, Set: map[string]any{"notify.audio": core.AudioSilent}})

	t.Run("fills an unset field", func(t *testing.T) {
		out, changes, errs := core.ApplyRules(alertType, core.AlertEvent{Level: "warning"}, []core.Rule{r})
		require.Empty(t, errs)
		require.Len(t, changes, 1)
		assert.Equal(t, core.AudioSilent, out.(core.AlertEvent).Notify.Audio)
	})

	t.Run("leaves an existing opinion alone", func(t *testing.T) {
		in := core.AlertEvent{Level: "warning", Notify: &core.NotifyHints{Audio: core.AudioSpeak}}
		out, changes, errs := core.ApplyRules(alertType, in, []core.Rule{r})
		require.Empty(t, errs)
		assert.Empty(t, changes)
		assert.Equal(t, core.AudioSpeak, out.(core.AlertEvent).Notify.Audio)
	})
}

func TestApplyRules_ForceOverwrites(t *testing.T) {
	r := rule(t, core.RuleSpec{Type: alertType, Force: map[string]any{"notify.audio": core.AudioSilent}})

	in := core.AlertEvent{Notify: &core.NotifyHints{Audio: core.AudioSpeak}}
	out, changes, errs := core.ApplyRules(alertType, in, []core.Rule{r})
	require.Empty(t, errs)
	require.Len(t, changes, 1)
	assert.True(t, changes[0].Forced)
	assert.Equal(t, core.AudioSpeak, changes[0].From)
	assert.Equal(t, core.AudioSilent, out.(core.AlertEvent).Notify.Audio)
}

// The producer states an opinion, the consumer supplies defaults: set must not
// override it, force must. This is the composition the whole design rests on.
func TestApplyRules_ProducerAndConsumerCompose(t *testing.T) {
	producer := rule(t, core.RuleSpec{Type: alertType, Set: map[string]any{"notify.audio": core.AudioSilent}})
	consumerDefault := rule(t, core.RuleSpec{Type: alertType, Set: map[string]any{"notify.audio": core.AudioSpeak}})
	consumerOverride := rule(t, core.RuleSpec{
		Type:  alertType,
		When:  map[string]any{"level": "critical"},
		Force: map[string]any{"notify.audio": core.AudioSpeak},
	})

	in := core.AlertEvent{Level: "warning"}
	afterProducer, _, _ := core.ApplyRules(alertType, in, []core.Rule{producer})
	afterConsumer, _, _ := core.ApplyRules(alertType, afterProducer, []core.Rule{consumerDefault})
	assert.Equal(t, core.AudioSilent, afterConsumer.(core.AlertEvent).Notify.Audio,
		"a consumer default must not override the producer's opinion")

	critical := core.AlertEvent{Level: "critical", Notify: &core.NotifyHints{Audio: core.AudioSilent}}
	forced, _, _ := core.ApplyRules(alertType, critical, []core.Rule{consumerOverride})
	assert.Equal(t, core.AudioSpeak, forced.(core.AlertEvent).Notify.Audio,
		"an explicit force must win")
}

// Subscribers type assert on the payload (msg.Payload.(core.AlertEvent)), so the
// concrete type has to survive unchanged.
func TestApplyRules_PreservesConcreteType(t *testing.T) {
	r := rule(t, core.RuleSpec{Type: alertType, Force: map[string]any{"category": "tide"}})

	t.Run("value in, value out", func(t *testing.T) {
		out, _, _ := core.ApplyRules(alertType, core.AlertEvent{Summary: "s"}, []core.Rule{r})
		got, ok := out.(core.AlertEvent)
		require.True(t, ok, "expected core.AlertEvent, got %T", out)
		assert.Equal(t, "tide", got.Category)
	})

	t.Run("pointer in, pointer out", func(t *testing.T) {
		out, _, _ := core.ApplyRules(alertType, &core.AlertEvent{Summary: "s"}, []core.Rule{r})
		got, ok := out.(*core.AlertEvent)
		require.True(t, ok, "expected *core.AlertEvent, got %T", out)
		assert.Equal(t, "tide", got.Category)
	})
}

// Producers build an alert, publish the pointer, and may keep using it. A rule
// must never reach back into the caller's data.
func TestApplyRules_DoesNotMutateCaller(t *testing.T) {
	r := rule(t, core.RuleSpec{Type: alertType, Force: map[string]any{
		"category":     "tide",
		"notify.audio": core.AudioSilent,
	}})

	t.Run("pointer payload", func(t *testing.T) {
		original := &core.AlertEvent{Summary: "s", Notify: &core.NotifyHints{Audio: core.AudioSpeak}}
		out, _, _ := core.ApplyRules(alertType, original, []core.Rule{r})

		assert.Equal(t, "", original.Category)
		assert.Equal(t, core.AudioSpeak, original.Notify.Audio, "the caller's hints must not be rewritten")
		assert.Equal(t, core.AudioSilent, out.(*core.AlertEvent).Notify.Audio)
		assert.NotSame(t, original, out)
		assert.NotSame(t, original.Notify, out.(*core.AlertEvent).Notify)
	})

	t.Run("shared hints behind a value payload", func(t *testing.T) {
		hints := &core.NotifyHints{Audio: core.AudioSpeak}
		original := core.AlertEvent{Summary: "s", Notify: hints}
		out, _, _ := core.ApplyRules(alertType, original, []core.Rule{r})

		assert.Equal(t, core.AudioSpeak, hints.Audio, "the shared hints struct must not be rewritten")
		assert.Equal(t, core.AudioSilent, out.(core.AlertEvent).Notify.Audio)
	})
}

func TestApplyRules_AllocatesMissingIntermediateStruct(t *testing.T) {
	r := rule(t, core.RuleSpec{Type: alertType, Force: map[string]any{"notify.sms": false}})

	in := core.AlertEvent{Summary: "s"} // Notify is nil
	out, changes, errs := core.ApplyRules(alertType, in, []core.Rule{r})
	require.Empty(t, errs)
	require.Len(t, changes, 1)

	got := out.(core.AlertEvent)
	require.NotNil(t, got.Notify)
	require.NotNil(t, got.Notify.SMS)
	assert.False(t, *got.Notify.SMS)
}

func TestApplyRules_NoMatchReturnsInputUntouched(t *testing.T) {
	r := rule(t, core.RuleSpec{
		Type:  alertType,
		When:  map[string]any{"level": "critical"},
		Force: map[string]any{"category": "x"},
	})

	in := core.AlertEvent{Level: "info"}
	out, changes, errs := core.ApplyRules(alertType, in, []core.Rule{r})
	assert.Empty(t, changes)
	assert.Empty(t, errs)
	assert.Equal(t, in, out)
}

func TestApplyRules_NoOpWriteIsNotAChange(t *testing.T) {
	r := rule(t, core.RuleSpec{Type: alertType, Force: map[string]any{"category": "tide"}})

	in := core.AlertEvent{Category: "tide"}
	out, changes, errs := core.ApplyRules(alertType, in, []core.Rule{r})
	assert.Empty(t, changes, "rewriting a field with its current value is not a change")
	assert.Empty(t, errs)
	assert.Equal(t, in, out)
}

func TestApplyRules_MultipleRulesMergeIntoOnePayload(t *testing.T) {
	rules, errs := core.ParseRules("test", []core.RuleSpec{
		{Type: alertType, Force: map[string]any{"category": "tide"}},
		{Type: alertType, Force: map[string]any{"notify.color": "orange"}},
		{Type: "core.error.v1", Force: map[string]any{"category": "wrong"}},
	})
	require.Empty(t, errs)

	out, changes, applyErrs := core.ApplyRules(alertType, core.AlertEvent{Summary: "s"}, rules)
	require.Empty(t, applyErrs)
	require.Len(t, changes, 2, "each matching rule contributes, the non-matching one does not")

	got := out.(core.AlertEvent)
	assert.Equal(t, "tide", got.Category)
	assert.Equal(t, "orange", got.Notify.Color)
}

func TestApplyRules_ChangesAreOrderedAndAttributed(t *testing.T) {
	rules, errs := core.ParseRules("test", []core.RuleSpec{
		{Type: alertType, Set: map[string]any{"notify.color": "red", "category": "alpha"}},
	})
	require.Empty(t, errs)

	_, changes, _ := core.ApplyRules(alertType, core.AlertEvent{}, rules)
	require.Len(t, changes, 2)
	// Paths are applied in sorted order so behaviour and logs are reproducible.
	assert.Equal(t, "category", changes[0].Path)
	assert.Equal(t, "notify.color", changes[1].Path)
	for _, c := range changes {
		assert.Equal(t, 0, c.RuleIndex)
		assert.False(t, c.Forced)
	}
}

func TestApplyRules_EmptyInputs(t *testing.T) {
	in := core.AlertEvent{Summary: "s"}

	out, changes, errs := core.ApplyRules(alertType, in, nil)
	assert.Equal(t, in, out)
	assert.Empty(t, changes)
	assert.Empty(t, errs)

	r := rule(t, core.RuleSpec{Type: alertType, Force: map[string]any{"category": "x"}})
	out, changes, errs = core.ApplyRules(alertType, nil, []core.Rule{r})
	assert.Nil(t, out)
	assert.Empty(t, changes)
	assert.Empty(t, errs)

	var nilAlert *core.AlertEvent
	out, _, _ = core.ApplyRules(alertType, nilAlert, []core.Rule{r})
	assert.Equal(t, nilAlert, out)
}

// A write that cannot be performed is reported and the payload left alone, rather
// than being applied partially or silently skipped.
func TestApplyRules_BadWriteIsReported(t *testing.T) {
	r := core.Rule{PayloadType: alertType, Force: map[string]any{"nosuchfield": "x"}}

	in := core.AlertEvent{Summary: "s"}
	out, changes, errs := core.ApplyRules(alertType, in, []core.Rule{r})
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), `no such field "nosuchfield"`)
	assert.Empty(t, changes)
	assert.Equal(t, in, out)
}

// Several services publish an alert wrapped in a richer payload -- tides embeds
// core.AlertEvent anonymously. encoding/json flattens those fields, so a rule
// written against the wire format has to reach them by their plain names.
type tideAlert struct {
	core.AlertEvent
	StationID string `json:"stationId"`
}

func TestApplyRules_EmbeddedStructFields(t *testing.T) {
	const wrapperType = "service.tide.alert.v1"
	lookup := func(pt string) (reflect.Type, bool) {
		if pt == wrapperType {
			return reflect.TypeOf(tideAlert{}), true
		}
		return nil, false
	}

	rules, parseErrs := core.ParseRules("tides.yaml", []core.RuleSpec{{
		Type:  wrapperType,
		When:  map[string]any{"level": "warning", "stationId": "9447130"},
		Force: map[string]any{"notify.audio": core.AudioSilent, "category": "tide"},
	}})
	require.Empty(t, parseErrs)
	require.Empty(t, core.ValidateRules("tides.yaml", rules, lookup),
		"paths through an embedded struct must validate")

	in := tideAlert{
		AlertEvent: core.AlertEvent{Level: "warning", Summary: "extreme low tide"},
		StationID:  "9447130",
	}
	out, changes, errs := core.ApplyRules(wrapperType, in, rules)
	require.Empty(t, errs)
	require.Len(t, changes, 2)

	got := out.(tideAlert)
	assert.Equal(t, "tide", got.Category)
	require.NotNil(t, got.Notify)
	assert.Equal(t, core.AudioSilent, got.Notify.Audio)
	assert.Equal(t, "9447130", got.StationID, "unrelated fields must be carried through")
}

func TestValidateRules_Structural(t *testing.T) {
	tests := []struct {
		name string
		rule core.Rule
		want string
	}{
		{
			name: "missing payload type",
			rule: core.Rule{Force: map[string]any{"category": "x"}},
			want: "'type' is required",
		},
		{
			name: "no writes",
			rule: core.Rule{PayloadType: alertType, When: map[string]core.Condition{}},
			want: "would change nothing",
		},
		{
			name: "path in both set and force",
			rule: core.Rule{
				PayloadType: alertType,
				Set:         map[string]any{"category": "a"},
				Force:       map[string]any{"category": "b"},
			},
			want: "appears in both 'set' and 'force'",
		},
		{
			name: "unknown payload type",
			rule: core.Rule{PayloadType: "service.nope.v1", Force: map[string]any{"category": "x"}},
			want: "unknown payload type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := core.ValidateRules("speak.yaml", []core.Rule{tt.rule}, alertLookup)
			require.NotEmpty(t, errs)
			assert.Contains(t, errs[0].Error(), tt.want)
			assert.Contains(t, errs[0].Error(), "speak.yaml: rule 0")
		})
	}
}

// The check that matters most: a typo'd field name must fail startup rather than
// becoming a rule that never fires.
func TestValidateRules_UnknownFieldPaths(t *testing.T) {
	tests := []struct {
		name string
		spec core.RuleSpec
		want string
	}{
		{
			name: "typo in a when path",
			spec: core.RuleSpec{Type: alertType, When: map[string]any{"levl": "info"}, Force: map[string]any{"category": "x"}},
			want: `when: "levl" is not a field`,
		},
		{
			name: "typo in a set path",
			spec: core.RuleSpec{Type: alertType, Set: map[string]any{"notify.audi": "speak"}},
			want: `no such field "audi"`,
		},
		{
			name: "typo in a force path",
			spec: core.RuleSpec{Type: alertType, Force: map[string]any{"notyfy.audio": "speak"}},
			want: `no such field "notyfy"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules, parseErrs := core.ParseRules("test", []core.RuleSpec{tt.spec})
			require.Empty(t, parseErrs)
			errs := core.ValidateRules("test", rules, alertLookup)
			require.NotEmpty(t, errs)
			assert.Contains(t, errs[0].Error(), tt.want)
		})
	}
}

func TestValidateRules_ValueTypes(t *testing.T) {
	tests := []struct {
		name    string
		writes  map[string]any
		wantErr string
	}{
		{"string into string", map[string]any{"notify.audio": "silent"}, ""},
		{"bool into *bool", map[string]any{"notify.sms": false}, ""},
		{"number into string field", map[string]any{"level": 3}, "cannot assign int to string field"},
		{"string into *bool", map[string]any{"notify.sms": "false"}, "cannot assign string to bool field"},
		{"bool into string field", map[string]any{"category": true}, "cannot assign bool to string field"},
		{"map into a scalar field", map[string]any{"level": map[string]any{"a": 1}}, "cannot assign"},
		{"struct from a map", map[string]any{"notify": map[string]any{"audio": "silent", "color": "red"}}, ""},
		{"struct from a map with a bad field", map[string]any{"notify": map[string]any{"audi": "silent"}}, `no such field "audi"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := core.ValidateRules("test", []core.Rule{{PayloadType: alertType, Force: tt.writes}}, alertLookup)
			if tt.wantErr == "" {
				assert.Empty(t, errs)
				return
			}
			require.NotEmpty(t, errs)
			assert.Contains(t, errs[0].Error(), tt.wantErr)
		})
	}
}

func TestValidateRules_OperatorFieldTypes(t *testing.T) {
	tests := []struct {
		name string
		when map[string]any
		want string
	}{
		{"numeric operator on a string field", map[string]any{"level": map[string]any{"gt": 3}}, "needs a numeric field"},
		{"contains on a non-string field", map[string]any{"notify.sms": map[string]any{"contains": "x"}}, "needs a string field"},
		{"matches on a non-string field", map[string]any{"notify.sms": map[string]any{"matches": "x"}}, "needs a string field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules, parseErrs := core.ParseRules("test", []core.RuleSpec{
				{Type: alertType, When: tt.when, Force: map[string]any{"category": "x"}},
			})
			require.Empty(t, parseErrs)
			errs := core.ValidateRules("test", rules, alertLookup)
			require.NotEmpty(t, errs)
			assert.Contains(t, errs[0].Error(), tt.want)
		})
	}
}

func TestValidateRules_AcceptsRealisticRules(t *testing.T) {
	rules, parseErrs := core.ParseRules("tides.yaml pub_rules", []core.RuleSpec{
		{
			Type: alertType,
			When: map[string]any{"level": "warning"},
			Set:  map[string]any{"notify.audio": core.AudioSilent, "notify.persist": false, "category": "tide"},
		},
		{
			Type:  alertType,
			When:  map[string]any{"summary": map[string]any{"contains": "extreme"}},
			Force: map[string]any{"notify.color": "orange"},
		},
	})
	require.Empty(t, parseErrs)
	assert.Empty(t, core.ValidateRules("tides.yaml pub_rules", rules, alertLookup))
}

// Without a lookup only the structural checks run, so a caller with no registry
// still catches the obvious mistakes.
func TestValidateRules_NilLookupSkipsTypeChecks(t *testing.T) {
	r := core.Rule{PayloadType: "anything.v1", Force: map[string]any{"nosuchfield": "x"}}
	assert.Empty(t, core.ValidateRules("test", []core.Rule{r}, nil))

	noWrites := core.Rule{PayloadType: "anything.v1"}
	assert.NotEmpty(t, core.ValidateRules("test", []core.Rule{noWrites}, nil))
}

func TestValidateRules_ReportsEveryProblem(t *testing.T) {
	rules := []core.Rule{
		{Force: map[string]any{"category": "x"}},                                 // missing type
		{PayloadType: alertType},                                                 // no writes
		{PayloadType: alertType, Force: map[string]any{"nosuchfield": "x"}},      // bad path
		{PayloadType: "service.nope.v1", Force: map[string]any{"category": "x"}}, // unknown type
	}
	assert.Len(t, core.ValidateRules("test", rules, alertLookup), 4)
}

// Timestamps and other non-scalar fields should still be reachable, so that a rule
// can be validated against the whole payload rather than a convenient subset.
func TestApplyRules_TimeFieldIsAddressableButTyped(t *testing.T) {
	errs := core.ValidateRules("test", []core.Rule{
		{PayloadType: alertType, Force: map[string]any{"timestamp": "not-a-time"}},
	}, alertLookup)
	require.NotEmpty(t, errs, "a string must not be assignable to a time.Time field")

	ts := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	out, changes, applyErrs := core.ApplyRules(alertType, core.AlertEvent{}, []core.Rule{
		{PayloadType: alertType, Force: map[string]any{"timestamp": ts}},
	})
	require.Empty(t, applyErrs)
	require.Len(t, changes, 1)
	assert.Equal(t, ts, out.(core.AlertEvent).Timestamp)
}

// --- payload shapes --------------------------------------------------------

// Registered payloads always decode to structs, but Publish accepts anything.
// A payload the engine cannot walk must produce an error rather than quietly
// dropping the rule's writes.
func TestApplyRules_NonStructPayloadIsAnError(t *testing.T) {
	r := core.Rule{PayloadType: alertType, Force: map[string]any{"category": "x"}}

	in := map[string]any{"level": "warning"}
	out, changes, errs := core.ApplyRules(alertType, in, []core.Rule{r})
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "not a struct")
	assert.Empty(t, changes)
	assert.Equal(t, in, out)
}

// Payloads without json tags fall back to the lower-cased Go field name, so a rule
// can still address them by the name they serialise under.
func TestRules_UntaggedFieldsUseLowerCasedGoName(t *testing.T) {
	type untagged struct {
		Kind  string
		Count int
	}
	const untaggedType = "service.untagged.v1"
	lookup := func(pt string) (reflect.Type, bool) {
		if pt == untaggedType {
			return reflect.TypeOf(untagged{}), true
		}
		return nil, false
	}

	rules, parseErrs := core.ParseRules("test", []core.RuleSpec{{
		Type:  untaggedType,
		When:  map[string]any{"count": map[string]any{"gt": 2}},
		Force: map[string]any{"kind": "busy"},
	}})
	require.Empty(t, parseErrs)
	require.Empty(t, core.ValidateRules("test", rules, lookup))

	out, changes, errs := core.ApplyRules(untaggedType, untagged{Kind: "idle", Count: 5}, rules)
	require.Empty(t, errs)
	require.Len(t, changes, 1)
	assert.Equal(t, "busy", out.(untagged).Kind)
}

func TestRules_NumericAndNilAssignment(t *testing.T) {
	type numbers struct {
		Count int     `json:"count"`
		Ratio float64 `json:"ratio"`
		Label string  `json:"label"`
	}
	const numbersType = "service.numbers.v1"
	lookup := func(pt string) (reflect.Type, bool) {
		if pt == numbersType {
			return reflect.TypeOf(numbers{}), true
		}
		return nil, false
	}

	t.Run("integers and floats", func(t *testing.T) {
		r := core.Rule{PayloadType: numbersType, Force: map[string]any{"count": 7, "ratio": 0.5}}
		require.Empty(t, core.ValidateRules("test", []core.Rule{r}, lookup))

		out, _, errs := core.ApplyRules(numbersType, numbers{}, []core.Rule{r})
		require.Empty(t, errs)
		assert.Equal(t, 7, out.(numbers).Count)
		assert.InDelta(t, 0.5, out.(numbers).Ratio, 0.0001)
	})

	t.Run("a fractional value is not an integer", func(t *testing.T) {
		r := core.Rule{PayloadType: numbersType, Force: map[string]any{"count": 1.5}}
		errs := core.ValidateRules("test", []core.Rule{r}, lookup)
		require.NotEmpty(t, errs)
		assert.Contains(t, errs[0].Error(), "cannot assign")
	})

	t.Run("an explicit null clears a field", func(t *testing.T) {
		r := core.Rule{PayloadType: numbersType, Force: map[string]any{"label": nil}}
		out, changes, errs := core.ApplyRules(numbersType, numbers{Label: "set"}, []core.Rule{r})
		require.Empty(t, errs)
		require.Len(t, changes, 1)
		assert.Equal(t, "", out.(numbers).Label)
	})
}
