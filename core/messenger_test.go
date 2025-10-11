package core

import (
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
	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	ch1 := m.Subscribe("alpha")
	ch2 := m.Subscribe("alpha")

	// Send in a goroutine to avoid blocking on unbuffered channels
	go func() {
		_ = m.Send("alpha", Message{Text: "hello"})
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
	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	a := m.Subscribe("a")
	b := m.Subscribe("b")

	// Send to channel "a" only
	go func() { _ = m.Send("a", Message{Text: "foo"}) }()

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
	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	ch := m.Subscribe("ordered")

	// Send three messages in order in a single goroutine
	go func() {
		for i := 1; i <= 3; i++ {
			_ = m.Send("ordered", Message{Text: fmt.Sprintf("%d", i)})
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
	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	// Should not block and should return nil
	err := m.Send("nobody", Message{Text: "ignored"})
	assert.NoError(t, err)
}
