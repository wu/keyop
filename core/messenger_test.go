package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Compile-time check that *Messenger implements MessengerApi
var _ MessengerApi = (*Messenger)(nil)

func TestMessenger_SubscribeAndSend_ToMultipleSubscribers(t *testing.T) {
	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)), FakeOsProvider{Host: "test-host"})

	var ch1Msg Message
	var ch2Msg Message
	err := m.Subscribe("test", "alpha", func(msg Message) error { ch1Msg = msg; return nil })
	assert.NoError(t, err)
	err = m.Subscribe("test", "alpha", func(msg Message) error { ch2Msg = msg; return nil })
	assert.NoError(t, err)

	// Send in a goroutine to avoid blocking on unbuffered channels
	go func() {
		_ = m.Send("alpha", Message{Text: "hello"}, nil)
	}()

	time.Sleep(500 * time.Millisecond)

	// Both subscribers should receive the same message
	assert.Equal(t, "hello", ch1Msg.Text)
	assert.Equal(t, "hello", ch2Msg.Text)

}

func TestMessenger_Send_IsolatedByChannel(t *testing.T) {
	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)), FakeOsProvider{Host: "test-host"})

	var ch1Msg Message
	var ch2Msg Message
	err := m.Subscribe("test", "a", func(msg Message) error { ch1Msg = msg; return nil })
	assert.NoError(t, err)
	err = m.Subscribe("test", "b", func(msg Message) error { ch1Msg = msg; return nil })
	assert.NoError(t, err)

	// Send to channel "a" only
	go func() { _ = m.Send("a", Message{Text: "foo"}, nil) }()

	time.Sleep(500 * time.Millisecond)

	assert.Equal(t, "foo", ch1Msg.Text)
	assert.Equal(t, "", ch2Msg.Text)
}

func TestMessenger_Send_OrderPreserved(t *testing.T) {
	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)), FakeOsProvider{Host: "test-host"})

	var messages []Message
	err := m.Subscribe("test", "ordered", func(msg Message) error { messages = append(messages, msg); return nil })
	assert.NoError(t, err)

	// Send three messages in order in a single goroutine
	for i := 1; i <= 3; i++ {
		_ = m.Send("ordered", Message{Text: fmt.Sprintf("%d", i)}, nil)
	}

	time.Sleep(500 * time.Millisecond)

	assert.Equal(t, "1", messages[0].Text)
	assert.Equal(t, "2", messages[1].Text)
	assert.Equal(t, "3", messages[2].Text)
}

func TestMessenger_Send_NoSubscribers_NoError(t *testing.T) {
	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)), FakeOsProvider{Host: "test-host"})

	// Should not block and should return nil
	err := m.Send("nobody", Message{Text: "ignored"}, nil)
	assert.NoError(t, err)
}

func TestMessenger_Send_SerializesDataToJSON(t *testing.T) {
	m := NewMessenger(&FakeLogger{}, FakeOsProvider{Host: "host-1"})

	var gotMessage Message
	err := m.Subscribe("test", "json", func(msg Message) error { gotMessage = msg; return nil })
	assert.NoError(t, err)

	// Define a struct to ensure stable JSON field order
	type payload struct {
		K string `json:"k"`
		N int    `json:"n"`
	}
	p := payload{K: "v", N: 123}

	go func() {
		_ = m.Send("json", Message{Text: "with-data"}, p)
	}()

	time.Sleep(500 * time.Millisecond)

	assert.Equal(t, "host-1", gotMessage.Hostname)
	assert.False(t, gotMessage.Timestamp.IsZero())

	b, _ := json.Marshal(p)
	assert.Equal(t, string(b), gotMessage.Data)
}

func TestNewMessenger_LoggerNotInitialized(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when logger is nil")
		}
	}()
	NewMessenger(nil, FakeOsProvider{Host: "test"})
}

func TestNewMessenger_OsProviderNotInitialized(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when osProvider is nil")
		}
	}()
	NewMessenger(&FakeLogger{}, nil)
}

func TestNewMessenger_HostnameError_LoggedAndEmptyHostname(t *testing.T) {
	fl := &FakeLogger{}
	testErr := errors.New("hostname lookup failed")

	m := NewMessenger(fl, FakeOsProvider{Host: "ignored", Err: testErr})

	// Verify error was logged with expected message and args
	assert.Equal(t, "Failed to determine hostname during initialization", fl.lastErrMsg)
	if assert.Len(t, fl.lastErrArgs, 2) {
		assert.Equal(t, "error", fl.lastErrArgs[0])
		assert.Equal(t, testErr, fl.lastErrArgs[1])
	}

	// Verify that resulting messenger uses empty hostname when sending
	var gotMessage Message
	err := m.Subscribe("test", "test", func(msg Message) error { gotMessage = msg; return nil })
	assert.NoError(t, err)

	go func() { _ = m.Send("test", Message{Text: "ping"}, nil) }()

	time.Sleep(500 * time.Millisecond)

	assert.Equal(t, "", gotMessage.Hostname)
	assert.Equal(t, "ping", gotMessage.Text)
}

func TestMessenger_Send_FailedToSerializeData_LogsErrorAndSendsWithoutData(t *testing.T) {
	fl := &FakeLogger{}
	m := NewMessenger(fl, FakeOsProvider{Host: "h"})

	var gotMessage Message
	err := m.Subscribe("test", "bad", func(msg Message) error { gotMessage = msg; return nil })
	assert.NoError(t, err)

	// Use a channel value which json.Marshal cannot serialize to trigger an error
	bad := make(chan int)
	go func() { _ = m.Send("bad", Message{Text: "oops"}, bad) }()

	time.Sleep(500 * time.Millisecond)

	assert.Equal(t, "oops", gotMessage.Text)

	// Ensure the error was logged with the expected message and args
	assert.Equal(t, "Failed to serialize data", fl.lastErrMsg)
	if assert.Len(t, fl.lastErrArgs, 2) {
		assert.Equal(t, "error", fl.lastErrArgs[0])
		if _, ok := fl.lastErrArgs[1].(error); !ok {
			t.Fatalf("expected an error type for logger arg[1]")
		}
	}
}
