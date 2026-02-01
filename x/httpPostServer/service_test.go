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
		expectError bool
	}{
		{
			name: "valid config",
			config: map[string]interface{}{
				"port":      8080,
				"targetDir": ".",
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
			}
			svc := NewService(deps, cfg)
			errs := svc.ValidateConfig()

			if tt.expectError {
				assert.NotEmpty(t, errs)
			} else {
				assert.Empty(t, errs)
			}

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
	os.Remove(filename)
}

func TestService_FileLogging(t *testing.T) {
	deps := testDeps()

	port := 8889
	cfg := core.ServiceConfig{
		Name: "test-httpPostServer-file",
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

	serviceName := "TestService"
	testMsg := map[string]string{
		"ServiceName": serviceName,
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

	// Check if file exists
	today := time.Now().Format("20060102")
	expectedFilename := fmt.Sprintf("httpPostServer_%s_%s.jsonl", serviceName, today)

	_, err = os.Stat(expectedFilename)
	assert.NoError(t, err, "Log file should exist")

	if err == nil {
		os.Remove(expectedFilename)
	}
}

func TestService_FileLogging_InvalidServiceName(t *testing.T) {
	deps := testDeps()

	port := 8890
	cfg := core.ServiceConfig{
		Name: "test-httpPostServer-invalid",
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

	// Invalid service names
	invalidNames := []string{"Test Service", "Test/Service", "Test..Service", ""}

	for _, name := range invalidNames {
		testMsg := map[string]string{
			"ServiceName": name,
			"foo":         "bar",
		}
		body, _ := json.Marshal(testMsg)

		resp, err := http.Post(
			fmt.Sprintf("http://localhost:%d/", port),
			"application/json",
			bytes.NewBuffer(body),
		)

		assert.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

		// Check if file exists (it should NOT)
		today := time.Now().Format("20060102")
		filename := fmt.Sprintf("%s_%s.json", name, today)

		_, err = os.Stat(filename)
		assert.True(t, os.IsNotExist(err), "Log file should NOT exist for name: %s", name)

		if err == nil {
			os.Remove(filename)
		}
	}
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

func TestService_MissingServiceName(t *testing.T) {
	deps := testDeps()

	cfg := core.ServiceConfig{
		Name: "test-httpPostServer-missing-service-name",
		Type: "httpPostServer",
		Config: map[string]interface{}{
			"port":      8895,
			"targetDir": ".",
		},
	}

	svc := NewService(deps, cfg)

	// JSON body missing ServiceName
	msg := map[string]interface{}{
		"foo": "bar",
	}
	body, _ := json.Marshal(msg)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()

	svc.(*Service).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "Missing or invalid ServiceName")
}

func TestService_FailedToOpenFileForAppending(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	fakeOs := core.FakeOsProvider{Host: "test-host"}
	fakeOs.OpenFileFunc = func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
		return nil, fmt.Errorf("simulated open error")
	}

	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(logger)
	deps.SetMessenger(core.NewMessenger(logger, fakeOs))

	cfg := core.ServiceConfig{
		Name: "test-httpPostServer-fail-open",
		Type: "httpPostServer",
		Config: map[string]interface{}{
			"port":      8896,
			"targetDir": ".",
		},
	}

	svc := NewService(deps, cfg)

	serviceName := "FailOpenService"
	msg := map[string]interface{}{
		"ServiceName": serviceName,
		"foo":         "bar",
	}
	body, _ := json.Marshal(msg)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()

	svc.(*Service).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "failed to open file for appending")
}

type errorWriterFile struct {
	core.FileApi
}

func (e *errorWriterFile) Write(p []byte) (n int, err error) {
	return 0, fmt.Errorf("simulated write error")
}

func (e *errorWriterFile) Close() error {
	return nil
}

type errorNewlineWriterFile struct {
	core.FileApi
}

func (e *errorNewlineWriterFile) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (e *errorNewlineWriterFile) WriteString(s string) (n int, err error) {
	return 0, fmt.Errorf("simulated write string error")
}

func (e *errorNewlineWriterFile) Close() error {
	return nil
}

func TestService_FailedToWriteJsonToFile(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	fakeOs := core.FakeOsProvider{Host: "test-host"}
	fakeOs.OpenFileFunc = func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
		return &errorWriterFile{}, nil
	}

	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(logger)
	deps.SetMessenger(core.NewMessenger(logger, fakeOs))

	cfg := core.ServiceConfig{
		Name: "test-httpPostServer-fail-write",
		Type: "httpPostServer",
		Config: map[string]interface{}{
			"port":      8897,
			"targetDir": ".",
		},
	}

	svc := NewService(deps, cfg)

	serviceName := "FailWriteService"
	msg := map[string]interface{}{
		"ServiceName": serviceName,
		"foo":         "bar",
	}
	body, _ := json.Marshal(msg)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()

	svc.(*Service).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "failed to write json to file")
}

func TestService_FailedToWriteNewlineToFile(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	fakeOs := core.FakeOsProvider{Host: "test-host"}
	fakeOs.OpenFileFunc = func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
		return &errorNewlineWriterFile{}, nil
	}

	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(logger)
	deps.SetMessenger(core.NewMessenger(logger, fakeOs))

	cfg := core.ServiceConfig{
		Name: "test-httpPostServer-fail-newline",
		Type: "httpPostServer",
		Config: map[string]interface{}{
			"port":      8898,
			"targetDir": ".",
		},
	}

	svc := NewService(deps, cfg)

	serviceName := "FailNewlineService"
	msg := map[string]interface{}{
		"ServiceName": serviceName,
		"foo":         "bar",
	}
	body, _ := json.Marshal(msg)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()

	svc.(*Service).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "failed to write newline to file")
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
