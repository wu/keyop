package core

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================
// lowerFirst
// ============================================================

func TestLowerFirst(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"A", "a"},
		{"Hello", "hello"},
		{"HTMLParser", "hTMLParser"},
		{"already", "already"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, lowerFirst(tc.in), "input: %q", tc.in)
	}
}

// ============================================================
// toFloat
// ============================================================

func TestToFloat(t *testing.T) {
	cases := []struct {
		in     any
		want   float64
		wantOK bool
	}{
		{float64(3.14), 3.14, true},
		{float32(2.5), 2.5, true},
		{int(7), 7.0, true},
		{int64(42), 42.0, true},
		{"1.5", 1.5, true},
		{"not-a-number", 0, false},
		{true, 0, false},
		{nil, 0, false},
	}
	for _, tc := range cases {
		got, ok := toFloat(tc.in)
		assert.Equal(t, tc.wantOK, ok, "toFloat(%v) ok", tc.in)
		if tc.wantOK {
			assert.InDelta(t, tc.want, got, 0.0001, "toFloat(%v) value", tc.in)
		}
	}
}

// ============================================================
// getValueByJSONPath
// ============================================================

func TestGetValueByJSONPath_EmptyPath(t *testing.T) {
	_, ok := getValueByJSONPath(map[string]any{"k": "v"}, "")
	assert.False(t, ok)
}

func TestGetValueByJSONPath_MapSimple(t *testing.T) {
	m := map[string]any{"status": "ok"}
	v, ok := getValueByJSONPath(m, "status")
	require.True(t, ok)
	assert.Equal(t, "ok", v)
}

func TestGetValueByJSONPath_MapNested(t *testing.T) {
	m := map[string]any{
		"outer": map[string]any{"inner": "deep"},
	}
	v, ok := getValueByJSONPath(m, "outer.inner")
	require.True(t, ok)
	assert.Equal(t, "deep", v)
}

func TestGetValueByJSONPath_MapMissingKey(t *testing.T) {
	m := map[string]any{"a": "b"}
	_, ok := getValueByJSONPath(m, "missing")
	assert.False(t, ok)
}

func TestGetValueByJSONPath_StructByJSONTag(t *testing.T) {
	type S struct {
		MyField string `json:"my_field"`
	}
	v, ok := getValueByJSONPath(S{MyField: "hello"}, "my_field")
	require.True(t, ok)
	assert.Equal(t, "hello", v)
}

func TestGetValueByJSONPath_StructByFieldName(t *testing.T) {
	type S struct {
		Name string
	}
	v, ok := getValueByJSONPath(S{Name: "test"}, "name")
	require.True(t, ok)
	assert.Equal(t, "test", v)
}

func TestGetValueByJSONPath_NilPointer(t *testing.T) {
	var p *Message
	_, ok := getValueByJSONPath(p, "status")
	assert.False(t, ok)
}

func TestGetValueByJSONPath_PointerDereference(t *testing.T) {
	m := &map[string]any{"key": "val"}
	v, ok := getValueByJSONPath(m, "key")
	require.True(t, ok)
	assert.Equal(t, "val", v)
}

func TestGetValueByJSONPath_NonStringMapKey(t *testing.T) {
	m := map[int]any{1: "v"}
	_, ok := getValueByJSONPath(m, "1")
	assert.False(t, ok)
}

func TestGetValueByJSONPath_PrimitiveRoot(t *testing.T) {
	_, ok := getValueByJSONPath(42, "field")
	assert.False(t, ok)
}

// ============================================================
// setReflectValue
// ============================================================

func TestSetReflectValue_String(t *testing.T) {
	s := struct{ V string }{}
	rv := reflect.ValueOf(&s).Elem().Field(0)
	setReflectValue(rv, "hello")
	assert.Equal(t, "hello", s.V)
}

func TestSetReflectValue_StringFromInt(t *testing.T) {
	s := struct{ V string }{}
	rv := reflect.ValueOf(&s).Elem().Field(0)
	setReflectValue(rv, 42)
	assert.Equal(t, "42", s.V)
}

func TestSetReflectValue_Float64(t *testing.T) {
	s := struct{ V float64 }{}
	rv := reflect.ValueOf(&s).Elem().Field(0)
	setReflectValue(rv, float64(3.14))
	assert.InDelta(t, 3.14, s.V, 0.0001)
}

