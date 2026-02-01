package httpPost

import (
	"encoding/json"
	"fmt"
	"io"
	"keyop/core"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// helper to build dependencies
func testDeps(t *testing.T) core.Dependencies {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}

	tmpDir, err := os.MkdirTemp("", "httpPost_test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	deps.SetOsProvider(core.OsProvider{})
	deps.SetLogger(logger)
	messenger := core.NewMessenger(logger, deps.MustGetOsProvider())
	messenger.SetDataDir(tmpDir)

	deps.SetMessenger(messenger)

	return deps
}

func TestService_ValidateConfig(t *testing.T) {
	deps := testDeps(t)

	tests := []struct {
		name        string
		config      map[string]interface{}
		subs        map[string]core.ChannelInfo
		expectError bool
	}{
		{
			name: "valid config",
			config: map[string]interface{}{
				"port":     8080,
				"hostname": "localhost",
			},
			subs: map[string]core.ChannelInfo{
				"temp": {Name: "temp-channel"},
			},
			expectError: false,
		},
		{
			name: "missing port",
			config: map[string]interface{}{
				"hostname": "localhost",
			},
			subs: map[string]core.ChannelInfo{
				"temp": {Name: "temp-channel"},
			},
			expectError: true,
		},
		{
			name: "missing hostname",
			config: map[string]interface{}{
				"port": 8080,
			},
			subs: map[string]core.ChannelInfo{
				"temp": {Name: "temp-channel"},
			},
			expectError: true,
		},
		{
			name: "missing subscription",
			config: map[string]interface{}{
				"port":     8080,
				"hostname": "localhost",
			},
			subs:        map[string]core.ChannelInfo{},
			expectError: true,
		},
		{
			name: "missing temp subscription",
			config: map[string]interface{}{
				"port":     8080,
				"hostname": "localhost",
			},
			subs: map[string]core.ChannelInfo{
				"heartbeat": {Name: "heartbeat-channel"},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := core.ServiceConfig{
				Config: tt.config,
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

func TestService_Initialize(t *testing.T) {
	deps := testDeps(t)
	cfg := core.ServiceConfig{
		Name: "test-httpPost",
		Subs: map[string]core.ChannelInfo{
			"heartbeat": {Name: "heartbeat-channel"},
		},
		Config: map[string]interface{}{
			"port":     8080,
			"hostname": "localhost",
		},
	}
	svc := NewService(deps, cfg)

	err := svc.Initialize()
	assert.NoError(t, err)
}

func TestService_MessageHandler_Success(t *testing.T) {
	deps := testDeps(t)

	done := make(chan bool)
	// Create a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var msg core.Message
		body, _ := io.ReadAll(r.Body)
		err := json.Unmarshal(body, &msg)
		assert.NoError(t, err)
		assert.Equal(t, "test-service", msg.ServiceName)
		assert.Equal(t, "test-data", msg.Data.(string))

		w.WriteHeader(http.StatusOK)
		done <- true
	}))
	defer server.Close()

	// Parse the server URL to get hostname and port
	var hostname string
	var port int
	fmt.Sscanf(server.URL, "http://%s", &hostname)
	addr := server.Listener.Addr().String()
	fmt.Sscanf(addr, "127.0.0.1:%d", &port)
	if port == 0 {
		fmt.Sscanf(addr, "[::]:%d", &port)
	}

	cfg := core.ServiceConfig{
		Name: "test-httpPost",
		Subs: map[string]core.ChannelInfo{
			"heartbeat": {Name: "heartbeat-channel"},
		},
		Config: map[string]interface{}{
			"port":     port,
			"hostname": "127.0.0.1",
		},
	}
	svc := NewService(deps, cfg)

	err := svc.Initialize()
	assert.NoError(t, err)

	// Trigger the message handler via the messenger
	testMsg := core.Message{
		ServiceName: "test-service",
		Data:        "test-data",
	}

	testMsg.ChannelName = "heartbeat-channel"
	err = deps.MustGetMessenger().Send(testMsg)
	assert.NoError(t, err)

	select {
	case <-done:
		// success
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for message processing")
	}
}

func TestService_MessageHandler_PostError(t *testing.T) {
	deps := testDeps(t)

	// Use an invalid port to trigger a post error
	cfg := core.ServiceConfig{
		Name: "test-httpPost",
		Subs: map[string]core.ChannelInfo{
			"heartbeat": {Name: "heartbeat-channel"},
		},
		Config: map[string]interface{}{
			"port":     1, // Unlikely to have a server on port 1
			"hostname": "127.0.0.1",
		},
	}
	svc := NewService(deps, cfg).(*Service)

	// Directly call messageHandler to test its error return
	testMsg := core.Message{
		ServiceName: "test-service",
		Data:        "test-data",
	}
	err := svc.messageHandler(testMsg)
	assert.Error(t, err)
}

func TestService_MessageHandler_MarshalError(t *testing.T) {
	deps := testDeps(t)
	svc := NewService(deps, core.ServiceConfig{}).(*Service)

	testMsg := core.Message{
		Metric: math.NaN(),
	}
	err := svc.messageHandler(testMsg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "json: unsupported value")
}

func TestService_MessageHandler_CreateRequestError(t *testing.T) {
	deps := testDeps(t)

	// Use an invalid hostname/port combination that will cause http.NewRequestWithContext to fail.
	// A URL with a control character or other invalid characters should do it.
	cfg := core.ServiceConfig{
		Name: "test-httpPost",
		Config: map[string]interface{}{
			"port":     8080,
			"hostname": "host\x7f", // DEL character is invalid in URL
		},
	}
	svc := NewService(deps, cfg).(*Service)

	testMsg := core.Message{
		ServiceName: "test-service",
		Data:        "test-data",
	}

	err := svc.messageHandler(testMsg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid control character in URL")
}

func TestService_MessageHandler_Timeout(t *testing.T) {
	deps := testDeps(t)

	// Create a mock HTTP server that sleeps longer than the timeout
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Parse the server URL to get hostname and port
	var hostname string
	var port int
	fmt.Sscanf(server.URL, "http://%s", &hostname)
	addr := server.Listener.Addr().String()
	fmt.Sscanf(addr, "127.0.0.1:%d", &port)
	if port == 0 {
		fmt.Sscanf(addr, "[::]:%d", &port)
	}

	cfg := core.ServiceConfig{
		Name: "test-httpPost",
		Config: map[string]interface{}{
			"port":     port,
			"hostname": "127.0.0.1",
			"timeout":  "100ms", // Short timeout
		},
	}
	svc := NewService(deps, cfg).(*Service)

	testMsg := core.Message{
		ServiceName: "test-service",
		Data:        "test-data",
	}

	err := svc.messageHandler(testMsg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestService_Check(t *testing.T) {
	deps := testDeps(t)
	svc := NewService(deps, core.ServiceConfig{})
	assert.NoError(t, svc.Check())
}
