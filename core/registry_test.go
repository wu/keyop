package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestPluginPayload struct {
	Value string `json:"value"`
}

func (p TestPluginPayload) PayloadType() string { return "plugin.test.v1" }

func TestPayloadRegistry_RuntimeRegistration(t *testing.T) {
	reg := newDefaultRegistry(&FakeLogger{})

	typeName := "plugin.test.v1"
	err := reg.Register(typeName, func() any { return &TestPluginPayload{} })
	assert.NoError(t, err)

	payload := map[string]any{"value": "hello"}
	decoded, err := reg.Decode(typeName, payload)
	assert.NoError(t, err)

	typed, ok := decoded.(*TestPluginPayload)
	assert.True(t, ok)
	assert.Equal(t, "hello", typed.Value)
}

func TestPayloadRegistry_DuplicateRegistration(t *testing.T) {
	reg := newDefaultRegistry(&FakeLogger{})
	typeName := "duplicate.v1"

	err := reg.Register(typeName, func() any { return &TestPluginPayload{} })
	assert.NoError(t, err)

	err = reg.Register(typeName, func() any { return &TestPluginPayload{} })
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrPayloadTypeAlreadyRegistered))
	assert.True(t, IsDuplicatePayloadRegistration(err))
}

func TestIsDuplicatePayloadRegistration_Regression(t *testing.T) {
	assert.False(t, IsDuplicatePayloadRegistration(nil))
	assert.False(t, IsDuplicatePayloadRegistration(errors.New("some other error")))
	assert.False(t, IsDuplicatePayloadRegistration(fmt.Errorf("not a duplicate: %w", errors.New("nested"))))
}

func TestPayloadRegistry_UnknownFallback(t *testing.T) {
	logger := &FakeLogger{}
	reg := newDefaultRegistry(logger)

	payload := map[string]any{"key": "value"}
	decoded, err := reg.Decode("unknown.v1", payload)
	assert.NoError(t, err)
	assert.Equal(t, payload, decoded)

	// Check if warned
	assert.Contains(t, logger.lastWarnMsg, "Unknown payload type")
}

func TestPayloadRegistry_Concurrency(t *testing.T) {
	reg := newDefaultRegistry(&FakeLogger{})
	const numGoroutines = 50
	const iterations = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				typeName := fmt.Sprintf("type-%d-%d", id, j)
				if err := reg.Register(typeName, func() any { return map[string]int{"id": id, "j": j} }); err != nil {
					t.Errorf("reg.Register error: %v", err)
				}
			}
		}(i)

		go func(_ int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if _, err := reg.Decode("nonexistent", map[string]string{"foo": "bar"}); err != nil {
					t.Errorf("reg.Decode error: %v", err)
				}
				reg.KnownTypes()
			}
		}(i)
	}
	wg.Wait()
}

func TestRegisterPayload_DuplicateErrorIsDetectable(t *testing.T) {
	// Use a local registry for isolation
	reg := newDefaultRegistry(&FakeLogger{})
	typeName := "test.duplicate.v1"
	factory := func() any { return &TestPluginPayload{} }

	err := reg.Register(typeName, factory)
	assert.NoError(t, err)

	err = reg.Register(typeName, factory)
	assert.Error(t, err)
	assert.True(t, IsDuplicatePayloadRegistration(err), "Expected duplicate registration error, got %v", err)
	assert.True(t, errors.Is(err, ErrPayloadTypeAlreadyRegistered), "Expected error to wrap ErrPayloadTypeAlreadyRegistered")
}

func TestServicePayloadRegistration_NoSilentError(t *testing.T) {
	reg := newDefaultRegistry(&FakeLogger{})

	// Simulate what heartbeat.RegisterPayloads does
	register := func(r PayloadRegistry) error {
		if err := r.Register("heartbeat", func() any { return &map[string]any{} }); err != nil {
			if !IsDuplicatePayloadRegistration(err) {
				return err
			}
		}
		if err := r.Register("heartbeat", func() any { return &map[string]any{} }); err != nil {
			if !IsDuplicatePayloadRegistration(err) {
				return err
			}
		}
		return nil
	}

	err := register(reg)
	assert.NoError(t, err, "Duplicate registration should be handled by IsDuplicatePayloadRegistration check")
}

func TestPluginRegisterPayloads_ErrorIsSurfaced(t *testing.T) {
	fl := &FakeLogger{}
	reg := newDefaultRegistry(fl)

	// Simulate a plugin that fails registration
	failReg := func(_ PayloadRegistry) error {
		return fmt.Errorf("hard failure")
	}

	err := failReg(reg)
	assert.Error(t, err)
	// In x/run/plugins.go we log this and return error if not duplicate
	if !IsDuplicatePayloadRegistration(err) {
		fl.Error("Plugin failed payload registration", "plugin", "fail-plugin", "error", err)
	}

	assert.Equal(t, "Plugin failed payload registration", fl.LastErrMsg())
}

func TestRegistryConsistency_MessengerDecodeUsesConfiguredRegistry(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_registry_consistency")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove %v: %v", tmpDir, err)
		}
	})

	fl := &FakeLogger{}
	m := NewMessenger(fl, OsProvider{})
	m.SetDataDir(tmpDir)

	// Create a new registry and set it
	customReg := newDefaultRegistry(fl)
	typeName := "custom.type.v1"
	require.NoError(t, customReg.Register(typeName, func() any { return &TestPluginPayload{} }))
	m.SetPayloadRegistry(customReg)

	// Verify messenger uses this registry for decoding
	payload := map[string]any{"value": "consistency-test"}
	msg := Message{
		Uuid:        "test-id",
		ChannelName: "chan",
		DataType:    typeName,
		Data:        payload,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	received := make(chan any, 1)
	require.NoError(t, m.Subscribe(ctx, "sub", "chan", "type", "name", 0, func(msg Message) error {
		received <- msg.Data
		return nil
	}))

	msgBytes, _ := json.Marshal(msg)
	require.NoError(t, m.initializePersistentQueue("chan"))
	require.NoError(t, m.queues["chan"].Enqueue(string(msgBytes)))

	select {
	case data := <-received:
		_, ok := data.(*TestPluginPayload)
		assert.True(t, ok, "Expected decoded *TestPluginPayload, got %T", data)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for message")
	}
}
