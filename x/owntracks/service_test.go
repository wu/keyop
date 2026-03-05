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
			expectError: false,
		},
		{
			name:        "missing port",
			config:      map[string]interface{}{},
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

	cfg := core.ServiceConfig{
		Name: "test-owntracks",
		Type: "owntracks",
		Config: map[string]interface{}{
			"port": 0,
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

	port := svc.(*Service).Port
	assert.NotZero(t, port)

	// Send an OwnTracks JSON message
	testData := map[string]interface{}{
		"_type": "location",
		"lat":   51.5033,
		"lon":   -0.1195,
		"tid":   "ne",
	}
	body, _ := json.Marshal(testData)

	var resp *http.Response
	var reqErr error
	for i := 0; i < 10; i++ {
		resp, reqErr = http.Post(
			fmt.Sprintf("http://localhost:%d/", port),
			"application/json",
			bytes.NewBuffer(body),
		)
		if reqErr == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	assert.NoError(t, reqErr)
	if resp != nil {
		if err := resp.Body.Close(); err != nil {
			t.Logf("failed to close response body: %v", err)
		}
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}
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
		allMsgs := make(chan core.Message, 20)

		if err := messenger.Subscribe(context.Background(), "test", "test-owntracks", "owntracks", "test", 0, func(m core.Message) error {
			allMsgs <- m
			return nil
		}); err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}

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

		// Collect all messages for this correlation
		var msgs []core.Message
		deadline := time.After(500 * time.Millisecond)
		for {
			select {
			case msg := <-allMsgs:
				if msg.Correlation == correlationID {
					msgs = append(msgs, msg)
				}
			case <-deadline:
				goto done1
			}
		}
	done1:
		// We expect: location, gps, battery_metric, region_enter(home)
		assert.GreaterOrEqual(t, len(msgs), 3, "expected at least 3 messages")

		events := map[string]core.Message{}
		for _, m := range msgs {
			events[m.Event] = m
		}
		assert.Contains(t, events, "location")
		assert.Contains(t, events, "gps")
		assert.Contains(t, events, "battery_metric")
		assert.Contains(t, events, "region_enter")

		gpsMsg := events["gps"]
		data := gpsMsg.Data.(map[string]interface{})
		assert.Equal(t, 51.5033, data["lat"])

		battMsg := events["battery_metric"]
		assert.Equal(t, float64(85), battMsg.Metric)

		enterMsg := events["region_enter"]
		enterData := enterMsg.Data.(map[string]interface{})
		assert.Equal(t, "enter", enterData["event"])
		assert.Equal(t, "home", enterData["region"])

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

		var regionMsgs []core.Message
		deadline2 := time.After(500 * time.Millisecond)
		for {
			select {
			case msg := <-allMsgs:
				if msg.Event == "region_exit" || msg.Event == "region_enter" {
					regionMsgs = append(regionMsgs, msg)
				}
			case <-deadline2:
				goto done2
			}
		}
	done2:
		foundExit := false
		foundEnter := false
		for _, e := range regionMsgs {
			d := e.Data.(map[string]interface{})
			if d["event"] == "exit" && d["region"] == "home" {
				foundExit = true
			}
			if d["event"] == "enter" && d["region"] == "work" {
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
		}
		svcWithDevices := NewService(deps, cfgWithDevices)

		messenger := deps.MustGetMessenger().(*core.Messenger)
		allMsgs2 := make(chan core.Message, 10)
		if err := messenger.Subscribe(context.Background(), "test-device", "test-owntracks", "owntracks", "test-device", 0, func(m core.Message) error {
			allMsgs2 <- m
			return nil
		}); err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}

		correlationID := "device-mapping-uuid"
		testData := map[string]interface{}{
			"uuid":  correlationID,
			"_type": "location",
			"topic": "owntracks/user/iphone",
			"batt":  float64(90),
		}
		body, _ := json.Marshal(testData)
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(body))
		rr := httptest.NewRecorder()

		svcWithDevices.(*Service).ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		var resp map[string]string
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Equal(t, "phone", resp["device"])

		// Check battery_metric message
		var battMsg *core.Message
		deadline3 := time.After(500 * time.Millisecond)
		for battMsg == nil {
			select {
			case msg := <-allMsgs2:
				if msg.Event == "battery_metric" && msg.Correlation == correlationID {
					m := msg
					battMsg = &m
				}
			case <-deadline3:
				goto done3
			}
		}
	done3:
		if battMsg != nil {
			assert.Equal(t, "test-owntracks-phone", battMsg.ServiceName)
			assert.Equal(t, "battery.phone", battMsg.MetricName)
		} else {
			t.Fatal("timed out waiting for battery_metric message")
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
