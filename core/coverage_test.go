package core_test

import (
	"context"
	"github.com/wu/keyop/core"
	"github.com/wu/keyop/core/adapter"
	"github.com/wu/keyop/core/testutil"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Dependencies - StateStore tests
func TestDependencies_StateStore_SetAndGet(t *testing.T) {
	var d core.Dependencies
	store := &testutil.NoOpStateStore{}

	d.SetStateStore(store)
	got := d.GetStateStore()

	assert.Equal(t, store, got)
}

func TestDependencies_StateStore_MustGetPanicsWhenUnset(t *testing.T) {
	var d core.Dependencies
	assert.Panics(t, func() { _ = d.MustGetStateStore() })
}

func TestDependencies_StateStore_MustGetReturnsWhenSet(t *testing.T) {
	var d core.Dependencies
	store := &testutil.NoOpStateStore{}
	d.SetStateStore(store)

	got := d.MustGetStateStore()
	assert.Equal(t, store, got)
}

func TestDependencies_StateStore_GetReturnsNilWhenUnset(t *testing.T) {
	var d core.Dependencies
	got := d.GetStateStore()
	assert.Nil(t, got)
}

// Dependencies - Messenger tests
func TestDependencies_Messenger_SetAndGet(t *testing.T) {
	var d core.Dependencies
	messenger := testutil.NewFakeMessenger()

	d.SetMessenger(messenger)
	got := d.MustGetMessenger()

	assert.Equal(t, messenger, got)
}

func TestDependencies_Messenger_MustGetPanicsWhenUnset(t *testing.T) {
	var d core.Dependencies
	assert.PanicsWithValue(t, "ERROR: messenger is not initialized", func() {
		_ = d.MustGetMessenger()
	})
}

func TestDependencies_Messenger_MustGetReturnsWhenSet(t *testing.T) {
	var d core.Dependencies
	messenger := testutil.NewFakeMessenger()
	d.SetMessenger(messenger)

	got := d.MustGetMessenger()
	assert.Equal(t, messenger, got)
}

// Event PayloadType tests
func TestMetricEvent_PayloadType(t *testing.T) {
	evt := &core.MetricEvent{}
	assert.Equal(t, "core.metric.v1", evt.PayloadType())
}

func TestAlertEvent_PayloadType(t *testing.T) {
	evt := &core.AlertEvent{}
	assert.Equal(t, "core.alert.v1", evt.PayloadType())
}

func TestStatusEvent_PayloadType(t *testing.T) {
	evt := &core.StatusEvent{}
	assert.Equal(t, "core.status.v1", evt.PayloadType())
}

func TestErrorEvent_PayloadType(t *testing.T) {
	evt := &core.ErrorEvent{}
	assert.Equal(t, "core.error.v1", evt.PayloadType())
}

func TestTempEvent_PayloadType(t *testing.T) {
	evt := &core.TempEvent{}
	assert.Equal(t, "core.temp.v1", evt.PayloadType())
}

func TestDeviceStatusEvent_PayloadType(t *testing.T) {
	evt := &core.DeviceStatusEvent{}
	assert.Equal(t, "core.device.status.v1", evt.PayloadType())
}

func TestSwitchEvent_PayloadType(t *testing.T) {
	evt := &core.SwitchEvent{}
	assert.Equal(t, "core.switch.v1", evt.PayloadType())
}

func TestSwitchCommand_PayloadType(t *testing.T) {
	cmd := &core.SwitchCommand{}
	assert.Equal(t, "core.switch.command.v1", cmd.PayloadType())
}

func TestWeatherStationEvent_PayloadType(t *testing.T) {
	evt := &core.WeatherStationEvent{}
	assert.Equal(t, "weatherstation.event.v1", evt.PayloadType())
}

func TestGpsEvent_PayloadType(t *testing.T) {
	evt := &core.GpsEvent{}
	assert.Equal(t, "core.gps.v1", evt.PayloadType())
}

// ExtractAlertEvent tests
func TestExtractAlertEvent_FromStructWithAlert(t *testing.T) {
	alert := &core.AlertEvent{
		Summary: "test alert",
		Text:    "test text",
	}

	type customEvent struct {
		Alert core.AlertEvent
		Field string
	}

	evt := customEvent{
		Alert: *alert,
		Field: "value",
	}

	extracted, ok := core.ExtractAlertEvent(evt)
	assert.True(t, ok)
	assert.NotNil(t, extracted)
	assert.Equal(t, "test alert", extracted.Summary)
	assert.Equal(t, "test text", extracted.Text)
}

func TestExtractAlertEvent_FromStructWithoutAlert(t *testing.T) {
	type customEvent struct {
		Field string
	}

	evt := customEvent{Field: "value"}

	extracted, ok := core.ExtractAlertEvent(evt)
	assert.False(t, ok)
	assert.Nil(t, extracted)
}

func TestExtractAlertEvent_DirectAlertEvent(t *testing.T) {
	alert := core.AlertEvent{
		Summary: "direct alert",
		Text:    "direct text",
	}

	extracted, ok := core.ExtractAlertEvent(alert)
	assert.True(t, ok)
	assert.NotNil(t, extracted)
	assert.Equal(t, "direct alert", extracted.Summary)
}

func TestExtractAlertEvent_NilInput(t *testing.T) {
	extracted, ok := core.ExtractAlertEvent(nil)
	assert.False(t, ok)
	assert.Nil(t, extracted)
}

// IsDuplicatePayloadRegistration tests
func TestIsDuplicatePayloadRegistration_WithDuplicateError(t *testing.T) {
	err := core.ErrPayloadTypeAlreadyRegistered
	assert.True(t, core.IsDuplicatePayloadRegistration(err))
}

func TestIsDuplicatePayloadRegistration_WithOtherError(t *testing.T) {
	assert.False(t, core.IsDuplicatePayloadRegistration(assert.AnError))
}

func TestIsDuplicatePayloadRegistration_WithNilError(t *testing.T) {
	assert.False(t, core.IsDuplicatePayloadRegistration(nil))
}

// RegisterService and LookupService tests
func TestRegisterService_AndLookupService(t *testing.T) {
	// Use a unique name to avoid conflicts with other tests
	serviceName := "test_service_" + t.Name()

	testConstructor := func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return &testutil.NoOpStateStore{}
	}

	core.RegisterService(serviceName, testConstructor)

	got, found := core.LookupService(serviceName)
	assert.True(t, found)
	assert.NotNil(t, got)
}

