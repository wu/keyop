package sun

import (
	"context"
	"keyop/core"
	"keyop/core/testutil"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSunService(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	handlers := make(map[string]func(core.Message) error)
	messenger := testutil.NewFakeMessenger(testutil.WithSubscribeHook(func(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
		handlers[channelName] = messageHandler
		return nil
	}))
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
		messenger.Reset()

		err := svc.Check()
		assert.NoError(t, err)

		msgs := messenger.Messages()
		assert.NotEmpty(t, msgs)

		var eventMsg *core.Message
		for i, m := range msgs {
			if m.Event == "sun_check" {
				eventMsg = &msgs[i]
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

		handler := handlers["gps"]
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
