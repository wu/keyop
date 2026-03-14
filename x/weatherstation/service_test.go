//nolint:revive
package weatherstation

import (
	"keyop/core"
	"keyop/core/testutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHandleWeather(t *testing.T) {
	mockMessenger := testutil.NewFakeMessenger()
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	deps.SetMessenger(mockMessenger)

	cfg := core.ServiceConfig{
		Name: "test-ws2902c",
		Pubs: map[string]core.ChannelInfo{
			"weather": {Name: "weather-channel"},
			"metrics": {Name: "metrics-channel"},
			"temp":    {Name: "temp-channel"},
		},
		Config: map[string]interface{}{
			"port": 8080,
			"fieldMetricNames": map[string]interface{}{
				"OutTemp": "outTemp",
			},
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
	var receivedData *core.WeatherStationEvent
	for _, m := range mockMessenger.SentMessages {
		if m.Data != nil {
			if data, ok := m.Data.(core.WeatherStationEvent); ok {
				receivedData = &data
				break
			}
		}
	}

	if receivedData == nil {
		t.Fatal("expected to find WeatherStationEvent message")
	}

	if receivedData.OutTemp != 72.5 {
		t.Errorf("expected temp 72.5, got %v", receivedData.OutTemp)
	}

	// Verify individual metric publication with default names
	foundOutTemp := false
	for _, msg := range mockMessenger.SentMessages {
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
	mockMessenger := testutil.NewFakeMessenger()
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	deps.SetMessenger(mockMessenger)

	cfg := core.ServiceConfig{
		Name: "test-ws2902c",
		Pubs: map[string]core.ChannelInfo{
			"weather": {Name: "weather-channel"},
			"metrics": {Name: "metrics-channel"},
			"temp":    {Name: "temp-channel"},
		},
		Config: map[string]interface{}{
			"port": 8080,
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
	for _, msg := range mockMessenger.SentMessages {
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
				"metrics": {Name: "m"},
				"temp":    {Name: "t"},
				"rain":    {Name: "r"},
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
