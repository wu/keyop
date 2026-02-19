package aurora

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

func testDeps(t *testing.T) core.Dependencies {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}

	ctx, cancel := context.WithCancel(context.Background())
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	osProvider := core.OsProvider{}
	deps.SetOsProvider(osProvider)
	deps.SetLogger(logger)

	// Create a unique data directory for each test to avoid interference
	dataDir, err := os.MkdirTemp("", "aurora-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(dataDir)
	})

	messenger := core.NewMessenger(logger, osProvider)
	messenger.SetDataDir(dataDir)
	deps.SetMessenger(messenger)

	return deps
}

func TestService_ValidateConfig(t *testing.T) {
	deps := testDeps(t)

	tests := []struct {
		name        string
		config      map[string]interface{}
		pubs        map[string]core.ChannelInfo
		subs        map[string]core.ChannelInfo
		expectError bool
	}{
		{
			name: "valid config",
			config: map[string]interface{}{
				"lat": 45.0,
				"lon": -93.0,
			},
			pubs: map[string]core.ChannelInfo{
				"events": {Name: "events-topic"},
				"alerts": {Name: "alerts-topic"},
			},
			subs: map[string]core.ChannelInfo{
				"gps": {Name: "gps-topic"},
			},
			expectError: false,
		},
		{
			name: "missing lat",
			config: map[string]interface{}{
				"lon": -93.0,
			},
			expectError: true,
		},
		{
			name: "missing lon",
			config: map[string]interface{}{
				"lat": 45.0,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := core.ServiceConfig{
				Config: tt.config,
				Pubs:   tt.pubs,
				Subs:   tt.subs,
			}
			svc := NewService(deps, cfg)
			errs := svc.ValidateConfig()

			if tt.expectError {
				assert.NotEmpty(t, errs)
			} else {
				assert.Empty(t, errs)
			}
		})
	}
}

func TestService_Check(t *testing.T) {
	// Mock NOAA server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := map[string]interface{}{
			"Forecast Time": "2026-02-18T21:00:00Z",
			"coordinates": [][]int{
				{267, 45, 10}, // 267E is 93W
				{0, 0, 0},
			},
		}
		json.NewEncoder(w).Encode(data)
	}))
	defer server.Close()

	deps := testDeps(t)
	cfg := core.ServiceConfig{
		Name: "aurora",
		Type: "aurora",
		Config: map[string]interface{}{
			"lat": 45.0,
			"lon": -93.0,
		},
		Pubs: map[string]core.ChannelInfo{
			"events": {Name: "events"},
			"alerts": {Name: "alerts"},
		},
	}

	svc := NewService(deps, cfg).(*Service)
	svc.apiURL = server.URL

	var receivedEvents []core.Message
	var receivedAlerts []core.Message
	var mu sync.Mutex

	messenger := deps.MustGetMessenger()
	messenger.Subscribe(context.Background(), "test", "events", "aurora", "aurora", 0, func(msg core.Message) error {
		mu.Lock()
		receivedEvents = append(receivedEvents, msg)
		mu.Unlock()
		return nil
	})
	messenger.Subscribe(context.Background(), "test", "alerts", "aurora", "aurora", 0, func(msg core.Message) error {
		mu.Lock()
		receivedAlerts = append(receivedAlerts, msg)
		mu.Unlock()
		return nil
	})

	err := svc.Check()
	assert.NoError(t, err)

	// Give it a moment to process the async subscription
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, receivedEvents, 1)
	assert.Equal(t, "Aurora: 10%", receivedEvents[0].Summary)
	assert.Len(t, receivedAlerts, 1)
	assert.Equal(t, "Aurora Alert: 10%", receivedAlerts[0].Summary)
}

func TestService_InitializeAndGpsHandler(t *testing.T) {
	deps := testDeps(t)
	cfg := core.ServiceConfig{
		Name: "aurora",
		Type: "aurora",
		Config: map[string]interface{}{
			"lat": 45.0,
			"lon": -93.0,
		},
		Subs: map[string]core.ChannelInfo{
			"gps": {Name: "gps-topic", MaxAge: 0},
		},
	}

	svc := NewService(deps, cfg).(*Service)
	err := svc.Initialize()
	assert.NoError(t, err)

	// Send GPS update
	messenger := deps.MustGetMessenger()
	newLat := 50.0
	newLon := -100.0
	gpsMsg := core.Message{
		ChannelName: "gps-topic",
		Data: map[string]interface{}{
			"lat": newLat,
			"lon": newLon,
		},
	}
	err = messenger.Send(gpsMsg)
	assert.NoError(t, err)

	// Give it a moment to process the async subscription
	time.Sleep(200 * time.Millisecond)

	svc.mu.RLock()
	defer svc.mu.RUnlock()
	assert.NotNil(t, svc.cachedLat)
	assert.NotNil(t, svc.cachedLon)
	assert.Equal(t, newLat, *svc.cachedLat)
	assert.Equal(t, newLon, *svc.cachedLon)
}

