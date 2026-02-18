package moon

import (
	"context"
	"keyop/core"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type mockMessenger struct {
	messages []core.Message
	mu       sync.Mutex
}

func (m *mockMessenger) Send(msg core.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockMessenger) Subscribe(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
	return nil
}

func (m *mockMessenger) SubscribeExtended(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message, string, int64) error) error {
	return nil
}

func (m *mockMessenger) SetReaderState(channelName string, readerName string, fileName string, offset int64) error {
	return nil
}

func (m *mockMessenger) SeekToEnd(channelName string, readerName string) error {
	return nil
}

func (m *mockMessenger) SetDataDir(dir string) {}

func (m *mockMessenger) SetHostname(hostname string) {}

func (m *mockMessenger) GetStats() core.MessengerStats {
	return core.MessengerStats{}
}

type mockStateStore struct {
	data map[string]interface{}
	mu   sync.Mutex
}

func (m *mockStateStore) Save(key string, value interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *mockStateStore) Load(key string, value interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if !ok {
		return nil
	}

	// This is a simple mock, in real life it might use JSON
	// For float64 we can just do a type assertion if we're careful,
	// but core.StateStore expects a pointer to decode into.
	if f, ok := v.(float64); ok {
		*(value.(*float64)) = f
	}

	return nil
}

func TestMoonService(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	messenger := &mockMessenger{}
	deps.SetMessenger(messenger)
	state := &mockStateStore{data: make(map[string]interface{})}
	deps.SetStateStore(state)
	deps.SetContext(context.Background())

	cfg := core.ServiceConfig{
		Name: "test-moon",
		Type: "moon",
		Pubs: map[string]core.ChannelInfo{
			"events": {Name: "events"},
			"alerts": {Name: "alerts"},
		},
	}

	svc := NewService(deps, cfg).(*Service)
	err := svc.Initialize()
	assert.NoError(t, err)

	t.Run("Check sends events and initial alert", func(t *testing.T) {
		messenger.mu.Lock()
		messenger.messages = nil
		messenger.mu.Unlock()

		err := svc.Check()
		assert.NoError(t, err)

		messenger.mu.Lock()
		defer messenger.mu.Unlock()
		assert.Equal(t, 2, len(messenger.messages)) // 1 event + 1 initial alert

		var eventMsg *core.Message
		var alertMsg *core.Message
		for _, m := range messenger.messages {
			if m.ChannelName == "events" {
				eventMsg = &m
			} else if m.ChannelName == "alerts" {
				alertMsg = &m
			}
		}
		assert.NotNil(t, eventMsg)
		assert.Contains(t, eventMsg.Text, "Current moon phase:")
		assert.NotNil(t, alertMsg)
		assert.Contains(t, alertMsg.Text, "The moon is now in the")
	})

	t.Run("Check sends only event if phase name hasn't changed", func(t *testing.T) {
		messenger.mu.Lock()
		messenger.messages = nil
		messenger.mu.Unlock()

		// svc.lastMoonPhase is already set from previous run
		err := svc.Check()
		assert.NoError(t, err)

		messenger.mu.Lock()
		defer messenger.mu.Unlock()
		// Unless we are exactly at the boundary, it should only be the event message
		// Since we just called it, it's very likely the same phase name.
		// If it DID change, it's fine too, but usually it won't in a few milliseconds.
		if len(messenger.messages) > 1 {
			// Phase changed exactly between calls, which is possible but rare.
			assert.Equal(t, 2, len(messenger.messages))
		} else {
			assert.Equal(t, 1, len(messenger.messages))
			assert.Equal(t, "events", messenger.messages[0].ChannelName)
		}
	})

	t.Run("Persistence across restarts", func(t *testing.T) {
		// 1. Initial run to set state
		err := svc.Check()
		assert.NoError(t, err)

		// 2. "Restart" service with same state store
		newSvc := NewService(deps, cfg).(*Service)
		err = newSvc.Initialize()
		assert.NoError(t, err)

		// 3. Check again, should NOT send alert if phase name is same
		messenger.mu.Lock()
		messenger.messages = nil
		messenger.mu.Unlock()

		err = newSvc.Check()
		assert.NoError(t, err)

		messenger.mu.Lock()
		defer messenger.mu.Unlock()
		// Only event message, no alert because phase name is same as persisted
		assert.Equal(t, 1, len(messenger.messages), "Should not send redundant alert after restart")
		if len(messenger.messages) > 0 {
			assert.Equal(t, "events", messenger.messages[0].ChannelName)
		}
	})
}
