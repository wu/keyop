package testutil

import (
	"context"
	"reflect"
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
	// Prototypes records what RegisterPayloadType was called with, so tests that
	// exercise startup validation can resolve a payload type the way the real
	// messenger does.
	Prototypes map[string]reflect.Type
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
		Prototypes:        make(map[string]reflect.Type),
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

// RegisterPayloadType records the prototype so PayloadPrototype can return it.
// Like the real registry it stores the element type for a pointer prototype, so
// what PayloadPrototype reports is the type a handler would receive.
func (f *FakeMessenger) RegisterPayloadType(typeStr string, prototype interface{}) error {
	if prototype == nil {
		return nil
	}
	t := reflect.TypeOf(prototype)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.Prototypes == nil {
		f.Prototypes = make(map[string]reflect.Type)
	}
	f.Prototypes[typeStr] = t
	return nil
}

// PayloadPrototype returns a prototype recorded by RegisterPayloadType.
func (f *FakeMessenger) PayloadPrototype(typeStr string) (reflect.Type, bool) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	t, ok := f.Prototypes[typeStr]
	return t, ok
}

// Subscribe captures the handler for the channel so tests can access it.
// SubscribeOptions (e.g. WithMaxAge) are accepted to match the interface but
// are not interpreted by the fake.
func (f *FakeMessenger) Subscribe(ctx context.Context, channel string, subscriberID string, handler km.HandlerFunc, _ ...km.SubscribeOption) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Handlers[channel] = handler
	return nil
}

// InstanceName returns the configured instance name.
func (f *FakeMessenger) InstanceName() string {
	return f.InstanceNameValue
}

// Stats returns an empty snapshot; the fake collects no metrics.
func (f *FakeMessenger) Stats() km.Stats { return km.Stats{} }

// DiagnosticStats returns a zero value; the fake collects no per-subscriber metrics.
func (f *FakeMessenger) DiagnosticStats(channel string, subscriberID string) km.DiagnosticStats {
	return km.DiagnosticStats{}
}

// Close is a no-op for testing.
func (f *FakeMessenger) Close() error {
	return nil
}
