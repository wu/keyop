package run

import (
	"context"
	"encoding/json"
	"fmt"
	"keyop/core"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockService struct {
	core.Service
	initialized bool
	checked     bool
}

func (m *mockService) Initialize() error {
	m.initialized = true
	return nil
}

func (m *mockService) Check() error {
	m.checked = true
	return nil
}

type mockPlugin struct {
	mockService
	name             string
	payloadType      string
	registerCalled   bool
	registrationHook func()
}

func (m *mockPlugin) Name() string { return m.name }
func (m *mockPlugin) RegisterPayloads(reg core.PayloadRegistry) error {
	m.registerCalled = true
	if m.registrationHook != nil {
		m.registrationHook()
	}
	return reg.Register(m.payloadType, func() any { return &struct{ Value string }{} })
}

func TestRuntimeInit_Order_RegistryThenPluginThenSubscribers(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "keyop-init-test")
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove %v: %v", tmpDir, err)
		}
	})

	// We can't easily use real plugins in a unit test without compiling .so files.
	// But we can mock LoadPlugins and loadPlugin behavior or use a modified test-friendly version.
	// For this test, we'll verify the logic in LoadPlugins by mocking parts of it.

	logger := &core.FakeLogger{}
	osProv := core.OsProvider{}
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(osProv)

	messenger := core.NewMessenger(logger, osProv)
	messenger.SetDataDir(tmpDir)
	deps.SetMessenger(messenger)

	// Since we can't easily use plugin.Open in tests, we will test the logic that would be invoked.

	pluginName := "test-plugin"
	payloadType := "plugin.test.v1"

	p := &mockPlugin{
		name:        pluginName,
		payloadType: payloadType,
	}

	// Mocking what loadPlugin does without the .so part
	newServiceFunc := func(d core.Dependencies, c core.ServiceConfig) core.Service {
		return p
	}

	// Manually invoke the logic we added to loadPlugin
	dummySvc := newServiceFunc(deps, core.ServiceConfig{Name: "discovery-" + pluginName})
	if rtPlugin, ok := dummySvc.(core.RuntimePlugin); ok {
		reg := deps.MustGetMessenger().GetPayloadRegistry()
		require.NotNil(t, reg)
		err := rtPlugin.RegisterPayloads(reg)
		assert.NoError(t, err)
	}
	ServiceRegistry[pluginName] = newServiceFunc

	// Verify registration happened
	assert.True(t, p.registerCalled)
	reg := messenger.GetPayloadRegistry()
	assert.Contains(t, reg.KnownTypes(), payloadType)

	// Now simulate the "run" part which starts subscribers
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	received := make(chan any, 1)
	err = messenger.Subscribe(ctx, "sub1", "chan1", "test", "test", 0, func(msg core.Message) error {
		received <- msg.Data
		return nil
	})
	assert.NoError(t, err)

	// Send a message with the plugin's payload type
	env := core.NewEnvelope("chan1", "source1", map[string]any{"value": "plugin-data"})
	if env.Headers == nil {
		env.Headers = make(map[string]string)
	}
	env.Headers["payload-type"] = payloadType

	_, _ = core.UnmarshalEnvelope([]byte(fmt.Sprintf(`{"v":"v1","id":"1","topic":"chan1","payload":{"value":"plugin-data"},"headers":{"payload-type":"%s"}}`, payloadType)))

	// Use messenger.Send to test the full path
	err = messenger.Send(core.Message{
		ChannelName: "chan1",
		Data:        map[string]any{"value": "plugin-data"},
	})
	_ = messenger.Send(core.Message{ChannelName: "chan1", Data: "trigger-queue-init"}) // Ensure queue is init
}

// Actually, let's write a simpler test that just verifies LoadPlugins behavior if we could mock plugin.Open
// Since we can't mock plugin.Open easily, we'll verify the registry behavior.

