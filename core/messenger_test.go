package core

import (
	"encoding/json"
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

	ch1 := m.Subscribe("alpha")
	ch2 := m.Subscribe("alpha")

	// Send in a goroutine to avoid blocking on unbuffered channels
	go func() {
		_ = m.Send("alpha", Message{Text: "hello"}, nil)
	}()

	// Both subscribers should receive the same message
	select {
	case msg := <-ch1:
		assert.Equal(t, "hello", msg.Text)
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for ch1 to receive message")
	}

	select {
	case msg := <-ch2:
		assert.Equal(t, "hello", msg.Text)
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for ch2 to receive message")
	}
}

func TestMessenger_Send_IsolatedByChannel(t *testing.T) {
	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)), FakeOsProvider{Host: "test-host"})

	a := m.Subscribe("a")
	b := m.Subscribe("b")

	// Send to channel "a" only
	go func() { _ = m.Send("a", Message{Text: "foo"}, nil) }()

	// a should receive
	select {
	case msg := <-a:
		assert.Equal(t, "foo", msg.Text)
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for a to receive message")
	}

	// b should not receive anything for this send
	select {
	case <-b:
		t.Fatalf("b received a message intended for channel 'a'")
	case <-time.After(100 * time.Millisecond):
		// expected timeout; no message
	}
}

func TestMessenger_Send_OrderPreserved(t *testing.T) {
	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)), FakeOsProvider{Host: "test-host"})
	ch := m.Subscribe("ordered")

	// Send three messages in order in a single goroutine
	go func() {
		for i := 1; i <= 3; i++ {
			_ = m.Send("ordered", Message{Text: fmt.Sprintf("%d", i)}, nil)
		}
	}()

	// Receive in order
	for i := 1; i <= 3; i++ {
		select {
		case msg := <-ch:
			assert.Equal(t, fmt.Sprintf("%d", i), msg.Text)
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("timeout waiting for message %d", i)
		}
	}
}

func TestMessenger_Send_NoSubscribers_NoError(t *testing.T) {
	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)), FakeOsProvider{Host: "test-host"})

	// Should not block and should return nil
	err := m.Send("nobody", Message{Text: "ignored"}, nil)
	assert.NoError(t, err)
}

func TestMessenger_Send_SerializesDataToJSON(t *testing.T) {
	m := NewMessenger(&FakeLogger{}, FakeOsProvider{Host: "host-1"})
	ch := m.Subscribe("json")

	// Define a struct to ensure stable JSON field order
	type payload struct {
		K string `json:"k"`
		N int    `json:"n"`
	}
	p := payload{K: "v", N: 123}

	go func() {
		_ = m.Send("json", Message{Text: "with-data"}, p)
	}()

	select {
	case msg := <-ch:
		// Check hostname and timestamp are set
		assert.Equal(t, "host-1", msg.Hostname)
		assert.False(t, msg.Timestamp.IsZero())

		// Validate DataString is the JSON representation of Data
		b, _ := json.Marshal(p)
		assert.Equal(t, string(b), msg.Data)

	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for json message")
	}
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
