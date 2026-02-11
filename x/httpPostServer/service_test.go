package httpPostServer

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"keyop/core"
	"keyop/util"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// helper to build dependencies
func testDeps(t *testing.T) core.Dependencies {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}

	tmpDir, err := os.MkdirTemp("", "httpPostServer_test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		//goland:noinspection GoUnhandledErrorResult
		os.RemoveAll(tmpDir)
	})

	// Setup .keyop/certs
	certsDir := filepath.Join(tmpDir, ".keyop", "certs")
	err = util.GenerateTestCerts(certsDir)
	if err != nil {
		t.Fatal(err)
	}

	fakeOs := core.FakeOsProvider{
		Host:         "test-host",
		Home:         tmpDir,
		ReadFileFunc: os.ReadFile,
		StatFunc:     os.Stat,
		OpenFileFunc: func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
			return os.OpenFile(name, flag, perm)
		},
		MkdirAllFunc: os.MkdirAll,
		ReadDirFunc:  os.ReadDir,
		RemoveFunc:   os.Remove,
		ChtimesFunc:  os.Chtimes,
	}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(logger)
	deps.SetMessenger(core.NewMessenger(logger, deps.MustGetOsProvider()))

	return deps
}

func TestService_ValidateConfig(t *testing.T) {
	deps := testDeps(t)

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

	t.Run("missing ca.crt", func(t *testing.T) {
		deps := testDeps(t)
		osProvider := deps.MustGetOsProvider()
		home, _ := osProvider.UserHomeDir()
		caPath := filepath.Join(home, ".keyop", "certs", "ca.crt")
		_ = os.Remove(caPath)

		cfg := core.ServiceConfig{
			Config: map[string]interface{}{
				"port":      8080,
				"targetDir": ".",
			},
			Pubs: map[string]core.ChannelInfo{
				"errors": {Name: "errors-topic"},
			},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.NotEmpty(t, errs)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "CA certificate not found") {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected error containing 'CA certificate not found', but got: %v", errs)
	})
}

func TestService_Initialize_StartsServerAndLogs(t *testing.T) {
	deps := testDeps(t)

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

	// Send a JSON message with TLS client
	osProvider := deps.MustGetOsProvider()
	home, _ := osProvider.UserHomeDir()
	certsDir := filepath.Join(home, ".keyop", "certs")
	clientCertPEM, err := osProvider.ReadFile(filepath.Join(certsDir, "keyop-client.crt"))
	assert.NoError(t, err)
	clientKeyPEM, err := osProvider.ReadFile(filepath.Join(certsDir, "keyop-client.key"))
	assert.NoError(t, err)
	clientCert, err := tls.X509KeyPair(clientCertPEM, clientKeyPEM)
	assert.NoError(t, err)

	serverCACert, err := osProvider.ReadFile(filepath.Join(certsDir, "ca.crt"))
	assert.NoError(t, err)
	serverCAPool := x509.NewCertPool()
	serverCAPool.AppendCertsFromPEM(serverCACert)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{clientCert},
				RootCAs:      serverCAPool,
			},
		},
	}

	testMsg := map[string]string{
		"ChannelName": "TestChannel",
		"ServiceName": "TestSvc",
		"foo":         "bar",
	}
	body, _ := json.Marshal(testMsg)
	resp, err := client.Post(
		fmt.Sprintf("https://localhost:%d/", port),
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
	deps := testDeps(t)

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

	// Setup TLS client
	osProvider := deps.MustGetOsProvider()
	home, _ := osProvider.UserHomeDir()
	certsDir := filepath.Join(home, ".keyop", "certs")
	clientCertPEM, _ := osProvider.ReadFile(filepath.Join(certsDir, "keyop-client.crt"))
	clientKeyPEM, _ := osProvider.ReadFile(filepath.Join(certsDir, "keyop-client.key"))
	clientCert, _ := tls.X509KeyPair(clientCertPEM, clientKeyPEM)
	serverCACert, _ := osProvider.ReadFile(filepath.Join(certsDir, "ca.crt"))
	serverCAPool := x509.NewCertPool()
	serverCAPool.AppendCertsFromPEM(serverCACert)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{clientCert},
				RootCAs:      serverCAPool,
			},
		},
	}

	// Send a GET request instead of POST
	resp, err := client.Get(fmt.Sprintf("https://localhost:%d/", port))

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
	deps := testDeps(t)

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
	deps := testDeps(t)

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

	deps := testDeps(t)
	deps.SetLogger(logger)

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
	assert.Contains(t, logOutput, "https server failed")
	assert.Contains(t, logOutput, "invalid port")
}

