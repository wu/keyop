package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================
// Test helpers
// ============================================================

type captureMessenger struct {
	Sent []Message
}

func (c *captureMessenger) Send(msg Message) error {
	c.Sent = append(c.Sent, msg)
	return nil
}
func (c *captureMessenger) Subscribe(_ context.Context, _ string, _ string, _ string, _ string, _ time.Duration, _ func(Message) error) error {
	return nil
}
func (c *captureMessenger) SubscribeExtended(_ context.Context, _ string, _ string, _ string, _ string, _ time.Duration, _ func(Message, string, int64) error) error {
	return nil
}
func (c *captureMessenger) SetReaderState(_ string, _ string, _ string, _ int64) error { return nil }
func (c *captureMessenger) SeekToEnd(_ string, _ string) error                         { return nil }
func (c *captureMessenger) SetDataDir(_ string)                                        {}
func (c *captureMessenger) SetHostname(_ string)                                       {}
func (c *captureMessenger) GetStats() MessengerStats                                   { return MessengerStats{} }
func (c *captureMessenger) GetPayloadRegistry() PayloadRegistry                        { return nil }
func (c *captureMessenger) SetPayloadRegistry(reg PayloadRegistry)                     {}

// subscribeCapture captures the handler passed to Subscribe so tests can invoke it directly.
type subscribeCapture struct {
	captureMessenger
	handler func(func(Message) error)
}

func (s *subscribeCapture) Subscribe(_ context.Context, _ string, _ string, _ string, _ string, _ time.Duration, h func(Message) error) error {
	if s.handler != nil {
		s.handler(h)
	}
	return nil
}
func (s *subscribeCapture) GetPayloadRegistry() PayloadRegistry    { return nil }
func (s *subscribeCapture) SetPayloadRegistry(reg PayloadRegistry) {}

// ============================================================
// tokenize
// ============================================================

func TestTokenize(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"a b c", []string{"a", "b", "c"}},
		{"  a   b   c  ", []string{"a", "b", "c"}},
		{`status eq "some value"`, []string{"status", "eq", "some value"}},
		{"status eq 'some value'", []string{"status", "eq", "some value"}},
		{`text eq "say \"hi\""`, []string{"text", "eq", `say "hi"`}},
		{"text eq 'it\\'s fine'", []string{"text", "eq", "it's fine"}},
		{"text matches /hello world/", []string{"text", "matches", "/hello world/"}},
		{"metric > 90", []string{"metric", ">", "90"}},
		{"my_field.sub >= 3.14", []string{"my_field.sub", ">=", "3.14"}},
		{"", []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := tokenize(tc.input)
			require.NoError(t, err)
			if len(tc.want) == 0 {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

// ============================================================
// ParseConditionString
// ============================================================

func TestParseConditionString_StringOps(t *testing.T) {
	cases := []struct {
		expr    string
		wantOp  string
		wantVal interface{}
	}{
		{`status eq ok`, "eq", "ok"},
		{`status eq "warn"`, "eq", "warn"},
		{`status eq 'warn'`, "eq", "warn"},
		{`status ne "ok"`, "ne", "ok"},
		{`text contains error`, "contains", "error"},
		{`text contains "some error"`, "contains", "some error"},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			c, err := ParseConditionString(tc.expr)
			require.NoError(t, err)
			assert.Equal(t, tc.wantOp, c.Operator)
			assert.Equal(t, tc.wantVal, c.Value)
		})
	}
}

func TestParseConditionString_NumericOps(t *testing.T) {
	cases := []struct {
		expr    string
		wantOp  string
		wantVal float64
	}{
		{"metric > 90", ">", 90},
		{"metric >= 90", ">=", 90},
		{"metric < 10", "<", 10},
		{"metric <= 10.5", "<=", 10.5},
		{"metric == 42", "==", 42},
		{"metric != 0", "!=", 0},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			c, err := ParseConditionString(tc.expr)
			require.NoError(t, err)
			assert.Equal(t, tc.wantOp, c.Operator)
			assert.InDelta(t, tc.wantVal, c.Value.(float64), 0.0001)
		})
	}
}