func TestSetReflectValue_Float32(t *testing.T) {
	s := struct{ V float32 }{}
	rv := reflect.ValueOf(&s).Elem().Field(0)
	setReflectValue(rv, float32(2.5))
	assert.InDelta(t, 2.5, s.V, 0.0001)
}

func TestSetReflectValue_Int(t *testing.T) {
	s := struct{ V int }{}
	rv := reflect.ValueOf(&s).Elem().Field(0)
	setReflectValue(rv, float64(7))
	assert.Equal(t, 7, s.V)
}

func TestSetReflectValue_Int64(t *testing.T) {
	s := struct{ V int64 }{}
	rv := reflect.ValueOf(&s).Elem().Field(0)
	setReflectValue(rv, float64(99))
	assert.Equal(t, int64(99), s.V)
}

func TestSetReflectValue_BoolDirect(t *testing.T) {
	s := struct{ V bool }{}
	rv := reflect.ValueOf(&s).Elem().Field(0)
	setReflectValue(rv, true)
	assert.True(t, s.V)
}

func TestSetReflectValue_BoolFromStringTrue(t *testing.T) {
	s := struct{ V bool }{}
	rv := reflect.ValueOf(&s).Elem().Field(0)
	setReflectValue(rv, "1")
	assert.True(t, s.V)
}

func TestSetReflectValue_BoolFromStringFalse(t *testing.T) {
	s := struct{ V bool }{}
	rv := reflect.ValueOf(&s).Elem().Field(0)
	setReflectValue(rv, "false")
	assert.False(t, s.V)
}

func TestSetReflectValue_Interface(t *testing.T) {
	s := struct{ V any }{}
	rv := reflect.ValueOf(&s).Elem().Field(0)
	setReflectValue(rv, "anything")
	assert.Equal(t, "anything", s.V)
}

func TestSetReflectValue_StructFromMap(t *testing.T) {
	type Inner struct {
		Level string `json:"level"`
	}
	s := struct{ V Inner }{}
	rv := reflect.ValueOf(&s).Elem().Field(0)
	setReflectValue(rv, map[string]any{"level": "warning"})
	assert.Equal(t, "warning", s.V.Level)
}

func TestSetReflectValue_NilPtrValue(t *testing.T) {
	s := struct{ V string }{V: "original"}
	rv := reflect.ValueOf(&s).Elem().Field(0)
	var nilPtr *string
	setReflectValue(rv, nilPtr) // should set zero value
	assert.Equal(t, "", s.V)
}

func TestSetReflectValue_PtrField(t *testing.T) {
	s := struct{ V *string }{}
	rv := reflect.ValueOf(&s).Elem().Field(0)
	val := "pointed"
	setReflectValue(rv, val)
	require.NotNil(t, s.V)
	assert.Equal(t, "pointed", *s.V)
}

// ============================================================
// setNestedDataField
// ============================================================

func TestSetNestedDataField_NilData(t *testing.T) {
	msg := &Message{}
	setNestedDataField(msg, "level", "warning")
	m, ok := msg.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "warning", m["level"])
}

func TestSetNestedDataField_MapStringAny(t *testing.T) {
	msg := &Message{Data: map[string]any{"existing": "val"}}
	setNestedDataField(msg, "level", "critical")
	m, ok := msg.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "critical", m["level"])
	assert.Equal(t, "val", m["existing"]) // preserved
}

func TestSetNestedDataField_StructData(t *testing.T) {
	type Payload struct {
		Level string `json:"level"`
		Name  string `json:"name"`
	}
	msg := &Message{Data: Payload{Level: "ok", Name: "svc"}}
	setNestedDataField(msg, "level", "warning")
	p, ok := msg.Data.(Payload)
	require.True(t, ok)
	assert.Equal(t, "warning", p.Level)
	assert.Equal(t, "svc", p.Name) // preserved
}

func TestSetNestedDataField_PointerToStruct(t *testing.T) {
	type Payload struct {
		Level string `json:"level"`
	}
	msg := &Message{Data: &Payload{Level: "ok"}}
	setNestedDataField(msg, "level", "critical")
	p, ok := msg.Data.(*Payload)
	require.True(t, ok)
	assert.Equal(t, "critical", p.Level)
}

func TestSetNestedDataField_NilPointerData(t *testing.T) {
	type Payload struct{ Level string }
	var p *Payload
	msg := &Message{Data: p}
	setNestedDataField(msg, "level", "warn")
	m, ok := msg.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "warn", m["level"])
}

