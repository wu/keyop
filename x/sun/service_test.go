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
	messenger := testutil.NewFakeMessenger(testutil.WithSubscribeHook(func(_ context.Context, _ string, channelName string, _ string, _ string, _ time.Duration, messageHandler func(core.Message) error) error {
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
		// Accept either the legacy Events type or the new SunEvent typed payload
		switch d := eventMsg.Data.(type) {
		case SunEvent:
			assert.NotEmpty(t, d.Sunrise)
			assert.NotEmpty(t, d.Sunset)
		case Events:
			assert.NotEmpty(t, d.Sunrise)
			assert.NotEmpty(t, d.Sunset)
		default:
			t.Fatalf("unexpected data type: %T", eventMsg.Data)
		}
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
		// Test that scheduling is idempotent: calling scheduleAlerts multiple times
		// without time passing shouldn't accumulate timers.

		// Clean and schedule
		svc.mu.Lock()
		for _, t := range svc.timers {
			t.Stop()
		}
		svc.timers = nil
		svc.mu.Unlock()
		svc.scheduleAlerts()

		svc.mu.RLock()
		count1 := len(svc.timers)
		svc.mu.RUnlock()

		// Schedule again without any time passing
		svc.scheduleAlerts()

		svc.mu.RLock()
		count2 := len(svc.timers)
		svc.mu.RUnlock()

		// Timer count should be the same (idempotent)
		assert.Equal(t, count1, count2, "rescheduling should not accumulate timers")

		// Test that the fix for the original bug works:
		// When GPS updates trigger a reschedule, the previous timers should be cancelled
		// and replaced with new ones for the updated location
		msg := core.Message{
			ChannelName: "gps",
			Data: map[string]interface{}{
				"lat": 40.7128, // New York (different location)
				"lon": -74.0060,
				"alt": 50.0,
			},
		}
		handler := handlers["gps"]
		err := handler(msg)
		assert.NoError(t, err)

		svc.mu.RLock()
		count3 := len(svc.timers)
		svc.mu.RUnlock()

		// After GPS update with same config, should still have manageable timer count
		// (The number might differ due to location, but should be a small number, not accumulated)
		assert.Greater(t, 10, count3, "GPS update should not create excessive timers")

		// Clean up timers
		svc.mu.Lock()
		for _, t := range svc.timers {
			t.Stop()
		}
		svc.timers = nil
		svc.mu.Unlock()
	})
}
