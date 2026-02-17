package nwsWeather

import (
	"context"
	"encoding/json"
	"keyop/core"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type mockMessenger struct {
	messages      []core.Message
	subscriptions map[string]func(core.Message) error
	mu            sync.Mutex
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
	if m.subscriptions == nil {
		m.subscriptions = make(map[string]func(core.Message) error)
	}
	m.subscriptions[channelName] = messageHandler
	return nil
}

func (m *mockMessenger) trigger(channelName string, msg core.Message) error {
	m.mu.Lock()
	handler, ok := m.subscriptions[channelName]
	m.mu.Unlock()
	if ok {
		return handler(msg)
	}
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

func TestNwsWeatherService(t *testing.T) {
	// Setup mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/points/45.0000,-93.0000" {
			data := map[string]interface{}{
				"properties": map[string]string{
					"forecast": "http://" + r.Host + "/gridpoints/MPX/107,71/forecast",
				},
			}
			json.NewEncoder(w).Encode(data)
			return
		}
		if r.URL.Path == "/points/46.0000,-94.0000" {
			data := map[string]interface{}{
				"properties": map[string]string{
					"forecast": "http://" + r.Host + "/gridpoints/MPX/123,45/forecast",
				},
			}
			json.NewEncoder(w).Encode(data)
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
			json.NewEncoder(w).Encode(data)
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
			json.NewEncoder(w).Encode(data)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	messenger := &mockMessenger{}
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

		messenger.mu.Lock()
		defer messenger.mu.Unlock()
		assert.Equal(t, 1, len(messenger.messages))
		msg := messenger.messages[0]
		assert.Equal(t, "weather", msg.ChannelName)
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
		err = messenger.trigger("gps-channel", gpsMsg)
		assert.NoError(t, err)

		// Check should now use new coordinates and new forecast URL
		messenger.mu.Lock()
		messenger.messages = nil
		messenger.mu.Unlock()

		err = svc.Check()
		assert.NoError(t, err)

		messenger.mu.Lock()
		defer messenger.mu.Unlock()
		assert.Equal(t, 1, len(messenger.messages))
		msg := messenger.messages[0]
		assert.Contains(t, msg.Text, "Sunny")
		assert.Contains(t, msg.Summary, "Sunny, 75°F")
	})
}
