package httpPostServer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
				"port":      8080,
				"targetDir": ".",
			},
			pubs: map[string]core.ChannelInfo{
				"errors": {Name: "errors-topic"},
			},
			expectError: false,
		},
		{
			name:        "missing port",
			config:      map[string]interface{}{"targetDir": "."},
			expectError: true,
		},
		{
			name:        "missing targetDir",
			config:      map[string]interface{}{"port": 8080},
			expectError: true,
		},
		{
			name: "wrong port type",
			config: map[string]interface{}{
				"port": "8080",
			},
			expectError: true,
		},
	}

	today := time.Now().Format("20060102")
	filename := fmt.Sprintf("httpPostServer_TestSvc_%s.jsonl", today)

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

			//goland:noinspection GoUnhandledErrorResult
			os.Remove(filename)
		})
	}

}

func TestService_Initialize_StartsServerAndLogs(t *testing.T) {
	deps := testDeps()

	port := 8877 // changed from 8888 to avoid conflicts
	cfg := core.ServiceConfig{
		Name: "test-httpPostServer",
		Type: "httpPostServer",
		Config: map[string]interface{}{
			"port":      port,
			"targetDir": ".",
		},
	}

	svc := NewService(deps, cfg)

	err := svc.Initialize()
	assert.NoError(t, err)

	// Give the server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Send a JSON message
	testMsg := map[string]string{
		"ChannelName": "TestChannel",
		"ServiceName": "TestSvc",
		"foo":         "bar",
	}
	body, _ := json.Marshal(testMsg)
	resp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/", port),
		"application/json",
		bytes.NewBuffer(body),
	)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Clean up
	today := time.Now().Format("20060102")
	filename := fmt.Sprintf("httpPostServer_TestSvc_%s.jsonl", today)
	//goland:noinspection GoUnhandledErrorResult
	os.Remove(filename)
}

func TestService_MethodNotAllowed(t *testing.T) {
	deps := testDeps()

	port := 8892
	cfg := core.ServiceConfig{
		Name: "test-httpPostServer-method",
		Type: "httpPostServer",
		Config: map[string]interface{}{
			"port":      port,
			"targetDir": ".",
		},
	}

	svc := NewService(deps, cfg)
	err := svc.Initialize()
	assert.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Send a GET request instead of POST
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/", port))

	assert.NoError(t, err)
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "Method not allowed")
}

type errorReader struct{}

//goland:noinspection GoUnusedParameter
func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, fmt.Errorf("simulated read error")
}

func TestService_ErrorReadingBody(t *testing.T) {
	deps := testDeps()

	cfg := core.ServiceConfig{
		Name: "test-httpPostServer-error-read",
		Type: "httpPostServer",
		Config: map[string]interface{}{
			"port":      8893,
			"targetDir": ".",
		},
	}

	svc := NewService(deps, cfg)

	// Use httptest to test the ServeHTTP method directly
	req := httptest.NewRequest(http.MethodPost, "/", &errorReader{})
	rr := httptest.NewRecorder()

	svc.(*Service).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "Error reading body")
}

func TestService_InvalidJSON(t *testing.T) {
	deps := testDeps()

	cfg := core.ServiceConfig{
		Name: "test-httpPostServer-invalid-json",
		Type: "httpPostServer",
		Config: map[string]interface{}{
			"port":      8894,
			"targetDir": ".",
		},
	}

	svc := NewService(deps, cfg)

	// Invalid JSON body
	invalidJSON := []byte(`{"ServiceName": "TestService", "foo": "bar"`) // Missing closing brace
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(invalidJSON))
	rr := httptest.NewRecorder()

	svc.(*Service).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "Invalid JSON")
}

func TestService_HTTPServerFailed(t *testing.T) {
	// Use a buffer to capture logs
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	deps := core.Dependencies{}
	deps.SetOsProvider(core.OsProvider{})
	deps.SetLogger(logger)
	deps.SetMessenger(core.NewMessenger(logger, deps.MustGetOsProvider()))

	// Use an invalid port to trigger ListenAndServe failure
	cfg := core.ServiceConfig{
		Name: "test-httpPostServer-fail",
		Type: "httpPostServer",
		Config: map[string]interface{}{
			"port":      -1,
			"targetDir": ".",
		},
	}

	svc := NewService(deps, cfg)
	err := svc.Initialize()
	assert.NoError(t, err)

	// Give it a moment to fail and log
	time.Sleep(100 * time.Millisecond)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "http server failed")
	assert.Contains(t, logOutput, "invalid port")
}

func TestService_Check(t *testing.T) {
	deps := testDeps()
	cfg := core.ServiceConfig{}
	svc := NewService(deps, cfg)

	err := svc.Check()
	assert.NoError(t, err)
}

func TestService_Initialize_FailedToCreateTargetDirectory(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	fakeOs := core.FakeOsProvider{Host: "test-host"}
	expectedErr := fmt.Errorf("permission denied")
	fakeOs.MkdirAllFunc = func(path string, perm os.FileMode) error {
		return expectedErr
	}

	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(logger)

	cfg := core.ServiceConfig{
		Name: "test-httpPostServer-fail-mkdir",
		Type: "httpPostServer",
		Config: map[string]interface{}{
			"port":      8899,
			"targetDir": "/restricted-dir",
		},
	}

	svc := NewService(deps, cfg)
	err := svc.Initialize()

	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
}
