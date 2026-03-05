//nolint:revive
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessenger_DLQFailure_DoesNotAckOriginal(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_dlq_fail")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove %s: %v", tmpDir, err)
		}
	})

	fl := &FakeLogger{}
	// Use a fake OS provider to inject failure for DLQ channel only
	osProv := &FakeOsProvider{
		OpenFileFunc: func(name string, flag int, perm os.FileMode) (FileApi, error) {
			if strings.Contains(name, "_dlq.fail-chan") {
				return nil, errors.New("DLQ write failed")
			}
			return OsProvider{}.OpenFile(name, flag, perm)
		},
		MkdirAllFunc: func(path string, perm os.FileMode) error {
			return os.MkdirAll(path, perm)
		},
		StatFunc: func(name string) (os.FileInfo, error) {
			return os.Stat(name)
		},
		ReadDirFunc: func(name string) ([]os.DirEntry, error) {
			return os.ReadDir(name)
		},
		UserHomeDirFunc: func() (string, error) {
			return os.UserHomeDir()
		},
		Host: "test-host",
	}

	m := NewMessenger(fl, osProv)
	m.SetDataDir(tmpDir)
	m.maxRetryAttempts = 0 // DLQ on first failure

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	channel := "fail-chan"
	source := "test-sub"

	handlerCalled := make(chan struct{}, 1)
	require.NoError(t, m.Subscribe(ctx, source, channel, "test", "test", 0, func(msg Message) error {
		handlerCalled <- struct{}{}
		return errors.New("fail always")
	}))

	if err := m.Send(Message{ChannelName: channel, Text: "trigger"}); err != nil {

		assert.NoError(t, err)

	}
	// Handler should be called
	select {
	case <-handlerCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("Handler not called")
	}

	// Verify DLQ failure was logged
	assert.Eventually(t, func() bool {
		return strings.Contains(fl.LastErrMsg(), "Failed to send to DLQ")
	}, 2*time.Second, 100*time.Millisecond)

	// Since it wasn't acked, if we restart a subscriber (with a new context), it should receive it again.
	// But first we must stop the current subscriber which is retrying DLQ.
	cancel()

	// Wait a bit to ensure goroutine exited
	time.Sleep(200 * time.Millisecond)

	// Start a new subscriber on the same source. It should pick up the message again because it wasn't ACKed.
	receivedAgain := make(chan struct{}, 1)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	require.NoError(t, m.Subscribe(ctx2, source, channel, "test", "test", 0, func(msg Message) error {
		receivedAgain <- struct{}{}
		return nil // succeed this time
	}))

	select {
	case <-receivedAgain:
		// Success! Message was re-delivered.
	case <-time.After(2 * time.Second):
		t.Fatal("Message not re-delivered after DLQ failure")
	}
}

func TestMessenger_DLQSuccess_AcksOriginal(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_dlq_success")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove %s: %v", tmpDir, err)
		}
	})

	fl := &FakeLogger{}
	m := NewMessenger(fl, OsProvider{})
	m.SetDataDir(tmpDir)
	m.maxRetryAttempts = 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	channel := "ok-chan"
	dlqChannel := "_dlq." + channel
	source := "test-sub"

	require.NoError(t, m.Subscribe(ctx, source, channel, "test", "test", 0, func(msg Message) error {
		return errors.New("fail always")
	}))

	if err := m.Send(Message{ChannelName: channel, Text: "to-dlq"}); err != nil {

		assert.NoError(t, err)

	}
	// Wait for DLQ
	dlqReceived := make(chan Message, 1)
	require.NoError(t, m.Subscribe(ctx, "dlq-reader", dlqChannel, "test", "dlq-reader", 0, func(msg Message) error {
		dlqReceived <- msg
		return nil
	}))

	select {
	case msg := <-dlqReceived:
		assert.Equal(t, "to-dlq", msg.Text)
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for DLQ")
	}

	// Verify original is NOT re-delivered if we start a new subscriber
	time.Sleep(500 * time.Millisecond)
	receivedAgain := make(chan struct{}, 1)
	require.NoError(t, m.Subscribe(ctx, source, channel, "test", "test", 0, func(msg Message) error {
		receivedAgain <- struct{}{}
		return nil
	}))

	select {
	case <-receivedAgain:
		t.Fatal("Message was re-delivered but should have been ACKed after successful DLQ")
	case <-time.After(1 * time.Second):
		// OK
	}
}

func TestMessenger_RetryCountContract(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_retry_contract")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove %s: %v", tmpDir, err)
		}
	})

	fl := &FakeLogger{}
	m := NewMessenger(fl, OsProvider{})
	m.SetDataDir(tmpDir)
	m.maxRetryAttempts = 2 // 1 initial + 2 retries = 3 total

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	calls := 0
	require.NoError(t, m.Subscribe(ctx, "sub", "chan", "test", "test", 0, func(msg Message) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return errors.New("fail")
	}))

	if err := m.Send(Message{ChannelName: "chan", Text: "retry-test"}); err != nil {

		assert.NoError(t, err)

	}
	// Wait until it hits DLQ
	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls == 3
	}, 5*time.Second, 100*time.Millisecond)

	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 3, calls, "Expected exactly 3 calls (1 initial + 2 retries)")
}

