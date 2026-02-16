package owntracks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"keyop/core"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// helper to build dependencies
func testDeps() core.Dependencies {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}

	ctx, cancel := context.WithCancel(context.Background())
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	osProvider := core.OsProvider{}
	deps.SetOsProvider(osProvider)
	deps.SetLogger(logger)
	deps.SetMessenger(core.NewMessenger(logger, osProvider))

	// Add a dummy state store
	dataDir, _ := os.MkdirTemp("", "owntracks-test-*")
	deps.SetStateStore(core.NewFileStateStore(dataDir, osProvider))

	return deps
}

func TestService_ValidateConfig(t *testing.T) {
	deps := testDeps()

	tests := []struct {
		name        string
		config      map[string]interface{}
		pubs        map[string]core.ChannelInfo
		expectError bool
	}{
		{
			name: "valid config",
			config: map[string]interface{}{
				"port": 8080,
			},
			pubs: map[string]core.ChannelInfo{
				"owntracks": {Name: "owntracks-topic"},
				"gps":       {Name: "gps-topic"},
				"metrics":   {Name: "metrics-topic"},
				"events":    {Name: "events-topic"},
			},
			expectError: false,
		},
		{
			name:        "missing port",
			config:      map[string]interface{}{},
			expectError: true,
		},
		{
			name: "missing pubs",
			config: map[string]interface{}{
				"port": 8080,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := core.ServiceConfig{
				Config: tt.config,
				Pubs:   tt.pubs,
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

func TestService_Initialize_StartsServer(t *testing.T) {
	deps := testDeps()
	defer deps.MustGetCancel()()

	port := 8879
	cfg := core.ServiceConfig{
		Name: "test-owntracks",
		Type: "owntracks",
		Config: map[string]interface{}{
			"port": port,
		},
		Pubs: map[string]core.ChannelInfo{
			"owntracks": {Name: "owntracks"},
			"gps":       {Name: "gps"},
			"metrics":   {Name: "metrics"},
			"events":    {Name: "events"},
		},
	}

	svc := NewService(deps, cfg)

	err := svc.Initialize()
	assert.NoError(t, err)

	// Give the server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Send an OwnTracks JSON message
	testData := map[string]interface{}{
		"_type": "location",
		"lat":   51.5033,
		"lon":   -0.1195,
		"tid":   "ne",
	}
	body, _ := json.Marshal(testData)
	resp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/", port),
		"application/json",
		bytes.NewBuffer(body),
	)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestService_ServeHTTP(t *testing.T) {
	deps := testDeps()
	defer deps.MustGetCancel()()
	cfg := core.ServiceConfig{
		Name:   "test-owntracks",
		Type:   "owntracks",
		Config: map[string]interface{}{"port": 8081},
		Pubs: map[string]core.ChannelInfo{
			"owntracks": {Name: "owntracks"},
			"gps":       {Name: "gps"},
			"metrics":   {Name: "metrics"},
			"events":    {Name: "events"},
		},
	}
	svc := NewService(deps, cfg)

	t.Run("Method Not Allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		svc.(*Service).ServeHTTP(rr, req)
		assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	})

	t.Run("Invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{invalid`))
		rr := httptest.NewRecorder()
		svc.(*Service).ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("Valid OwnTracks Payload", func(t *testing.T) {
		testData := map[string]interface{}{
			"_type": "location",
			"lat":   51.5033,
			"lon":   -0.1195,
		}
		body, _ := json.Marshal(testData)
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(body))
		rr := httptest.NewRecorder()

		svc.(*Service).ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("Location and Battery and Regions", func(t *testing.T) {
		messenger := deps.MustGetMessenger().(*core.Messenger)
		gpsChan := make(chan core.Message, 10)
		metricsChan := make(chan core.Message, 10)
		eventsChan := make(chan core.Message, 10)
		owntracksChan := make(chan core.Message, 10)

		messenger.Subscribe(context.Background(), "test", "gps", "owntracks", "test", 0, func(m core.Message) error {
			gpsChan <- m
			return nil
		})
		messenger.Subscribe(context.Background(), "test", "metrics", "owntracks", "test", 0, func(m core.Message) error {
			metricsChan <- m
			return nil
		})
		messenger.Subscribe(context.Background(), "test", "events", "owntracks", "test", 0, func(m core.Message) error {
			eventsChan <- m
			return nil
		})
		messenger.Subscribe(context.Background(), "test", "owntracks", "owntracks", "test", 0, func(m core.Message) error {
			owntracksChan <- m
			return nil
		})

		correlationID := "test-uuid-123"
		testData := map[string]interface{}{
			"uuid":      correlationID,
			"_type":     "location",
			"topic":     "owntracks/user/device1",
			"lat":       51.5033,
			"lon":       -0.1195,
			"batt":      float64(85),
			"tid":       "ne",
			"inregions": []interface{}{"home"},
		}
		body, _ := json.Marshal(testData)
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(body))
		rr := httptest.NewRecorder()

		svc.(*Service).ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)

		// Check owntracks message
		select {
		case msg := <-owntracksChan:
			// Drain previous messages if any (unlikely but safe)
			for msg.Uuid != correlationID {
				select {
				case msg = <-owntracksChan:
				case <-time.After(100 * time.Millisecond):
					t.Fatal("timed out waiting for owntracks message with correlationID")
				}
			}
			assert.Equal(t, "owntracks", msg.ChannelName)
			assert.Equal(t, correlationID, msg.Uuid)
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for owntracks message")
		}

		// Check gps message
		select {
		case msg := <-gpsChan:
			for msg.Uuid != correlationID {
				select {
				case msg = <-gpsChan:
				case <-time.After(100 * time.Millisecond):
					t.Fatal("timed out waiting for gps message with correlationID")
				}
			}
			assert.Equal(t, "gps", msg.ChannelName)
			assert.Equal(t, correlationID, msg.Uuid)
			data := msg.Data.(map[string]interface{})
			assert.Equal(t, 51.5033, data["lat"])
			assert.Equal(t, 2, len(data)) // lat, lon
			assert.Nil(t, data["batt"])
			assert.Nil(t, data["tid"])
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for gps message")
		}

		// Check metrics message
		select {
		case msg := <-metricsChan:
			for msg.Uuid != correlationID {
				select {
				case msg = <-metricsChan:
				case <-time.After(100 * time.Millisecond):
					t.Fatal("timed out waiting for metrics message with correlationID")
				}
			}
			assert.Equal(t, "metrics", msg.ChannelName)
			assert.Equal(t, correlationID, msg.Uuid)
			assert.Equal(t, float64(85), msg.Metric)
			assert.Equal(t, "test-owntracks", msg.ServiceName)
			assert.Equal(t, "owntracks", msg.ServiceType)
			data := msg.Data.(map[string]interface{})
			assert.Equal(t, "ne", data["tid"])
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for metrics message")
		}

		// Check events message (enter home)
		select {
		case msg := <-eventsChan:
			for msg.Uuid != correlationID {
				select {
				case msg = <-eventsChan:
				case <-time.After(100 * time.Millisecond):
					t.Fatal("timed out waiting for events message with correlationID")
				}
			}
			assert.Equal(t, "events", msg.ChannelName)
			assert.Equal(t, correlationID, msg.Uuid)
			assert.Equal(t, "test-owntracks", msg.ServiceName)
			assert.Equal(t, "owntracks", msg.ServiceType)
			data := msg.Data.(map[string]interface{})
			assert.Equal(t, "enter", data["event"])
			assert.Equal(t, "home", data["region"])
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for events message")
		}

		// Second request: exit home, enter work
		testData2 := map[string]interface{}{
			"_type":     "location",
			"topic":     "owntracks/user/device1",
			"lat":       51.5,
			"lon":       -0.12,
			"inregions": []interface{}{"work"},
		}
		body2, _ := json.Marshal(testData2)
		req2 := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(body2))
		rr2 := httptest.NewRecorder()
		svc.(*Service).ServeHTTP(rr2, req2)
		assert.Equal(t, http.StatusOK, rr2.Code)

		// Should get exit home and enter work
		var events []core.Message
		for i := 0; i < 2; i++ {
			select {
			case msg := <-eventsChan:
				events = append(events, msg)
			case <-time.After(500 * time.Millisecond):
				t.Fatalf("timed out waiting for event %d", i)
			}
		}

		assert.Len(t, events, 2)
		// One should be exit home, other enter work
		foundExit := false
		foundEnter := false
		for _, e := range events {
			data := e.Data.(map[string]interface{})
			if data["event"] == "exit" && data["region"] == "home" {
				foundExit = true
			}
			if data["event"] == "enter" && data["region"] == "work" {
				foundEnter = true
			}
		}
		assert.True(t, foundExit, "did not find exit home event")
		assert.True(t, foundEnter, "did not find enter work event")
	})

	t.Run("Device Name Mapping", func(t *testing.T) {
		cfgWithDevices := core.ServiceConfig{
			Name: "test-owntracks",
			Type: "owntracks",
			Config: map[string]interface{}{
				"port": 8080,
				"devices": map[string]interface{}{
					"iphone": "phone",
				},
			},
			Pubs: map[string]core.ChannelInfo{
				"owntracks": {Name: "owntracks"},
				"gps":       {Name: "gps"},
				"metrics":   {Name: "metrics"},
				"events":    {Name: "events"},
			},
		}
		svcWithDevices := NewService(deps, cfgWithDevices)

		messenger := deps.MustGetMessenger().(*core.Messenger)
		metricsChan := make(chan core.Message, 10)
		messenger.Subscribe(context.Background(), "test-device", "metrics", "owntracks", "test", 0, func(m core.Message) error {
			metricsChan <- m
			return nil
		})

		correlationID := "device-mapping-uuid"
		testData := map[string]interface{}{
			"uuid":  correlationID,
			"_type": "location",
			"topic": "owntracks/user/iphone",
			"batt":  float64(90),
		}
		body, _ := json.Marshal(testData)
		// Use topic that maps to "phone"
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(body))
		rr := httptest.NewRecorder()

		svcWithDevices.(*Service).ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		// Check response body
		var resp map[string]string
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Equal(t, "phone", resp["device"])

		// Check metrics message
		select {
		case msg := <-metricsChan:
			for msg.Uuid != correlationID {
				select {
				case msg = <-metricsChan:
				case <-time.After(100 * time.Millisecond):
					t.Fatal("timed out waiting for metrics message with correlationID")
				}
			}
			assert.Equal(t, "test-owntracks-phone", msg.ServiceName)
			assert.Equal(t, "battery.phone", msg.MetricName)
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for metrics message")
		}
	})
}

func TestService_Check(t *testing.T) {
	deps := testDeps()
	defer deps.MustGetCancel()()
	cfg := core.ServiceConfig{}
	svc := NewService(deps, cfg)

	err := svc.Check()
	assert.NoError(t, err)
}
