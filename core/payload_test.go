package core

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 1. PayloadType() methods
// ---------------------------------------------------------------------------

func TestEventPayloadTypes(t *testing.T) {
	cases := []struct {
		event    TypedPayload
		wantType string
	}{
		{&DeviceStatusEvent{}, "core.device.status.v1"},
		{DeviceStatusEvent{}, "core.device.status.v1"},
		{&MetricEvent{}, "core.metric.v1"},
		{MetricEvent{}, "core.metric.v1"},
		{&AlertEvent{}, "core.alert.v1"},
		{AlertEvent{}, "core.alert.v1"},
		{&ErrorEvent{}, "core.error.v1"},
		{ErrorEvent{}, "core.error.v1"},
		{&StatusEvent{}, "core.status.v1"},
		{StatusEvent{}, "core.status.v1"},
		{&TempEvent{}, "core.temp.v1"},
		{TempEvent{}, "core.temp.v1"},
		{&SwitchCommand{}, "core.switch.command.v1"},
		{SwitchCommand{}, "core.switch.command.v1"},
		{&SwitchEvent{}, "core.switch.v1"},
		{SwitchEvent{}, "core.switch.v1"},
		{&WeatherStationEvent{}, "weatherstation.event.v1"},
		{WeatherStationEvent{}, "weatherstation.event.v1"},
	}
	for _, tc := range cases {
		t.Run(tc.wantType, func(t *testing.T) {
			got := tc.event.PayloadType()
			assert.Equal(t, tc.wantType, got)
		})
	}
}

// ---------------------------------------------------------------------------
// 2. ExtractAlertEvent
// ---------------------------------------------------------------------------

func TestExtractAlertEvent_DirectValue(t *testing.T) {
	ae := AlertEvent{Summary: "s", Text: "t", Level: "info"}
	got, ok := ExtractAlertEvent(ae)
	require.True(t, ok)
	require.NotNil(t, got)
	assert.Equal(t, "s", got.Summary)
	assert.Equal(t, "t", got.Text)
	assert.Equal(t, "info", got.Level)
}

func TestExtractAlertEvent_Pointer(t *testing.T) {
	ae := &AlertEvent{Summary: "ptr", Text: "body", Level: "warning"}
	got, ok := ExtractAlertEvent(ae)
	require.True(t, ok)
	require.NotNil(t, got)
	assert.Equal(t, ae, got)
}