func TestParseConditionString_MatchesOp(t *testing.T) {
	t.Run("simple regexp", func(t *testing.T) {
		c, err := ParseConditionString("text matches /error/")
		require.NoError(t, err)
		assert.Equal(t, "matches", c.Operator)
		assert.Equal(t, "/error/", c.Value)
	})
	t.Run("case-insensitive regexp via inline flag", func(t *testing.T) {
		c, err := ParseConditionString("text matches /(?i)error/")
		require.NoError(t, err)
		assert.Equal(t, "/(?i)error/", c.Value)
	})
	t.Run("regexp with spaces in pattern", func(t *testing.T) {
		c, err := ParseConditionString("text matches /hello world/")
		require.NoError(t, err)
		assert.Equal(t, "/hello world/", c.Value)
	})
}

func TestParseConditionString_FieldVariants(t *testing.T) {
	cases := []string{
		"my_field eq val",
		"my.nested.field eq val",
		"camelCaseField eq val",
		"field123 eq val",
	}
	for _, expr := range cases {
		t.Run(expr, func(t *testing.T) {
			_, err := ParseConditionString(expr)
			assert.NoError(t, err)
		})
	}
}

func TestParseConditionString_Errors(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want string
	}{
		{"too few tokens", "status eq", "expected '<field> <operator> <value>'"},
		{"unknown operator", "status xxx val", "unknown operator"},
		{"numeric op with non-number", "metric > abc", "numeric value"},
		{"matches without slash", "text matches error", "requires a /regexp/ value"},
		{"invalid regexp", "text matches /[invalid/", "invalid regexp"},
		{"empty string", "", "expected '<field> <operator> <value>'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseConditionString(tc.expr)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// ============================================================
// ParseConditions — three input shapes
// ============================================================

func TestParseConditions_ShapeString(t *testing.T) {
	raw := []interface{}{
		`status eq "warn"`,
		`metric > 80`,
	}
	conds := ParseConditions(raw)
	require.Len(t, conds, 2)
	assert.Equal(t, "status", conds[0].Field)
	assert.Equal(t, "eq", conds[0].Operator)
	assert.Equal(t, "warn", conds[0].Value)
	assert.Nil(t, conds[0].Updates)
	assert.Equal(t, "metric", conds[1].Field)
	assert.Equal(t, ">", conds[1].Operator)
	assert.InDelta(t, 80.0, conds[1].Value.(float64), 0.0001)
}

func TestParseConditions_ShapeWhen(t *testing.T) {
	raw := []interface{}{
		map[string]interface{}{
			"when": `status eq "warn"`,
			"updates": map[string]interface{}{
				"summary": "warning raised",
			},
		},
	}
	conds := ParseConditions(raw)
	require.Len(t, conds, 1)
	assert.Equal(t, "status", conds[0].Field)
	assert.Equal(t, "warn", conds[0].Value)
	assert.Equal(t, map[string]any{"summary": "warning raised"}, conds[0].Updates)
}

func TestParseConditions_MixedShapes(t *testing.T) {
	raw := []interface{}{
		`status eq "ok"`,
		map[string]interface{}{
			"when":    `metric > 90`,
			"updates": map[string]interface{}{"status": "critical"},
		},
	}
	conds := ParseConditions(raw)
	require.Len(t, conds, 2)
	assert.Equal(t, "eq", conds[0].Operator)
	assert.Equal(t, ">", conds[1].Operator)
}

func TestParseConditions_InvalidStringSkipped(t *testing.T) {
	raw := []interface{}{
		"bad expression", // only 2 tokens — skipped
		`status eq "ok"`,
	}
	conds := ParseConditions(raw)
	require.Len(t, conds, 1)
	assert.Equal(t, "status", conds[0].Field)
}

func TestParseConditions_Empty(t *testing.T) {
	assert.Empty(t, ParseConditions(nil))
	assert.Empty(t, ParseConditions([]interface{}{}))
}

// ============================================================
// ValidateConditions
// ============================================================

func TestValidateConditions_Valid(t *testing.T) {
	raw := []interface{}{
		`status eq "ok"`,
		map[string]interface{}{"when": `metric > 90`, "updates": map[string]interface{}{"status": "critical"}},
	}
	assert.Empty(t, ValidateConditions("test", raw))
}

func TestValidateConditions_StringBadExpr(t *testing.T) {
	raw := []interface{}{"status badop value"}
	errs := ValidateConditions("test", raw)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "unknown operator")
}

func TestValidateConditions_WhenBadExpr(t *testing.T) {
	raw := []interface{}{
		map[string]interface{}{"when": "metric > notanumber"},
	}
	errs := ValidateConditions("test", raw)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "numeric value")
}