func TestService_Check_Errors(t *testing.T) {
	deps := testDeps(t)
	cfg := core.ServiceConfig{
		Name: "aurora",
		Type: "aurora",
		Config: map[string]interface{}{
			"lat": 45.0,
			"lon": -93.0,
		},
		Pubs: map[string]core.ChannelInfo{
			"events": {Name: "events"},
			"alerts": {Name: "alerts"},
		},
	}

	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		svc := NewService(deps, cfg).(*Service)
		svc.apiURL = server.URL

		err := svc.Check()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "status 500")
	})

	t.Run("Invalid JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("invalid-json"))
		}))
		defer server.Close()

		svc := NewService(deps, cfg).(*Service)
		svc.apiURL = server.URL

		err := svc.Check()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse json")
	})

	t.Run("Invalid coordinates in data", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			data := map[string]interface{}{
				"Forecast Time": "2026-02-18T21:00:00Z",
				"coordinates": [][]interface{}{
					{10, 50}, // Missing probability
				},
			}
			json.NewEncoder(w).Encode(data)
		}))
		defer server.Close()

		svc := NewService(deps, cfg).(*Service)
		svc.apiURL = server.URL

		err := svc.Check()
		assert.NoError(t, err) // Should handle it and not crash
	})
}

func TestService_Check_GridSearch(t *testing.T) {
	// Mock NOAA server with a grid
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := map[string]interface{}{
			"Forecast Time": "2026-02-18T21:00:00Z",
			"coordinates": [][]int{
				{10, 50, 20},  // 10E, 50N, 20%
				{350, 50, 30}, // 350E (10W), 50N, 30%
				{180, 0, 40},  // 180E, 0N, 40%
			},
		}
		json.NewEncoder(w).Encode(data)
	}))
	defer server.Close()

	deps := testDeps(t)
	baseCfg := core.ServiceConfig{
		Name: "aurora",
		Type: "aurora",
		Pubs: map[string]core.ChannelInfo{
			"events": {Name: "events"},
			"alerts": {Name: "alerts"},
		},
	}

	tests := []struct {
		name         string
		lat, lon     float64
		expectedProb int
	}{
		{
			name:         "Near 10E, 50N",
			lat:          50.1,
			lon:          10.1,
			expectedProb: 20,
		},
		{
			name:         "Near 10W (350E), 50N",
			lat:          49.9,
			lon:          -9.9,
			expectedProb: 30,
		},
		{
			name:         "Near 180E, 0N",
			lat:          0.5,
			lon:          179.5,
			expectedProb: 40,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseCfg
			cfg.Config = map[string]interface{}{
				"lat": tt.lat,
				"lon": tt.lon,
			}
			svc := NewService(deps, cfg).(*Service)
			svc.apiURL = server.URL

			err := svc.Check()
			assert.NoError(t, err)

			// Check only events because zero probability won't send alerts (not in this case but anyway)
			// But we need to capture sent message.
			// Re-use messenger subscription.
			var mu sync.Mutex
			var prob int
			messenger := deps.MustGetMessenger()
			messenger.Subscribe(context.Background(), "test-grid", cfg.Pubs["events"].Name, "aurora", "aurora", 0, func(msg core.Message) error {
				mu.Lock()
				if data, ok := msg.Data.(map[string]interface{}); ok {
					if l, ok := data["likelihood"].(int); ok {
						prob = l
					} else if lf, ok := data["likelihood"].(float64); ok {
						prob = int(lf)
					}
				}
				mu.Unlock()
				return nil
			})

			err = svc.Check()
			assert.NoError(t, err)
			time.Sleep(100 * time.Millisecond)

			mu.Lock()
			assert.Equal(t, tt.expectedProb, prob)
			mu.Unlock()
		})
	}
}

func TestService_Check_ZeroProbability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := map[string]interface{}{
			"Forecast Time": "2026-02-18T21:00:00Z",
			"coordinates": [][]int{
				{0, 0, 0},
			},
		}
		json.NewEncoder(w).Encode(data)
	}))
	defer server.Close()

	deps := testDeps(t)
	cfg := core.ServiceConfig{
		Name: "aurora",
		Type: "aurora",
		Config: map[string]interface{}{
			"lat": 0.0,
			"lon": 0.0,
		},
		Pubs: map[string]core.ChannelInfo{
			"events": {Name: "events-zero"},
			"alerts": {Name: "alerts-zero"},
		},
	}

	svc := NewService(deps, cfg).(*Service)
	svc.apiURL = server.URL

	var alertsCount int
	var mu sync.Mutex
	messenger := deps.MustGetMessenger()
	messenger.Subscribe(context.Background(), "test", "alerts-zero", "aurora", "aurora", 0, func(msg core.Message) error {
		mu.Lock()
		alertsCount++
		mu.Unlock()
		return nil
	})

	err := svc.Check()
	assert.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 0, alertsCount, "Should not send alert for 0% probability")
}