func TestSetNestedDataField_PrimitiveData(t *testing.T) {
	msg := &Message{Data: 42}
	setNestedDataField(msg, "level", "warn")
	m, ok := msg.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "warn", m["level"])
}

// ============================================================
// applyUpdatesToMessage
// ============================================================

func TestApplyUpdatesToMessage_Nil(t *testing.T) {
	msg := &Message{Status: "ok"}
	err := applyUpdatesToMessage(msg, nil)
	require.NoError(t, err)
	assert.Equal(t, "ok", msg.Status)
}

func TestApplyUpdatesToMessage_SetDataKey(t *testing.T) {
	msg := &Message{}
	err := applyUpdatesToMessage(msg, map[string]any{"data": map[string]any{"k": "v"}})
	require.NoError(t, err)
	m, ok := msg.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "v", m["k"])
}

func TestApplyUpdatesToMessage_SetDataType(t *testing.T) {
	msg := &Message{}
	err := applyUpdatesToMessage(msg, map[string]any{"data-type": "my.type.v1"})
	require.NoError(t, err)
	assert.Equal(t, "my.type.v1", msg.DataType)
}

func TestApplyUpdatesToMessage_SetDataTypeCamel(t *testing.T) {
	msg := &Message{}
	err := applyUpdatesToMessage(msg, map[string]any{"dataType": "other.type.v1"})
	require.NoError(t, err)
	assert.Equal(t, "other.type.v1", msg.DataType)
}

func TestApplyUpdatesToMessage_SetNestedDataField(t *testing.T) {
	msg := &Message{Data: map[string]any{"level": "ok"}}
	err := applyUpdatesToMessage(msg, map[string]any{"data.level": "critical"})
	require.NoError(t, err)
	m, ok := msg.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "critical", m["level"])
}

func TestApplyUpdatesToMessage_TopLevelField(t *testing.T) {
	msg := &Message{Status: "ok"}
	err := applyUpdatesToMessage(msg, map[string]any{"status": "critical"})
	require.NoError(t, err)
	assert.Equal(t, "critical", msg.Status)
}

func TestApplyUpdatesToMessage_UnknownFieldSkipped(t *testing.T) {
	msg := &Message{Status: "ok"}
	err := applyUpdatesToMessage(msg, map[string]any{"nonexistentField": "value"})
	require.NoError(t, err)
	assert.Equal(t, "ok", msg.Status) // unchanged
}

// ============================================================
// PreprocessMessenger delegation methods
// ============================================================

// trackingMessenger records calls to delegation methods for verification.
type trackingMessenger struct {
	captureMessenger
	readerStateCall *[4]any
	seekToEndCall   *[2]string
	dataDirCall     *string
	hostnameCall    *string
	statsReturn     MessengerStats
	registryReturn  PayloadRegistry
}

func (m *trackingMessenger) SetReaderState(ch, reader, file string, offset int64) error {
	m.readerStateCall = &[4]any{ch, reader, file, offset}
	return nil
}
func (m *trackingMessenger) SeekToEnd(ch, reader string) error {
	m.seekToEndCall = &[2]string{ch, reader}
	return nil
}
func (m *trackingMessenger) SetDataDir(dir string)                { m.dataDirCall = &dir }
func (m *trackingMessenger) SetHostname(h string)                 { m.hostnameCall = &h }
func (m *trackingMessenger) GetStats() MessengerStats             { return m.statsReturn }
func (m *trackingMessenger) GetPayloadRegistry() PayloadRegistry  { return m.registryReturn }
func (m *trackingMessenger) SetPayloadRegistry(_ PayloadRegistry) {}

func TestPreprocessMessenger_SetReaderState(t *testing.T) {
	inner := &trackingMessenger{}
	pm := NewPreprocessMessenger(inner, nil, nil)
	require.NoError(t, pm.SetReaderState("ch", "reader", "file.log", 42))
	require.NotNil(t, inner.readerStateCall)
	assert.Equal(t, [4]any{"ch", "reader", "file.log", int64(42)}, *inner.readerStateCall)
}

func TestPreprocessMessenger_SeekToEnd(t *testing.T) {
	inner := &trackingMessenger{}
	pm := NewPreprocessMessenger(inner, nil, nil)
	require.NoError(t, pm.SeekToEnd("ch", "reader"))
	require.NotNil(t, inner.seekToEndCall)
	assert.Equal(t, [2]string{"ch", "reader"}, *inner.seekToEndCall)
}

