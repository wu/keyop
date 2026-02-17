package httpPostClient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"keyop/core"
	"keyop/util"
	"log/slog"
	"math"
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

	ctx, cancel := context.WithCancel(context.Background())
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	t.Cleanup(cancel)

	tmpDir, err := os.MkdirTemp("", "httpPost_test")
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
		pubs        map[string]core.ChannelInfo
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
			pubs: map[string]core.ChannelInfo{
				"errors": {Name: "errors-channel"},
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
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := core.ServiceConfig{
				Config: tt.config,
				Subs:   tt.subs,
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
				"port":     8080,
				"hostname": "localhost",
			},
			Subs: map[string]core.ChannelInfo{
				"temp": {Name: "temp-channel"},
			},
			Pubs: map[string]core.ChannelInfo{
				"errors": {Name: "errors-channel"},
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

func TestService_Initialize(t *testing.T) {
	deps := testDeps(t)
	cfg := core.ServiceConfig{
		Name: "test-httpPostClient",
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

//goland:noinspection GoUnhandledErrorResult
func TestService_MessageHandler_Success(t *testing.T) {
	deps := testDeps(t)

	// Load certs for the mock server
	osProvider := deps.MustGetOsProvider()
	home, _ := osProvider.UserHomeDir()
	certsDir := filepath.Join(home, ".keyop", "certs")
	serverCertPEM, _ := osProvider.ReadFile(filepath.Join(certsDir, "keyop-server.crt"))
	serverKeyPEM, _ := osProvider.ReadFile(filepath.Join(certsDir, "keyop-server.key"))
	serverCert, _ := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	caCertPEM, _ := osProvider.ReadFile(filepath.Join(certsDir, "ca.crt"))
	caCAPool := x509.NewCertPool()
	caCAPool.AppendCertsFromPEM(caCertPEM)

	done := make(chan bool)
	// Create a mock HTTPS server with mutual TLS
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caCAPool,
	}
	server.StartTLS()
	defer server.Close()

	// Parse the server URL to get hostname and port
	var hostname string
	var port int
	fmt.Sscanf(server.URL, "https://%s", &hostname)
	addr := server.Listener.Addr().String()
	fmt.Sscanf(addr, "127.0.0.1:%d", &port)
	if port == 0 {
		fmt.Sscanf(addr, "[::]:%d", &port)
	}

	cfg := core.ServiceConfig{
		Name: "test-httpPostClient",
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
		Name: "test-httpPostClient",
		Subs: map[string]core.ChannelInfo{
			"heartbeat": {Name: "heartbeat-channel"},
		},
		Config: map[string]interface{}{
			"port":     1, // Unlikely to have a server on port 1
			"hostname": "127.0.0.1",
		},
	}
	svc := NewService(deps, cfg).(*Service)
	err := svc.Initialize()
	assert.NoError(t, err)

	// Directly call messageHandler to test its error return
	testMsg := core.Message{
		ServiceName: "test-service",
		Data:        "test-data",
	}
	err = svc.messageHandler(testMsg)
	assert.Error(t, err)
}

func TestService_MessageHandler_MarshalError(t *testing.T) {
	deps := testDeps(t)
	svc := NewService(deps, core.ServiceConfig{
		Config: map[string]interface{}{
			"port":     8080,
			"hostname": "localhost",
		},
	}).(*Service)
	err := svc.Initialize()
	assert.NoError(t, err)

	testMsg := core.Message{
		Metric: math.NaN(),
	}
	err = svc.messageHandler(testMsg)
	assert.Error(t, err)
	//goland:noinspection GoMaybeNil
	assert.Contains(t, err.Error(), "json: unsupported value")
}

func TestService_MessageHandler_CreateRequestError(t *testing.T) {
	deps := testDeps(t)

	// Use an invalid hostname/port combination that will cause http.NewRequestWithContext to fail.
	// A URL with a control character or other invalid characters should do it.
	cfg := core.ServiceConfig{
		Name: "test-httpPostClient",
		Config: map[string]interface{}{
			"port":     8080,
			"hostname": "host\x7f", // DEL character is invalid in URL
		},
	}
	svc := NewService(deps, cfg).(*Service)
	err := svc.Initialize()
	assert.NoError(t, err)

	testMsg := core.Message{
		ServiceName: "test-service",
		Data:        "test-data",
	}

	err = svc.messageHandler(testMsg)
	assert.Error(t, err)
	//goland:noinspection GoMaybeNil
	assert.Contains(t, err.Error(), "invalid control character in URL")
}

//goland:noinspection GoUnhandledErrorResult
func TestService_MessageHandler_Timeout(t *testing.T) {
	deps := testDeps(t)

	// Create a mock HTTPS server that sleeps longer than the timeout
	osProvider := deps.MustGetOsProvider()
	home, _ := osProvider.UserHomeDir()
	certsDir := filepath.Join(home, ".keyop", "certs")
	serverCertPEM, _ := osProvider.ReadFile(filepath.Join(certsDir, "keyop-server.crt"))
	serverKeyPEM, _ := osProvider.ReadFile(filepath.Join(certsDir, "keyop-server.key"))
	serverCert, _ := tls.X509KeyPair(serverCertPEM, serverKeyPEM)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
	}
	server.StartTLS()
	defer server.Close()

	// Parse the server URL to get hostname and port
	var hostname string
	var port int
	fmt.Sscanf(server.URL, "https://%s", &hostname)
	addr := server.Listener.Addr().String()
	fmt.Sscanf(addr, "127.0.0.1:%d", &port)
	if port == 0 {
		fmt.Sscanf(addr, "[::]:%d", &port)
	}

	cfg := core.ServiceConfig{
		Name: "test-httpPostClient",
		Config: map[string]interface{}{
			"port":     port,
			"hostname": "127.0.0.1",
			"timeout":  "100ms", // Short timeout
		},
	}
	svc := NewService(deps, cfg).(*Service)
	err := svc.Initialize()
	assert.NoError(t, err)

	testMsg := core.Message{
		ServiceName: "test-service",
		Data:        "test-data",
	}

	err = svc.messageHandler(testMsg)
	assert.Error(t, err)
	//goland:noinspection GoMaybeNil
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestService_MessageHandler_UntrustedServerCert(t *testing.T) {
	deps := testDeps(t)

	// Create a different CA and a server certificate signed by it
	tmpDir, _ := os.MkdirTemp("", "untrusted_server")
	defer os.RemoveAll(tmpDir)
	_ = util.GenerateTestCerts(tmpDir)

	serverCertPEM, _ := os.ReadFile(filepath.Join(tmpDir, "keyop-server.crt"))
	serverKeyPEM, _ := os.ReadFile(filepath.Join(tmpDir, "keyop-server.key"))
	serverCert, _ := tls.X509KeyPair(serverCertPEM, serverKeyPEM)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
	}
	server.StartTLS()
	defer server.Close()

	var hostname string
	var port int
	fmt.Sscanf(server.URL, "https://%s", &hostname)
	addr := server.Listener.Addr().String()
	fmt.Sscanf(addr, "127.0.0.1:%d", &port)
	if port == 0 {
		fmt.Sscanf(addr, "[::]:%d", &port)
	}

	cfg := core.ServiceConfig{
		Name: "test-httpPostClient",
		Config: map[string]interface{}{
			"port":     port,
			"hostname": "127.0.0.1",
		},
	}
	svc := NewService(deps, cfg).(*Service)
	err := svc.Initialize()
	assert.NoError(t, err)

	testMsg := core.Message{
		ServiceName: "test-service",
		Data:        "test-data",
	}

	err = svc.messageHandler(testMsg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "x509: certificate signed by unknown authority")
}

func TestService_MessageHandler_RoutingLoop(t *testing.T) {
	deps := testDeps(t)

	cfg := core.ServiceConfig{
		Name: "test-httpPostClient",
		Config: map[string]interface{}{
			"port":                 8080,
			"hostname":             "localhost",
			"route_loop_skip_host": "skip-this-host",
		},
	}
	svc := NewService(deps, cfg).(*Service)
	err := svc.Initialize()
	assert.NoError(t, err)

	// Message with route containing the skip host
	testMsg := core.Message{
		ServiceName: "test-service",
		Route:       []string{"skip-this-host:service:name", "other-host:service:name"},
	}

	// Should return nil (skipped) and NOT try to send the message
	// Since we haven't set up a mock server, if it tries to send it will fail with an error.
	err = svc.messageHandler(testMsg)
	assert.NoError(t, err)

	// Message without the skip host in route should proceed to try and send
	testMsg2 := core.Message{
		ServiceName: "test-service",
		Route:       []string{"allowed-host:service:name"},
	}
	err = svc.messageHandler(testMsg2)
	// Should error because there is no server running on 8080
	assert.Error(t, err)
}

func TestService_Check(t *testing.T) {
	deps := testDeps(t)
	svc := NewService(deps, core.ServiceConfig{})
	assert.NoError(t, svc.Check())
}
