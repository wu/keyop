package testutil

import (
	"context"
	"sync"
	"time"

	"keyop/core"
)

// FakeMessenger is a simple, thread-safe fake Messenger implementation for tests.
// It exposes safe accessors (Messages, Reset) and supports optional hooks via options
// so tests can inject special behaviors without needing bespoke fake types.
type FakeMessenger struct {
	Mu               sync.Mutex
	PayloadRegistry  core.PayloadRegistry
	SentMessages     []core.Message // Exported for test access
	Stats            core.MessengerStats
	MaxRetryAttempts int

	// Optional hooks for tests that need subscription behavior
	SubscribeHook         func(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message) error) error
	SubscribeExtendedHook func(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message, string, int64) error) error
}

// FakeMessengerOption configures a FakeMessenger.
type FakeMessengerOption func(*FakeMessenger)

// NewFakeMessenger creates a new FakeMessenger and applies any provided options.
func NewFakeMessenger(opts ...FakeMessengerOption) *FakeMessenger {
	f := &FakeMessenger{}
	for _, o := range opts {
		o(f)
	}
	return f
}

// WithMaxRetryAttempts sets MaxRetryAttempts on the FakeMessenger.
func WithMaxRetryAttempts(n int) FakeMessengerOption {
	return func(f *FakeMessenger) { f.MaxRetryAttempts = n }
}

// WithPayloadRegistry sets the PayloadRegistry on the FakeMessenger.
func WithPayloadRegistry(reg core.PayloadRegistry) FakeMessengerOption {
	return func(f *FakeMessenger) { f.PayloadRegistry = reg }
}

// WithStats sets initial stats on the FakeMessenger.
func WithStats(s core.MessengerStats) FakeMessengerOption {
	return func(f *FakeMessenger) { f.Stats = s }
}

// WithSubscribeHook injects a custom Subscribe implementation for tests that need it.
func WithSubscribeHook(h func(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message) error) error) FakeMessengerOption {
	return func(f *FakeMessenger) { f.SubscribeHook = h }
}

// WithSubscribeExtendedHook injects a custom SubscribeExtended implementation for tests that need it.
func WithSubscribeExtendedHook(h func(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message, string, int64) error) error) FakeMessengerOption {
	return func(f *FakeMessenger) { f.SubscribeExtendedHook = h }
}

func (f *FakeMessenger) Send(msg core.Message) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.SentMessages = append(f.SentMessages, msg)
	return nil
}

func (f *FakeMessenger) Subscribe(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
	if f.SubscribeHook != nil {
		return f.SubscribeHook(ctx, sourceName, channelName, serviceType, serviceName, maxAge, messageHandler)
	}
	return nil
}

func (f *FakeMessenger) SubscribeExtended(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message, string, int64) error) error {
	if f.SubscribeExtendedHook != nil {
		return f.SubscribeExtendedHook(ctx, source, channelName, serviceType, serviceName, maxAge, messageHandler)
	}
	return nil
}

func (f *FakeMessenger) SetReaderState(channelName string, readerName string, fileName string, offset int64) error {
	return nil
}

func (f *FakeMessenger) SeekToEnd(channelName string, readerName string) error {
	return nil
}

func (f *FakeMessenger) SetDataDir(dir string) {}

func (f *FakeMessenger) SetHostname(hostname string) {}

func (f *FakeMessenger) GetStats() core.MessengerStats {
	return f.Stats
}

func (f *FakeMessenger) GetPayloadRegistry() core.PayloadRegistry {
	return f.PayloadRegistry
}

func (f *FakeMessenger) SetPayloadRegistry(reg core.PayloadRegistry) {
	f.PayloadRegistry = reg
}

// Reset clears recorded messages and stats in a thread-safe manner.
func (f *FakeMessenger) Reset() {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.SentMessages = nil
	f.Stats = core.MessengerStats{}
}

// Messages returns a copy of sent messages in a thread-safe manner.
func (f *FakeMessenger) Messages() []core.Message {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	msgs := make([]core.Message, len(f.SentMessages))
	copy(msgs, f.SentMessages)
	return msgs
}