func TestValidateConditions_WhenMapMissingWhen(t *testing.T) {
	// A map without a "when" key should produce an error.
	raw := []interface{}{
		map[string]interface{}{"updates": map[string]interface{}{"status": "ok"}},
	}
	errs := ValidateConditions("test", raw)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "'when'")
}

func TestValidateConditions_NotStringOrMap(t *testing.T) {
	raw := []interface{}{42}
	errs := ValidateConditions("test", raw)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "not a string or map")
}

// ============================================================
// evaluateCondition — all operators via ApplyConditions
// ============================================================

func TestEvaluateCondition_StringOps(t *testing.T) {
	cases := []struct {
		name string
		cond ConditionConfig
		msg  Message
		want bool
	}{
		{"eq hit", ConditionConfig{Field: "status", Operator: "eq", Value: "ok"}, Message{Status: "ok"}, true},
		{"eq miss", ConditionConfig{Field: "status", Operator: "eq", Value: "ok"}, Message{Status: "bad"}, false},
		{"ne hit", ConditionConfig{Field: "status", Operator: "ne", Value: "ok"}, Message{Status: "bad"}, true},
		{"ne miss", ConditionConfig{Field: "status", Operator: "ne", Value: "ok"}, Message{Status: "ok"}, false},
		{"contains hit", ConditionConfig{Field: "text", Operator: "contains", Value: "err"}, Message{Text: "an error"}, true},
		{"contains miss", ConditionConfig{Field: "text", Operator: "contains", Value: "err"}, Message{Text: "all good"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			results := ApplyConditions(tc.msg, []ConditionConfig{tc.cond})
			assert.Equal(t, tc.want, len(results) > 0)
		})
	}
}

func TestEvaluateCondition_NumericOps(t *testing.T) {
	cases := []struct {
		name   string
		op     string
		metric float64
		value  float64
		want   bool
	}{
		{"> hit", ">", 20, 10, true},
		{"> miss", ">", 5, 10, false},
		{"< hit", "<", 5, 10, true},
		{"< miss", "<", 20, 10, false},
		{">= hit equal", ">=", 10, 10, true},
		{">= hit greater", ">=", 11, 10, true},
		{">= miss", ">=", 9, 10, false},
		{"<= hit equal", "<=", 10, 10, true},
		{"<= hit lesser", "<=", 9, 10, true},
		{"<= miss", "<=", 11, 10, false},
		{"== hit", "==", 42, 42, true},
		{"== miss", "==", 43, 42, false},
		{"!= hit", "!=", 43, 42, true},
		{"!= miss", "!=", 42, 42, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cond := ConditionConfig{Field: "metric", Operator: tc.op, Value: tc.value}
			results := ApplyConditions(Message{Metric: tc.metric}, []ConditionConfig{cond})
			assert.Equal(t, tc.want, len(results) > 0)
		})
	}
}