func TestLookupService_NotFound(t *testing.T) {
	_, found := core.LookupService("nonexistent_service_" + t.Name())
	assert.False(t, found)
}

// AsType tests
func TestAsType_MatchingType(t *testing.T) {
	testErr := assert.AnError
	got, ok := core.AsType[error](testErr)

	assert.True(t, ok)
	assert.Equal(t, testErr, got)
}

func TestAsType_NonMatchingType(t *testing.T) {
	testVal := "string value"
	_, ok := core.AsType[int](testVal)

	assert.False(t, ok)
}

func TestAsType_WithCustomType(t *testing.T) {
	type customType struct {
		value string
	}

	custom := customType{value: "test"}
	got, ok := core.AsType[customType](custom)

	assert.True(t, ok)
	assert.Equal(t, custom, got)
}

// NewUUID tests
func TestNewUUID_GeneratesValidUUID(t *testing.T) {
	id1 := core.NewUUID()
	id2 := core.NewUUID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2, "two UUIDs should be different")
}

func TestNewUUID_MultipleCalls_AllUnique(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := core.NewUUID()
		assert.False(t, ids[id], "UUID %s was already generated", id)
		ids[id] = true
	}
	assert.Equal(t, 100, len(ids), "should have 100 unique UUIDs")
}

// Integration test: FileStateStore with Dependencies
func TestDependencies_WithFileStateStore(t *testing.T) {
	tmpDir := t.TempDir()
	osProvider := adapter.OsProvider{}
	store := adapter.NewFileStateStore(tmpDir, osProvider)

	var deps core.Dependencies
	deps.SetStateStore(store)

	// Verify we can save and load through dependencies
	testKey := "test_key"
	testValue := map[string]string{"name": "test", "message": "hello"}

	err := deps.MustGetStateStore().Save(testKey, testValue)
	require.NoError(t, err)

	var loaded map[string]string
	err = deps.MustGetStateStore().Load(testKey, &loaded)
	require.NoError(t, err)
	assert.Equal(t, testValue, loaded)
}

// Integration test: event types with messenger
func TestEventTypes_InMessenger(t *testing.T) {
	// Test that we can publish different event types
	events := []interface{}{
		&core.MetricEvent{Name: "cpu", Value: 42.5},
		&core.AlertEvent{Summary: "test alert"},
		&core.StatusEvent{Name: "service", Status: "ok"},
		&core.ErrorEvent{Summary: "error occurred"},
		&core.TempEvent{TempC: 23.5},
	}

	for _, evt := range events {
		// Each event type has a PayloadType method
		typedEvt := evt.(interface{ PayloadType() string })
		payloadType := typedEvt.PayloadType()
		assert.NotEmpty(t, payloadType)
	}
}