func TestPreprocessMessenger_SetDataDir(t *testing.T) {
	inner := &trackingMessenger{}
	pm := NewPreprocessMessenger(inner, nil, nil)
	pm.SetDataDir("/tmp/data")
	require.NotNil(t, inner.dataDirCall)
	assert.Equal(t, "/tmp/data", *inner.dataDirCall)
}

func TestPreprocessMessenger_SetHostname(t *testing.T) {
	inner := &trackingMessenger{}
	pm := NewPreprocessMessenger(inner, nil, nil)
	pm.SetHostname("myhost")
	require.NotNil(t, inner.hostnameCall)
	assert.Equal(t, "myhost", *inner.hostnameCall)
}

func TestPreprocessMessenger_GetStats(t *testing.T) {
	inner := &trackingMessenger{statsReturn: MessengerStats{TotalMessageCount: 5}}
	pm := NewPreprocessMessenger(inner, nil, nil)
	stats := pm.GetStats()
	assert.Equal(t, int64(5), stats.TotalMessageCount)
}

func TestPreprocessMessenger_GetPayloadRegistry(t *testing.T) {
	reg := NewPayloadRegistry(nil)
	inner := &trackingMessenger{registryReturn: reg}
	pm := NewPreprocessMessenger(inner, nil, nil)
	assert.Equal(t, reg, pm.GetPayloadRegistry())
}

func TestPreprocessMessenger_SetPayloadRegistry(_ *testing.T) {
	inner := &trackingMessenger{}
	pm := NewPreprocessMessenger(inner, nil, nil)
	// Just verify it doesn't panic — delegates to inner.
	pm.SetPayloadRegistry(NewPayloadRegistry(nil))
}

// ============================================================
// SubscribeExtended
// ============================================================

// extendedSubscribeCapture captures the extended handler passed to SubscribeExtended.
type extendedSubscribeCapture struct {
	captureMessenger
	handler func(func(Message, string, int64) error)
}

func (e *extendedSubscribeCapture) SubscribeExtended(_ context.Context, _, _, _, _ string, _ time.Duration, h func(Message, string, int64) error) error {
	if e.handler != nil {
		e.handler(h)
	}
	return nil
}

func TestPreprocessMessenger_SubscribeExtended_NoConditions(t *testing.T) {
	var received []Message
	handler := func(msg Message, _ string, _ int64) error {
		received = append(received, msg)
		return nil
	}
	var captured func(Message, string, int64) error
	inner := &extendedSubscribeCapture{handler: func(h func(Message, string, int64) error) { captured = h }}
	pm := NewPreprocessMessenger(inner, nil, nil)
	require.NoError(t, pm.SubscribeExtended(context.Background(), "src", "ch", "t", "n", 0, handler))
	require.NoError(t, captured(Message{Status: "ok"}, "file.log", 100))
	require.Len(t, received, 1)
	assert.Equal(t, "ok", received[0].Status)
}

func TestPreprocessMessenger_SubscribeExtended_Match(t *testing.T) {
	subConds := []ConditionConfig{
		{Field: "status", Operator: "eq", Value: "warn", Updates: map[string]any{"summary": "processed"}},
	}
	var received []Message
	handler := func(msg Message, _ string, _ int64) error {
		received = append(received, msg)
		return nil
	}
	var captured func(Message, string, int64) error
	inner := &extendedSubscribeCapture{handler: func(h func(Message, string, int64) error) { captured = h }}
	pm := NewPreprocessMessenger(inner, subConds, nil)
	require.NoError(t, pm.SubscribeExtended(context.Background(), "src", "ch", "t", "n", 0, handler))
	require.NoError(t, captured(Message{Status: "warn"}, "f", 0))
	require.Len(t, received, 1)
	assert.Equal(t, "processed", received[0].Summary)
}

func TestPreprocessMessenger_SubscribeExtended_Drop(t *testing.T) {
	subConds := []ConditionConfig{
		{Field: "status", Operator: "eq", Value: "warn"},
	}
	var received []Message
	handler := func(msg Message, _ string, _ int64) error {
		received = append(received, msg)
		return nil
	}
	var captured func(Message, string, int64) error
	inner := &extendedSubscribeCapture{handler: func(h func(Message, string, int64) error) { captured = h }}
	pm := NewPreprocessMessenger(inner, subConds, nil)
	require.NoError(t, pm.SubscribeExtended(context.Background(), "src", "ch", "t", "n", 0, handler))
	require.NoError(t, captured(Message{Status: "ok"}, "f", 0)) // no match → drop
	assert.Empty(t, received)
}