func TestEvaluateCondition_MatchesOp(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		text    string
		want    bool
	}{
		{"simple hit", "/error/", "an error occurred", true},
		{"simple miss", "/error/", "all good", false},
		{"case-insensitive hit", "/(?i)ERROR/", "an error occurred", true},
		{"anchored hit", "/^start/", "start of line", true},
		{"anchored miss", "/^start/", "not start", false},
		{"digit pattern hit", `/\d+/`, "value 42 here", true},
		{"digit pattern miss", `/\d+/`, "no digits here", false},
		{"spaces in pattern hit", "/hello world/", "say hello world today", true},
		{"spaces in pattern miss", "/hello world/", "hello there", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cond := ConditionConfig{Field: "text", Operator: "matches", Value: tc.pattern}
			results := ApplyConditions(Message{Text: tc.text}, []ConditionConfig{cond})
			assert.Equal(t, tc.want, len(results) > 0)
		})
	}
}

func TestEvaluateCondition_MissingField(t *testing.T) {
	cond := ConditionConfig{Field: "nonexistent", Operator: "eq", Value: "x"}
	assert.Empty(t, ApplyConditions(Message{Status: "ok"}, []ConditionConfig{cond}))
}

func TestEvaluateCondition_NumericStringCoercion(t *testing.T) {
	// Value supplied as a string (e.g. from certain YAML decoders) should still work
	// for numeric operators via the toFloat string case.
	cond := ConditionConfig{Field: "metric", Operator: ">", Value: "80"}
	assert.NotEmpty(t, ApplyConditions(Message{Metric: 90}, []ConditionConfig{cond}))
}

// ============================================================
// ApplyConditions (pub_preprocess behaviour)
// ============================================================

func TestApplyConditions_NoMatch(t *testing.T) {
	conds := []ConditionConfig{{Field: "metric", Operator: ">", Value: float64(100)}}
	assert.Empty(t, ApplyConditions(Message{Metric: 50}, conds))
}

func TestApplyConditions_SingleMatch(t *testing.T) {
	conds := []ConditionConfig{
		{Field: "metric", Operator: ">", Value: float64(80), Updates: map[string]any{"status": "critical"}},
	}
	results := ApplyConditions(Message{Metric: 90}, conds)
	require.Len(t, results, 1)
	assert.Equal(t, "critical", results[0].Status)
}

func TestApplyConditions_MultipleMatches_EachYieldsMessage(t *testing.T) {
	conds := []ConditionConfig{
		{Field: "metric", Operator: ">", Value: float64(80), Updates: map[string]any{"status": "critical"}},
		{Field: "text", Operator: "contains", Value: "error", Updates: map[string]any{"summary": "error detected"}},
	}
	results := ApplyConditions(Message{Metric: 90, Text: "error in system"}, conds)
	require.Len(t, results, 2)
	assert.Equal(t, "critical", results[0].Status)
	assert.Equal(t, "error detected", results[1].Summary)
}

func TestApplyConditions_UpdateChannelName(t *testing.T) {
	cond, err := ParseConditionString(`status eq "warn"`)
	require.NoError(t, err)
	cond.Updates = map[string]any{"channelName": "alerts"}
	results := ApplyConditions(Message{Status: "warn", ChannelName: "events"}, []ConditionConfig{cond})
	require.Len(t, results, 1)
	assert.Equal(t, "alerts", results[0].ChannelName)
}

// ============================================================
// ApplySubPreprocess (sub_preprocess behaviour)
// ============================================================

func TestApplySubPreprocess_NoConditions_PassThrough(t *testing.T) {
	msg := Message{Status: "ok"}
	result, ok := ApplySubPreprocess(msg, nil)
	assert.True(t, ok)
	assert.Equal(t, msg, result)
}

func TestApplySubPreprocess_Match_UpdatesApplied(t *testing.T) {
	conds := []ConditionConfig{
		{Field: "status", Operator: "eq", Value: "warn", Updates: map[string]any{"summary": "warning raised"}},
	}
	result, ok := ApplySubPreprocess(Message{Status: "warn"}, conds)
	assert.True(t, ok)
	assert.Equal(t, "warning raised", result.Summary)
}

func TestApplySubPreprocess_NoMatch_MessageDropped(t *testing.T) {
	conds := []ConditionConfig{
		{Field: "status", Operator: "eq", Value: "warn"},
	}
	_, ok := ApplySubPreprocess(Message{Status: "ok"}, conds)
	assert.False(t, ok)
}

