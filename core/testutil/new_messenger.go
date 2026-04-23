package testutil

import (
	"context"
	"sync"
	"time"

	km "github.com/wu/keyop-messenger"
)

// FakeMessenger is a simple, thread-safe fake implementation of the new messenger for tests.
type FakeMessenger struct {
	PublishedMessages []PublishedMessage
	InstanceNameValue string
	Mu                sync.Mutex
	Handlers          map[string]km.HandlerFunc
}

// PublishedMessage records a message that was published.
type PublishedMessage struct {
	Channel     string
	PayloadType string
	Payload     interface{}
	Timestamp   time.Time
}

// NewFakeMessenger creates a new FakeMessenger.
func NewFakeMessenger() *FakeMessenger {
	return &FakeMessenger{
		PublishedMessages: []PublishedMessage{},
		Handlers:          make(map[string]km.HandlerFunc),
	}
}

// Publish records the published message.
func (f *FakeMessenger) Publish(ctx context.Context, channel string, payloadType string, payload interface{}) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.PublishedMessages = append(f.PublishedMessages, PublishedMessage{
		Channel:     channel,
		PayloadType: payloadType,
		Payload:     payload,
		Timestamp:   time.Now(),
	})
	return nil
}

// RegisterPayloadType is a no-op for testing.
func (f *FakeMessenger) RegisterPayloadType(typeStr string, prototype interface{}) error {
	return nil
}

// Subscribe captures the handler for the channel so tests can access it.
func (f *FakeMessenger) Subscribe(ctx context.Context, channel string, subscriberID string, handler km.HandlerFunc) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Handlers[channel] = handler
	return nil
}

// InstanceName returns the configured instance name.
func (f *FakeMessenger) InstanceName() string {
	return f.InstanceNameValue
}

// Close is a no-op for testing.
func (f *FakeMessenger) Close() error {
	return nil
}