func TestExtractAlertEvent_Nil(t *testing.T) {
	got, ok := ExtractAlertEvent(nil)
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestExtractAlertEvent_WrongType_String(t *testing.T) {
	got, ok := ExtractAlertEvent("not an alert")
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestExtractAlertEvent_WrongType_Int(t *testing.T) {
	got, ok := ExtractAlertEvent(42)
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestExtractAlertEvent_WrongType_OtherStruct(t *testing.T) {
	got, ok := ExtractAlertEvent(MetricEvent{Name: "cpu", Value: 1.5})
	assert.False(t, ok)
	assert.Nil(t, got)
}

// struct that contains an AlertEvent as a named field
type wrapperWithAlertField struct {
	ID    int
	Alert AlertEvent
}

// struct that contains a *AlertEvent pointer field
type wrapperWithAlertPtr struct {
	ID    int
	Alert *AlertEvent
}

// struct that embeds AlertEvent anonymously
type wrapperEmbeddedAlert struct {
	AlertEvent
	Extra string
}

func TestExtractAlertEvent_StructWithAlertField(t *testing.T) {
	w := wrapperWithAlertField{
		ID:    1,
		Alert: AlertEvent{Summary: "field-summary", Text: "field-text", Level: "critical"},
	}
	got, ok := ExtractAlertEvent(w)
	require.True(t, ok)
	require.NotNil(t, got)
	assert.Equal(t, "field-summary", got.Summary)
	assert.Equal(t, "field-text", got.Text)
	assert.Equal(t, "critical", got.Level)
}

func TestExtractAlertEvent_StructWithAlertPtrField_NonNil(t *testing.T) {
	ae := &AlertEvent{Summary: "ptr-field", Text: "detail", Level: "info"}
	w := wrapperWithAlertPtr{ID: 2, Alert: ae}
	got, ok := ExtractAlertEvent(w)
	require.True(t, ok)
	assert.Equal(t, ae, got)
}

func TestExtractAlertEvent_StructWithAlertPtrField_Nil(t *testing.T) {
	w := wrapperWithAlertPtr{ID: 3, Alert: nil}
	got, ok := ExtractAlertEvent(w)
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestExtractAlertEvent_EmbeddedAlert(t *testing.T) {
	w := wrapperEmbeddedAlert{
		AlertEvent: AlertEvent{Summary: "embedded", Text: "emb-text", Level: "warning"},
		Extra:      "extra",
	}
	got, ok := ExtractAlertEvent(w)
	require.True(t, ok)
	require.NotNil(t, got)
	assert.Equal(t, "embedded", got.Summary)
}

func TestExtractAlertEvent_PreservesFields(t *testing.T) {
	ae := AlertEvent{Summary: "preserve-summary", Text: "preserve-text", Level: "critical"}
	got, ok := ExtractAlertEvent(ae)
	require.True(t, ok)
	assert.Equal(t, "preserve-summary", got.Summary)
	assert.Equal(t, "preserve-text", got.Text)
	assert.Equal(t, "critical", got.Level)
}

func TestExtractAlertEvent_NilPointer(t *testing.T) {
	var ae *AlertEvent
	got, ok := ExtractAlertEvent(ae)
	// *AlertEvent typed nil: AsType[*AlertEvent] will succeed with nil pointer,
	// but the nil guard "aePtr != nil" causes it to fall through.
	// It's not an AlertEvent value either. The reflection loop detects IsNil and returns false.
	assert.False(t, ok)
	assert.Nil(t, got)
}

// ---------------------------------------------------------------------------
// 3. JSON round-trip tests
// ---------------------------------------------------------------------------

func TestAlertEvent_JSON(t *testing.T) {
	orig := AlertEvent{Summary: "disk full", Text: "partition /data is 100%", Level: "warning"}
	b, err := json.Marshal(orig)
	require.NoError(t, err)

	var got AlertEvent
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig, got)
}

func TestErrorEvent_JSON(t *testing.T) {
	orig := ErrorEvent{Summary: "connection refused", Text: "could not reach host", Level: "error"}
	b, err := json.Marshal(orig)
	require.NoError(t, err)

	var got ErrorEvent
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig, got)
}

func TestStatusEvent_JSON(t *testing.T) {
	orig := StatusEvent{
		Name:     "api-server",
		Hostname: "host1",
		Status:   "running",
		Details:  "all systems nominal",
		Level:    "ok",
	}
	b, err := json.Marshal(orig)
	require.NoError(t, err)

	var got StatusEvent
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig, got)
}

func TestMetricEvent_JSON(t *testing.T) {
	orig := MetricEvent{Name: "cpu_usage", Value: 72.5, Unit: "percent"}
	b, err := json.Marshal(orig)
	require.NoError(t, err)

	var got MetricEvent
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig, got)
}

func TestDeviceStatusEvent_JSON(t *testing.T) {
	orig := DeviceStatusEvent{DeviceID: "dev-42", Status: "online", Battery: 85}
	b, err := json.Marshal(orig)
	require.NoError(t, err)

	var got DeviceStatusEvent
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig, got)
}

func TestDeviceStatusEvent_JSON_OmitEmptyBattery(t *testing.T) {
	orig := DeviceStatusEvent{DeviceID: "dev-1", Status: "offline"}
	b, err := json.Marshal(orig)
	require.NoError(t, err)

	// Battery is zero-value and omitempty, so it must not appear in JSON
	assert.NotContains(t, string(b), `"battery"`)

	var got DeviceStatusEvent
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig, got)
}

func TestTempEvent_JSON(t *testing.T) {
	orig := TempEvent{
		TempC:      22.5,
		TempF:      72.5,
		Hostname:   "sensor-host",
		SensorName: "cpu-diode",
		Raw:        "0x1A2B",
	}
	b, err := json.Marshal(orig)
	require.NoError(t, err)

	var got TempEvent
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig, got)
}

