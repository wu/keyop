package nwsWeather

import (
	"context"
	"encoding/json"
	"keyop/core"
	"keyop/core/testutil"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNwsWeatherService(t *testing.T) {
	// Setup mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/points/45.0000,-93.0000" {
			data := map[string]interface{}{
				"properties": map[string]string{
					"forecast": "http://" + r.Host + "/gridpoints/MPX/107,71/forecast",
				},
			}
			if err := json.NewEncoder(w).Encode(data); err != nil {
				t.Fatalf("failed to encode response: %v", err)
			}
			return
		}
		if r.URL.Path == "/points/46.0000,-94.0000" {
			data := map[string]interface{}{
				"properties": map[string]string{
					"forecast": "http://" + r.Host + "/gridpoints/MPX/123,45/forecast",
				},
			}
			if err := json.NewEncoder(w).Encode(data); err != nil {
				t.Fatalf("failed to encode response: %v", err)
			}
			return
		}
		if r.URL.Path == "/gridpoints/MPX/107,71/forecast" {
			data := map[string]interface{}{
				"properties": map[string]interface{}{
					"periods": []map[string]interface{}{
						{
							"detailedForecast": "Mostly cloudy, with a low around 28.",
							"shortForecast":    "Mostly Cloudy",
							"temperature":      28,
							"temperatureUnit":  "F",
						},
					},
				},
			}
			if err := json.NewEncoder(w).Encode(data); err != nil {
				t.Fatalf("failed to encode response: %v", err)
			}
			return
		}
		if r.URL.Path == "/gridpoints/MPX/123,45/forecast" {
			data := map[string]interface{}{
				"properties": map[string]interface{}{
					"periods": []map[string]interface{}{
						{
							"detailedForecast": "Sunny with a high of 75.",
							"shortForecast":    "Sunny",
							"temperature":      75,
							"temperatureUnit":  "F",
						},
					},
				},
			}
			if err := json.NewEncoder(w).Encode(data); err != nil {
				t.Fatalf("failed to encode response: %v", err)
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

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
		Name: "test-weather",
		Type: "nwsWeather",
		Pubs: map[string]core.ChannelInfo{
			"events": {Name: "weather"},
		},
		Subs: map[string]core.ChannelInfo{
			"gps": {Name: "gps-channel"},
		},
		Config: map[string]interface{}{
			"lat": 45.0,
			"lon": -93.0,
		},
	}

	svc := NewService(deps, cfg).(*Service)
	svc.mu.Lock()
	svc.apiBaseURL = server.URL
	svc.mu.Unlock()

	t.Run("Check fetches and sends weather", func(t *testing.T) {
		err := svc.Check()
		assert.NoError(t, err)

		msgs := messenger.Messages()
		assert.Equal(t, 1, len(msgs))
		msg := msgs[0]
		assert.Equal(t, "test-weather", msg.ChannelName)
		assert.Contains(t, msg.Text, "Mostly cloudy")
		assert.Contains(t, msg.Summary, "Mostly Cloudy, 28°F")
	})

	t.Run("Uses GPS coordinates from channel", func(t *testing.T) {
		err := svc.Initialize()
		assert.NoError(t, err)

		// Trigger GPS update
		gpsMsg := core.Message{
			ChannelName: "gps-channel",
			Data: map[string]interface{}{
				"lat": 46.0,
				"lon": -94.0,
			},
		}
		handler := handlers["gps-channel"]
		assert.NotNil(t, handler)
		err = handler(gpsMsg)
		assert.NoError(t, err)

		// Check should now use new coordinates and new forecast URL
		messenger.Reset()

		err = svc.Check()
		assert.NoError(t, err)

		msgs := messenger.Messages()
		assert.Equal(t, 1, len(msgs))
		msg := msgs[0]
		assert.Contains(t, msg.Text, "Sunny")
		assert.Contains(t, msg.Summary, "Sunny, 75°F")
	})
}