func TestApplySubPreprocess_MultipleMatches_Merged(t *testing.T) {
	conds := []ConditionConfig{
		{Field: "metric", Operator: ">", Value: float64(80), Updates: map[string]any{"status": "critical"}},
		{Field: "text", Operator: "contains", Value: "error", Updates: map[string]any{"summary": "error detected"}},
	}
	result, ok := ApplySubPreprocess(Message{Metric: 90, Text: "error in system"}, conds)
	assert.True(t, ok)
	assert.Equal(t, "critical", result.Status)
	assert.Equal(t, "error detected", result.Summary)
}

func TestApplySubPreprocess_RegexpMatch(t *testing.T) {
	cond, err := ParseConditionString(`text matches /(?i)CRITICAL/`)
	require.NoError(t, err)
	cond.Updates = map[string]any{"status": "critical"}
	result, ok := ApplySubPreprocess(Message{Text: "this is critical news"}, []ConditionConfig{cond})
	assert.True(t, ok)
	assert.Equal(t, "critical", result.Status)
}

// ============================================================
// PreprocessMessenger wrappers
// ============================================================

func TestPreprocessMessenger_Send_NoConditions(t *testing.T) {
	inner := &captureMessenger{}
	pm := NewPreprocessMessenger(inner, nil, nil)
	require.NoError(t, pm.Send(Message{ChannelName: "out", Status: "ok"}))
	require.Len(t, inner.Sent, 1)
	assert.Equal(t, "ok", inner.Sent[0].Status)
}

func TestPreprocessMessenger_Send_PubPreprocess_Match(t *testing.T) {
	inner := &captureMessenger{}
	pubConds := []ConditionConfig{
		{Field: "status", Operator: "eq", Value: "warn", Updates: map[string]any{"summary": "warning!"}},
	}
	pm := NewPreprocessMessenger(inner, nil, pubConds)
	require.NoError(t, pm.Send(Message{ChannelName: "out", Status: "warn"}))
	require.Len(t, inner.Sent, 1)
	assert.Equal(t, "warning!", inner.Sent[0].Summary)
}

func TestPreprocessMessenger_Send_PubPreprocess_NoMatch_PassThrough(t *testing.T) {
	inner := &captureMessenger{}
	pubConds := []ConditionConfig{
		{Field: "status", Operator: "eq", Value: "critical"},
	}
	pm := NewPreprocessMessenger(inner, nil, pubConds)
	require.NoError(t, pm.Send(Message{ChannelName: "out", Status: "ok"}))
	require.Len(t, inner.Sent, 1)
	assert.Equal(t, "ok", inner.Sent[0].Status)
}

func TestPreprocessMessenger_Send_PubPreprocess_MultipleMatches(t *testing.T) {
	inner := &captureMessenger{}
	pubConds := []ConditionConfig{
		{Field: "status", Operator: "eq", Value: "warn", Updates: map[string]any{"channelName": "alerts"}},
		{Field: "status", Operator: "eq", Value: "warn", Updates: map[string]any{"channelName": "log"}},
	}
	pm := NewPreprocessMessenger(inner, nil, pubConds)
	require.NoError(t, pm.Send(Message{Status: "warn"}))
	require.Len(t, inner.Sent, 2)
	assert.Equal(t, "alerts", inner.Sent[0].ChannelName)
	assert.Equal(t, "log", inner.Sent[1].ChannelName)
}

func TestPreprocessMessenger_Subscribe_SubPreprocess_Match(t *testing.T) {
	subConds := []ConditionConfig{
		{Field: "status", Operator: "eq", Value: "warn", Updates: map[string]any{"summary": "processed"}},
	}
	var received []Message
	handler := func(msg Message) error {
		received = append(received, msg)
		return nil
	}
	var capturedHandler func(Message) error
	inner := &subscribeCapture{handler: func(h func(Message) error) { capturedHandler = h }}
	pm := NewPreprocessMessenger(inner, subConds, nil)
	_ = pm.Subscribe(context.Background(), "src", "ch", "t", "n", 0, handler)

	require.NoError(t, capturedHandler(Message{Status: "warn"}))
	require.Len(t, received, 1)
	assert.Equal(t, "processed", received[0].Summary)
}

