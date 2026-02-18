package sun

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
	subs     map[string]func(core.Message) error
}

func (m *mockMessenger) Send(msg core.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockMessenger) Subscribe(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.subs == nil {
		m.subs = make(map[string]func(core.Message) error)
	}
	m.subs[channelName] = messageHandler
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

func TestSunService(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	messenger := &mockMessenger{}
	deps.SetMessenger(messenger)
	deps.SetContext(context.Background())

	cfg := core.ServiceConfig{
		Name: "test-sun",
		Type: "sun",
		Pubs: map[string]core.ChannelInfo{
			"events": {Name: "events"},
			"alerts": {Name: "alerts"},
		},
		Subs: map[string]core.ChannelInfo{
			"gps": {Name: "gps"},
		},
		Config: map[string]interface{}{
			"lat": 34.0522, // Los Angeles
			"lon": -118.2437,
			"alt": 100.0,
		},
	}

	svc := NewService(deps, cfg).(*Service)
	assert.Equal(t, 100.0, svc.Alt)
	err := svc.Initialize()
	assert.NoError(t, err)

	t.Run("Check sends events", func(t *testing.T) {
		messenger.mu.Lock()
		messenger.messages = nil
		messenger.mu.Unlock()

		err := svc.Check()
		assert.NoError(t, err)

		messenger.mu.Lock()
		defer messenger.mu.Unlock()
		assert.NotEmpty(t, messenger.messages)

		var eventMsg *core.Message
		for _, m := range messenger.messages {
			if m.ChannelName == "events" {
				eventMsg = &m
				break
			}
		}
		assert.NotNil(t, eventMsg)
		assert.Contains(t, eventMsg.Text, "Next sun event:")
		events := eventMsg.Data.(SunEvents)
		assert.NotEmpty(t, events.Sunrise)
		assert.NotEmpty(t, events.Sunset)
	})

	t.Run("GPS updates cache", func(t *testing.T) {
		newLat := 40.7128 // New York
		newLon := -74.0060
		newAlt := 50.0

		msg := core.Message{
			ChannelName: "gps",
			Data: map[string]interface{}{
				"lat": newLat,
				"lon": newLon,
				"alt": newAlt,
			},
		}

		handler := messenger.subs["gps"]
		assert.NotNil(t, handler)
		err := handler(msg)
		assert.NoError(t, err)

		lat, lon, alt := svc.getObserverData()
		assert.Equal(t, newLat, lat)
		assert.Equal(t, newLon, lon)
		assert.Equal(t, newAlt, alt)
	})

	t.Run("Alerts scheduled", func(t *testing.T) {
		// Just verify it doesn't crash and runs without errors
		svc.scheduleAlerts()
	})
}