func TestService_Check(t *testing.T) {
	deps := testDeps(t)
	cfg := core.ServiceConfig{}
	svc := NewService(deps, cfg)

	err := svc.Check()
	assert.NoError(t, err)
}

func TestService_NoClientCert_Fails(t *testing.T) {
	deps := testDeps(t)

	port := 8878
	cfg := core.ServiceConfig{
		Name: "test-httpPostServer-no-cert",
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

	// Client without certificate
	osProvider := deps.MustGetOsProvider()
	home, _ := osProvider.UserHomeDir()
	certsDir := filepath.Join(home, ".keyop", "certs")
	serverCACert, _ := osProvider.ReadFile(filepath.Join(certsDir, "ca.crt"))
	serverCAPool := x509.NewCertPool()
	serverCAPool.AppendCertsFromPEM(serverCACert)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: serverCAPool,
			},
		},
	}

	_, err = client.Post(fmt.Sprintf("https://localhost:%d/", port), "application/json", nil)
	assert.Error(t, err)
}

func TestService_UntrustedClientCert_Fails(t *testing.T) {
	deps := testDeps(t)

	port := 8879
	cfg := core.ServiceConfig{
		Name: "test-httpPostServer-untrusted-cert",
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

	// Create an untrusted certificate (self-signed, not signed by our CA)
	tmpDir, _ := os.MkdirTemp("", "untrusted_cert")
	defer os.RemoveAll(tmpDir)
	err = util.GenerateTestCerts(tmpDir)
	assert.NoError(t, err)

	untrustedCertPEM, _ := os.ReadFile(filepath.Join(tmpDir, "keyop-client.crt"))
	untrustedKeyPEM, _ := os.ReadFile(filepath.Join(tmpDir, "keyop-client.key"))
	untrustedCert, _ := tls.X509KeyPair(untrustedCertPEM, untrustedKeyPEM)

	osProvider := deps.MustGetOsProvider()
	home, _ := osProvider.UserHomeDir()
	certsDir := filepath.Join(home, ".keyop", "certs")
	serverCACert, _ := osProvider.ReadFile(filepath.Join(certsDir, "ca.crt"))
	serverCAPool := x509.NewCertPool()
	serverCAPool.AppendCertsFromPEM(serverCACert)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{untrustedCert},
				RootCAs:      serverCAPool,
			},
		},
	}

	_, err = client.Post(fmt.Sprintf("https://localhost:%d/", port), "application/json", nil)
	assert.Error(t, err)
}

func TestService_Initialize_FailedToCreateTargetDirectory(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	tmpDir, _ := os.MkdirTemp("", "httpPostServer_fail_mkdir")
	defer os.RemoveAll(tmpDir)
	certsDir := filepath.Join(tmpDir, ".keyop", "certs")
	_ = util.GenerateTestCerts(certsDir)

	fakeOs := core.FakeOsProvider{
		Host:         "test-host",
		Home:         tmpDir,
		ReadFileFunc: os.ReadFile,
		StatFunc:     os.Stat,
		OpenFileFunc: func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
			return os.OpenFile(name, flag, perm)
		},
		ReadDirFunc: os.ReadDir,
		RemoveFunc:  os.Remove,
		ChtimesFunc: os.Chtimes,
	}
	expectedErr := fmt.Errorf("permission denied")
	fakeOs.MkdirAllFunc = func(path string, perm os.FileMode) error {
		if path == "/restricted-dir" {
			return expectedErr
		}
		return os.MkdirAll(path, perm)
	}

	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(logger)
	deps.SetMessenger(core.NewMessenger(logger, deps.MustGetOsProvider()))

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