func TestPreprocessMessenger_Subscribe_SubPreprocess_Drop(t *testing.T) {
	subConds := []ConditionConfig{
		{Field: "status", Operator: "eq", Value: "warn"},
	}
	var received []Message
	handler := func(msg Message) error {
		received = append(received, msg)
		return nil
	}
	var capturedHandler func(Message) error
	inner := &subscribeCapture{handler: func(h func(Message) error) { capturedHandler = h }}
	pm := NewPreprocessMessenger(inner, subConds, nil)
	_ = pm.Subscribe(context.Background(), "src", "ch", "t", "n", 0, handler)

	require.NoError(t, capturedHandler(Message{Status: "ok"}))
	assert.Empty(t, received)
}

// ============================================================
// ParsePreprocessConditions — reads from ServiceConfig
// ============================================================

func TestParsePreprocessConditions_CompactString(t *testing.T) {
	cfg := ServiceConfig{
		Config: map[string]interface{}{
			"sub_preprocess": []interface{}{`status eq "warn"`},
			"pub_preprocess": []interface{}{`metric > 90`},
		},
	}
	subConds, pubConds := ParsePreprocessConditions(cfg)
	require.Len(t, subConds, 1)
	assert.Equal(t, "status", subConds[0].Field)
	require.Len(t, pubConds, 1)
	assert.Equal(t, "metric", pubConds[0].Field)
}

func TestParsePreprocessConditions_WhenMap(t *testing.T) {
	cfg := ServiceConfig{
		Config: map[string]interface{}{
			"pub_preprocess": []interface{}{
				map[string]interface{}{
					"when":    `status eq "warn"`,
					"updates": map[string]interface{}{"channelName": "alerts"},
				},
			},
		},
	}
	_, pubConds := ParsePreprocessConditions(cfg)
	require.Len(t, pubConds, 1)
	assert.Equal(t, "alerts", pubConds[0].Updates["channelName"])
}

func TestParsePreprocessConditions_Empty(t *testing.T) {
	cfg := ServiceConfig{Config: map[string]interface{}{}}
	subConds, pubConds := ParsePreprocessConditions(cfg)
	assert.Empty(t, subConds)
	assert.Empty(t, pubConds)
}

// ============================================================
// Heartbeat-style routing use case (mirrors heartbeat.yaml)
// ============================================================

func TestHeartbeatStyleRouting(t *testing.T) {
	// Simulates heartbeat.yaml pub_preprocess routing by status field.
	raw := []interface{}{
		map[string]interface{}{"when": `status eq "restart"`, "updates": map[string]interface{}{"channelName": "alerts"}},
		map[string]interface{}{"when": `status eq "uptime"`, "updates": map[string]interface{}{"channelName": "heartbeat"}},
		map[string]interface{}{"when": `status eq "uptime-metric"`, "updates": map[string]interface{}{"channelName": "metrics"}},
	}
	conds := ParseConditions(raw)
	require.Len(t, conds, 3)

	inner := &captureMessenger{}
	pm := NewPreprocessMessenger(inner, nil, conds)

	_ = pm.Send(Message{Status: "restart"})
	_ = pm.Send(Message{Status: "uptime"})
	_ = pm.Send(Message{Status: "uptime-metric"})
	_ = pm.Send(Message{Status: "unknown"}) // no match → sent unchanged

	require.Len(t, inner.Sent, 4)
	assert.Equal(t, "alerts", inner.Sent[0].ChannelName)
	assert.Equal(t, "heartbeat", inner.Sent[1].ChannelName)
	assert.Equal(t, "metrics", inner.Sent[2].ChannelName)
	assert.Equal(t, "unknown", inner.Sent[3].Status)
	assert.Equal(t, "", inner.Sent[3].ChannelName) // unchanged
}