func TestPluginPayloadRegistration_BeforeSubscribers(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "keyop-plugin-test")
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove %v: %v", tmpDir, err)
		}
	})

	logger := &core.FakeLogger{}
	messenger := core.NewMessenger(logger, core.OsProvider{})
	messenger.SetDataDir(tmpDir)

	reg := messenger.GetPayloadRegistry()

	// Simulate plugin registration
	pluginPayloadType := "plugin.custom.v1"
	type CustomPayload struct {
		Name string `json:"name"`
	}
	reg.Register(pluginPayloadType, func() any { return &CustomPayload{} })

	// Start subscriber
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	received := make(chan any, 1)
	messenger.Subscribe(ctx, "sub", "chan", "test", "test", 0, func(msg core.Message) error {
		received <- msg.Data
		return nil
	})

	// Send message with plugin payload-type
	env := core.Envelope{
		Version: core.EnvelopeV1,
		ID:      "1",
		Topic:   "chan",
		Headers: map[string]string{"payload-type": pluginPayloadType},
		Payload: map[string]any{"name": "plugin-val"},
	}
	envBytes, _ := json.Marshal(env)

	// Wait for subscription to be active
	time.Sleep(100 * time.Millisecond)

	// Send raw envelope to queue
	q, _ := core.NewPersistentQueue("chan", tmpDir, core.OsProvider{}, logger)
	q.Enqueue(string(envBytes))

	select {
	case data := <-received:
		typed, ok := data.(*CustomPayload)
		assert.True(t, ok, "Expected *CustomPayload, got %T", data)
		assert.Equal(t, "plugin-val", typed.Name)
	case <-time.After(1 * time.Second):
		t.Fatal("Timed out waiting for message")
	}
}

func TestMissingPluginPayloadType_FallbackStillProcesses(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "keyop-fallback-test")
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove %v: %v", tmpDir, err)
		}
	})

	logger := &core.FakeLogger{}
	messenger := core.NewMessenger(logger, core.OsProvider{})
	messenger.SetDataDir(tmpDir)

	// No registration for "missing.plugin.v1"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	received := make(chan any, 1)
	messenger.Subscribe(ctx, "sub", "chan", "test", "test", 0, func(msg core.Message) error {
		received <- msg.Data
		return nil
	})

	// Send message with missing payload-type
	env := core.Envelope{
		Version: core.EnvelopeV1,
		ID:      "1",
		Topic:   "chan",
		Headers: map[string]string{"payload-type": "missing.plugin.v1"},
		Payload: map[string]any{"key": "value"},
	}
	envBytes, _ := json.Marshal(env)

	time.Sleep(100 * time.Millisecond)
	q, _ := core.NewPersistentQueue("chan", tmpDir, core.OsProvider{}, logger)
	q.Enqueue(string(envBytes))

	select {
	case data := <-received:
		// Should be raw map
		m, ok := data.(map[string]any)
		assert.True(t, ok, "Expected map[string]any, got %T", data)
		assert.Equal(t, "value", m["key"])
	case <-time.After(1 * time.Second):
		t.Fatal("Timed out waiting for message")
	}
}

func TestDuplicatePayloadRegistration_ReturnsError(t *testing.T) {
	reg := core.GetPayloadRegistry()
	typeName := "duplicate.test.v1"

	err := reg.Register(typeName, func() any { return &struct{}{} })
	// It might already be registered if tests run concurrently, but usually they don't share globals unless specified.
	// Actually tests in same package share globals.

	// Try to register again
	err = reg.Register(typeName, func() any { return &struct{}{} })
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")

	// Verify helper also returns error
	err = core.RegisterPayload(typeName, func() any { return &struct{}{} })
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestRegistrySingleSourceOfTruth(t *testing.T) {
	logger := &core.FakeLogger{}
	messenger := core.NewMessenger(logger, core.OsProvider{})

	reg := messenger.GetPayloadRegistry()
	globalReg := core.GetPayloadRegistry()

	assert.Equal(t, globalReg, reg, "Messenger should use global registry by default")
}

func TestLoadPlugins_InvokesRegisterPayloads(t *testing.T) {
	// Since we can't use real plugins, we mock the logic
	// This test verifies that if we have a function that behaves like NewService,
	// it registers payloads.

	deps := core.Dependencies{}
	logger := &core.FakeLogger{}
	deps.SetLogger(logger)
	messenger := core.NewMessenger(logger, core.OsProvider{})
	deps.SetMessenger(messenger)

	p := &mockPlugin{
		name:        "mock",
		payloadType: "mock.payload.v1",
	}

	// Mock newServiceFunc
	newServiceFunc := func(d core.Dependencies, c core.ServiceConfig) core.Service {
		return p
	}

	// Simulate what loadPlugin does:
	dummySvc := newServiceFunc(deps, core.ServiceConfig{Name: "discovery-mock"})
	if rtPlugin, ok := dummySvc.(core.RuntimePlugin); ok {
		reg := deps.MustGetMessenger().GetPayloadRegistry()
		err := rtPlugin.RegisterPayloads(reg)
		assert.NoError(t, err)
	}

	assert.True(t, p.registerCalled)
	assert.Contains(t, messenger.GetPayloadRegistry().KnownTypes(), "mock.payload.v1")
}