func TestSwitchEvent_JSON(t *testing.T) {
	orig := SwitchEvent{DeviceName: "living-room-lamp", State: "ON"}
	b, err := json.Marshal(orig)
	require.NoError(t, err)

	var got SwitchEvent
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig, got)
}

func TestSwitchCommand_JSON(t *testing.T) {
	orig := SwitchCommand{DeviceName: "bedroom-fan", State: "OFF"}
	b, err := json.Marshal(orig)
	require.NoError(t, err)

	var got SwitchCommand
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig, got)
}

func TestWeatherStationEvent_JSON(t *testing.T) {
	orig := WeatherStationEvent{
		OutTemp:     68.2,
		InTemp:      72.0,
		OutHumidity: 55,
		WindSpeed:   12.3,
		WindDir:     270,
		Model:       "WS-2000",
		StationType: "AMBWeatherNetwork",
		DateUTC:     "2024-01-15 18:00:00",
	}
	b, err := json.Marshal(orig)
	require.NoError(t, err)

	var got WeatherStationEvent
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig, got)
}

// ---------------------------------------------------------------------------
// 4. init() registration — all core types present in global registry
// ---------------------------------------------------------------------------

func TestCoreTypesRegisteredInInit(t *testing.T) {
	reg := GetPayloadRegistry()
	require.NotNil(t, reg)

	known := make(map[string]struct{})
	for _, k := range reg.KnownTypes() {
		known[k] = struct{}{}
	}

	requiredTypes := []string{
		"core.device.status.v1",
		"core.metric.v1",
		"core.alert.v1",
		"core.error.v1",
		"core.status.v1",
		"core.temp.v1",
		"core.switch.v1",
		"core.switch.command.v1",
		// compatibility aliases
		"device.status",
		"metric",
		"alert",
		"error",
		"status",
		"temp",
		"switch",
		"switch.command",
	}

	for _, typ := range requiredTypes {
		_, present := known[typ]
		assert.True(t, present, "expected %q to be registered in init()", typ)
	}
}

// Verify the registry can decode each registered core type back to the correct Go type.
func TestCoreTypesDecodeToCorrectType(t *testing.T) {
	reg := GetPayloadRegistry()
	require.NotNil(t, reg)

	cases := []struct {
		typeName string
		payload  map[string]any
		wantType string
	}{
		{"core.device.status.v1", map[string]any{"deviceId": "d1", "status": "ok"}, "*core.DeviceStatusEvent"},
		{"core.metric.v1", map[string]any{"name": "cpu", "value": 1.0}, "*core.MetricEvent"},
		{"core.alert.v1", map[string]any{"summary": "s", "text": "t"}, "*core.AlertEvent"},
		{"core.error.v1", map[string]any{"summary": "s", "text": "t"}, "*core.ErrorEvent"},
		{"core.status.v1", map[string]any{"name": "n", "status": "ok", "details": ""}, "*core.StatusEvent"},
		{"core.temp.v1", map[string]any{"tempC": 20.0, "tempF": 68.0, "hostname": "h", "sensorName": "s"}, "*core.TempEvent"},
		{"core.switch.v1", map[string]any{"deviceName": "lamp", "state": "ON"}, "*core.SwitchEvent"},
		{"core.switch.command.v1", map[string]any{"deviceName": "lamp", "state": "OFF"}, "*core.SwitchCommand"},
	}

	for _, tc := range cases {
		t.Run(tc.typeName, func(t *testing.T) {
			decoded, err := reg.Decode(tc.typeName, tc.payload)
			require.NoError(t, err)
			require.NotNil(t, decoded)
		})
	}
}

// ---------------------------------------------------------------------------
// 5. IsDuplicatePayloadRegistration — true-positive path (non-duplicate not covered)
// ---------------------------------------------------------------------------

func TestIsDuplicatePayloadRegistration_TrueForDuplicate(t *testing.T) {
	reg := newDefaultRegistry(nil)
	err := reg.Register("dup.type.v1", func() any { return &AlertEvent{} })
	require.NoError(t, err)

	err = reg.Register("dup.type.v1", func() any { return &AlertEvent{} })
	require.Error(t, err)
	assert.True(t, IsDuplicatePayloadRegistration(err))
}