func TestMessenger_UnmarshalFailure_LogsEnvelopeAndLegacyErrors(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_unmarshal_err")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove %s: %v", tmpDir, err)
		}
	})

	fl := &FakeLogger{}
	m := NewMessenger(fl, OsProvider{})
	m.SetDataDir(tmpDir)

	channel := "bad-json-chan"
	err = m.initializePersistentQueue(channel)
	assert.NoError(t, err)

	logPath := filepath.Join(tmpDir, fmt.Sprintf("%s_queue_%s.log", channel, time.Now().Format("20060102")))
	if err := os.WriteFile(logPath, []byte("invalid json\n"), 0644); err != nil {
		assert.NoError(t, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, m.Subscribe(ctx, "sub", channel, "test", "test", 0, func(msg Message) error { return nil }))

	assert.Eventually(t, func() bool {
		fl.mu.RLock()
		defer fl.mu.RUnlock()
		hasEnvelopeErr := false
		hasLegacyErr := false
		for i := 0; i < len(fl.lastErrArgs); i += 2 {
			if fl.lastErrArgs[i] == "envelopeError" {
				hasEnvelopeErr = true
			}
			if fl.lastErrArgs[i] == "legacyError" {
				hasLegacyErr = true
			}
		}
		return fl.lastErrMsg == "Failed to unmarshal dequeued message as Envelope or Message" && hasEnvelopeErr && hasLegacyErr
	}, 2*time.Second, 100*time.Millisecond)
}

func TestEnvelope_TypedPayload_RoundTrip(t *testing.T) {
	status := DeviceStatusEvent{
		DeviceID: "sensor-1",
		Status:   "online",
		Battery:  85,
	}

	env := NewEnvelope("device.status", "test-source", status)
	if env.Headers == nil {
		env.Headers = make(map[string]string)
	}
	env.Headers["payload-type"] = "device.status"

	// Marshal to JSON
	data, err := json.Marshal(env)
	assert.NoError(t, err)

	// Unmarshal back to Envelope
	env2, err := UnmarshalEnvelope(data)
	assert.NoError(t, err)

	// Unmarshal payload
	typed, err := env2.UnmarshalPayload()
	assert.NoError(t, err)

	typedStatus, ok := typed.(*DeviceStatusEvent)
	if !ok {
		// Try non-pointer for robustness, though registry should return pointer based on registration
		ts, ok2 := typed.(DeviceStatusEvent)
		if ok2 {
			typedStatus = &ts
			ok = true
		}
	}
	assert.True(t, ok, "Expected *DeviceStatusEvent or DeviceStatusEvent, got %T", typed)
	assert.Equal(t, status.DeviceID, typedStatus.DeviceID)
	assert.Equal(t, status.Status, typedStatus.Status)
	assert.Equal(t, status.Battery, typedStatus.Battery)
}

func TestEnvelope_PayloadRegistry_Concurrency(t *testing.T) {
	const numGoroutines = 20
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Concurrent registration
	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()
			for j := range iterations {
				typeName := fmt.Sprintf("type-%d-%d", id, j)
				if err := RegisterPayload(typeName, func() any { return map[string]int{"id": id, "j": j} }); err != nil {
					t.Errorf("RegisterPayload failed: %v", err)
				}
			}
		}(i)
	}

	// Concurrent unmarshaling
	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()
			for j := range iterations {
				typeName := "device.status" // Always present
				env := Envelope{
					Headers: map[string]string{"payload-type": typeName},
					Payload: DeviceStatusEvent{DeviceID: "test"},
				}
				if _, err := env.UnmarshalPayload(); err != nil {
					t.Logf("env.UnmarshalPayload failed: %v", err)
				}

				// Also try to unmarshal something we might have just registered
				regTypeName := fmt.Sprintf("type-%d-%d", id, j)
				env2 := Envelope{
					Headers: map[string]string{"payload-type": regTypeName},
					Payload: map[string]int{"id": id, "j": j},
				}
				if _, err := env2.UnmarshalPayload(); err != nil {
					t.Logf("env2.UnmarshalPayload failed: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestMessenger_ConcurrentSubscribeAndSend_NoDeadlock(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_concurrent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove %s: %v", tmpDir, err)
		}
	})

	m := NewMessenger(&FakeLogger{}, OsProvider{})
	m.SetDataDir(tmpDir)

	const (
		numGoroutines = 5
		numMessages   = 50
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	channel := "concurrent-chan"
	var wg sync.WaitGroup

	// Multiple subscribers
	for i := range numGoroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			source := fmt.Sprintf("sub-%d", id)
			if err := m.Subscribe(ctx, source, channel, "test", source, 0, func(msg Message) error {
				return nil
			}); err != nil {
				t.Errorf("Subscribe error: %v", err)
			}
		}(i)
	}

	// Multiple senders
	for i := range numGoroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range numMessages {
				_ = m.Send(Message{
					ChannelName: channel,
					Text:        fmt.Sprintf("msg-%d-%d", id, j),
				})
			}
		}(i)
	}

	// Wait a bit then cancel everything
	time.Sleep(1 * time.Second)
	cancel()
	wg.Wait()
	// If it didn't hang, it's a success
}

func TestEnvelope_LegacyPayload_BackwardCompatible(t *testing.T) {
	legacy := Message{
		Text: "legacy-content",
		Uuid: "old-uuid",
	}
	env := NewEnvelopeFromMessage(legacy)

	msg := env.ToMessage()
	assert.Equal(t, "legacy-content", msg.Text)
	assert.Equal(t, "old-uuid", msg.Uuid)
}

func TestEnvelope_UnknownType_GracefulFallback(t *testing.T) {
	env := Envelope{
		Headers: map[string]string{"payload-type": "unknown.type"},
		Payload: map[string]any{"key": "value"},
	}

	typed, err := env.UnmarshalPayload()
	assert.NoError(t, err)

	// Should fallback to map[string]any
	pMap, ok := typed.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "value", pMap["key"])
}
