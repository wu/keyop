package weatherWs2902c

import (
	"context"
	"keyop/core"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type MockMessenger struct {
	LastMessage core.Message
	Messages    []core.Message
}

func (m *MockMessenger) Send(msg core.Message) error {
	m.LastMessage = msg
	m.Messages = append(m.Messages, msg)
	return nil
}

func (m *MockMessenger) Subscribe(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
	return nil
}

func (m *MockMessenger) SubscribeExtended(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message, string, int64) error) error {
	return nil
}

func (m *MockMessenger) SetReaderState(channelName string, readerName string, fileName string, offset int64) error {
	return nil
}

func (m *MockMessenger) SeekToEnd(channelName string, readerName string) error {
	return nil
}

func (m *MockMessenger) SetDataDir(dir string) {}

func (m *MockMessenger) SetHostname(hostname string) {}

func (m *MockMessenger) GetStats() core.MessengerStats { return core.MessengerStats{} }

func TestHandleWeather(t *testing.T) {
	mockMessenger := &MockMessenger{}
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	deps.SetMessenger(mockMessenger)

	cfg := core.ServiceConfig{
		Name: "test-ws2902c",
		Pubs: map[string]core.ChannelInfo{
			"weather": {Name: "weather-channel"},
			"metrics": {Name: "metrics-channel"},
		},
	}

	svc := NewService(deps, cfg).(*Service)

	form := url.Values{}
	form.Add("baromabsin", "29.92")
	form.Add("dateutc", "2023-10-27 12:00:00")
	form.Add("humidity", "50")
	form.Add("tempf", "72.5")
	form.Add("stationtype", "WS-2902C")

	req := httptest.NewRequest(http.MethodPost, "/weather", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	svc.handleWeather(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status OK, got %v", w.Code)
	}

	// Find the main weatherData message
	var receivedData *WeatherData
	for _, m := range mockMessenger.Messages {
		if m.MetricName == "weatherData" {
			if data, ok := m.Data.(*WeatherData); ok {
				receivedData = data
				break
			}
		}
	}

	if receivedData == nil {
		t.Fatal("expected to find weatherData message")
	}

	if receivedData.OutTemp != 72.5 {
		t.Errorf("expected temp 72.5, got %v", receivedData.OutTemp)
	}

	// Verify individual metric publication with default names
	foundOutTemp := false
	for _, msg := range mockMessenger.Messages {
		if msg.MetricName == "outTemp" {
			foundOutTemp = true
			if msg.Metric != 72.5 {
				t.Errorf("expected metric outTemp to be 72.5, got %v", msg.Metric)
			}
		}
	}
	if !foundOutTemp {
		t.Error("expected to find outTemp metric message")
	}
}

func TestHandleWeatherWithConfiguredMetrics(t *testing.T) {
	mockMessenger := &MockMessenger{}
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	deps.SetMessenger(mockMessenger)

	cfg := core.ServiceConfig{
		Name: "test-ws2902c",
		Pubs: map[string]core.ChannelInfo{
			"weather": {Name: "weather-channel"},
			"metrics": {Name: "metrics-channel"},
		},
		Config: map[string]interface{}{
			"fieldMetricNames": map[string]interface{}{
				"OutTemp": "customTempMetric",
			},
		},
	}

	svc := NewService(deps, cfg).(*Service)

	form := url.Values{}
	form.Add("tempf", "72.5")

	req := httptest.NewRequest(http.MethodPost, "/weather", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	svc.handleWeather(w, req)

	foundCustomTemp := false
	for _, msg := range mockMessenger.Messages {
		if msg.MetricName == "customTempMetric" {
			foundCustomTemp = true
			if msg.Metric != 72.5 {
				t.Errorf("expected metric customTempMetric to be 72.5, got %v", msg.Metric)
			}
		}
	}
	if !foundCustomTemp {
		t.Error("expected to find customTempMetric message")
	}
}

func TestValidateConfig(t *testing.T) {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})

	t.Run("valid config", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Config: map[string]interface{}{
				"port": 8080,
				"fieldMetricNames": map[string]interface{}{
					"OutTemp": "customTemp",
				},
			},
			Pubs: map[string]core.ChannelInfo{
				"weather": {Name: "w"},
			},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		if len(errs) > 0 {
			t.Errorf("expected no errors, got %v", errs)
		}
	})

	t.Run("invalid field name", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Config: map[string]interface{}{
				"port": 8080,
				"fieldMetricNames": map[string]interface{}{
					"InvalidField": "someName",
				},
			},
			Pubs: map[string]core.ChannelInfo{
				"weather": {Name: "w"},
			},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		found := false
		for _, err := range errs {
			if strings.Contains(err.Error(), "invalid field name") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected invalid field name error, got %v", errs)
		}
	})
}
