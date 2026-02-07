package owntracks

import (
	"bytes"
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
	deps.SetOsProvider(core.OsProvider{})
	deps.SetLogger(logger)
	deps.SetMessenger(core.NewMessenger(logger, deps.MustGetOsProvider()))

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

	port := 8878
	cfg := core.ServiceConfig{
		Name: "test-owntracks",
		Type: "owntracks",
		Config: map[string]interface{}{
			"port": port,
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
	cfg := core.ServiceConfig{
		Config: map[string]interface{}{"port": 8080},
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
}

func TestService_Check(t *testing.T) {
	deps := testDeps()
	cfg := core.ServiceConfig{}
	svc := NewService(deps, cfg)

	err := svc.Check()
	assert.NoError(t, err)
}
