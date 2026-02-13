package graphite

import (
	"context"
	"fmt"
	"keyop/core"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDeps(t *testing.T) core.Dependencies {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}

	ctx, cancel := context.WithCancel(context.Background())
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	t.Cleanup(cancel)

	tmpDir, err := os.MkdirTemp("", "graphite_test")
	require.NoError(t, err)
	t.Cleanup(func() {
		//goland:noinspection GoUnhandledErrorResult
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
		pubs        map[string]core.ChannelInfo
		expectError bool
		errMsg      string
	}{
		{
			name: "valid config",
			config: map[string]interface{}{
				"port":     2003,
				"hostname": "localhost",
			},
			subs: map[string]core.ChannelInfo{
				"graphite": {Name: "graphite-channel"},
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
				"graphite": {Name: "graphite-channel"},
			},
			expectError: true,
			errMsg:      "port not set",
		},
		{
			name: "missing hostname",
			config: map[string]interface{}{
				"port": 2003,
			},
			subs: map[string]core.ChannelInfo{
				"graphite": {Name: "graphite-channel"},
			},
			expectError: true,
			errMsg:      "hostname not set",
		},
		{
			name: "missing graphite subscription",
			config: map[string]interface{}{
				"port":     2003,
				"hostname": "localhost",
			},
			subs:        map[string]core.ChannelInfo{},
			expectError: true,
			errMsg:      "required subs channel 'graphite' is missing",
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
				if tt.errMsg != "" {
					found := false
					for _, e := range errs {
						if strings.Contains(e.Error(), tt.errMsg) {
							found = true
							break
						}
					}
					assert.True(t, found, "expected error message containing: %s", tt.errMsg)
				}
			} else {
				assert.Empty(t, errs)
				assert.Equal(t, tt.config["hostname"], svc.(*Service).Host)
				assert.Equal(t, tt.config["port"], svc.(*Service).Port)
			}
		})
	}
}

func TestService_Initialize(t *testing.T) {
	deps := testDeps(t)
	cfg := core.ServiceConfig{
		Name: "graphite-svc",
		Subs: map[string]core.ChannelInfo{
			"graphite": {Name: "graphite-topic"},
		},
	}
	svc := NewService(deps, cfg)
	err := svc.Initialize()
	assert.NoError(t, err)
}

func TestService_MessageHandler(t *testing.T) {
	// Start a local TCP server to mock Graphite
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	//goland:noinspection GoUnhandledErrorResult
	defer ln.Close()

	addr := ln.Addr().(*net.TCPAddr)
	host := addr.IP.String()
	port := addr.Port

	received := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		//goland:noinspection GoUnhandledErrorResult
		defer conn.Close()
		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err == nil {
			received <- string(buf[:n])
		}
	}()

	deps := testDeps(t)
	cfg := core.ServiceConfig{
		Config: map[string]interface{}{
			"hostname": host,
			"port":     port,
		},
		Subs: map[string]core.ChannelInfo{
			"graphite": {Name: "graphite-topic"},
		},
	}
	s := NewService(deps, cfg).(*Service)
	s.Host = host
	s.Port = port

	msg := core.Message{
		ServiceName: "test-service",
		Metric:      123.456,
	}

	err = s.messageHandler(msg)
	assert.NoError(t, err)

	select {
	case data := <-received:
		// Graphite format: <metric_path> <metric_value> <metric_timestamp>
		// Our implementation: metric := graphite.NewMetric(msg.ServiceName, fmt.Sprintf("%v", value), unixTime)
		// and value := fmt.Sprintf("%2.2f", msg.Metric)
		expectedValue := "123.46"
		assert.Contains(t, data, "test-service")
		assert.Contains(t, data, expectedValue)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for graphite metric")
	}
}

func TestService_MessageHandler_UsesMessageTimestamp(t *testing.T) {
	// Start a local TCP server to mock Graphite
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	//goland:noinspection GoUnhandledErrorResult
	defer ln.Close()

	addr := ln.Addr().(*net.TCPAddr)
	host := addr.IP.String()
	port := addr.Port

	received := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		//goland:noinspection GoUnhandledErrorResult
		defer conn.Close()
		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err == nil {
			received <- string(buf[:n])
		}
	}()

	deps := testDeps(t)
	cfg := core.ServiceConfig{
		Config: map[string]interface{}{
			"hostname": host,
			"port":     port,
		},
		Subs: map[string]core.ChannelInfo{
			"graphite": {Name: "graphite-topic"},
		},
	}
	s := NewService(deps, cfg).(*Service)
	s.Host = host
	s.Port = port

	// Use a specific timestamp in the past
	testTimestamp := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	msg := core.Message{
		ServiceName: "timestamp-test",
		Metric:      42.0,
		Timestamp:   testTimestamp,
	}

	err = s.messageHandler(msg)
	assert.NoError(t, err)

	select {
	case data := <-received:
		// Graphite format: <metric_path> <metric_value> <metric_timestamp>\n
		expectedTimestamp := fmt.Sprintf("%d", testTimestamp.Unix())
		assert.Contains(t, data, "timestamp-test")
		assert.Contains(t, data, "42.00")
		assert.True(t, strings.HasSuffix(strings.TrimSpace(data), expectedTimestamp), "Metric should end with the correct timestamp, got: %s", data)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for graphite metric")
	}
}

func TestService_MessageHandler_ConnectError(t *testing.T) {
	deps := testDeps(t)
	// Use an invalid port to trigger connection error
	cfg := core.ServiceConfig{
		Config: map[string]interface{}{
			"hostname": "127.0.0.1",
			"port":     1, // Highly likely to fail
		},
		Subs: map[string]core.ChannelInfo{
			"graphite": {Name: "graphite-topic"},
		},
	}
	s := NewService(deps, cfg).(*Service)
	s.Host = "127.0.0.1"
	s.Port = 1

	msg := core.Message{
		ServiceName: "test-service",
		Metric:      123.456,
	}

	err := s.messageHandler(msg)
	assert.Error(t, err)
	//goland:noinspection GoMaybeNil
	assert.Contains(t, err.Error(), "connection refused")
}

func TestService_MessageHandler_SendError(t *testing.T) {
	// Start a local TCP server that closes the connection immediately
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	//goland:noinspection GoUnhandledErrorResult
	defer ln.Close()

	addr := ln.Addr().(*net.TCPAddr)
	host := addr.IP.String()
	port := addr.Port

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Close the connection immediately to cause a send error
			//goland:noinspection GoUnhandledErrorResult
			conn.Close()
		}
	}()

	deps := testDeps(t)
	cfg := core.ServiceConfig{
		Config: map[string]interface{}{
			"hostname": host,
			"port":     port,
		},
		Subs: map[string]core.ChannelInfo{
			"graphite": {Name: "graphite-topic"},
		},
	}
	s := NewService(deps, cfg).(*Service)
	s.Host = host
	s.Port = port

	msg := core.Message{
		ServiceName: "test-service",
		Metric:      123.456,
	}

	// Try multiple times as some libraries might retry or the timing might be tricky
	var lastErr error
	for i := 0; i < 5; i++ {
		lastErr = s.messageHandler(msg)
		if lastErr != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	assert.Error(t, lastErr)
}

func TestService_Check(t *testing.T) {
	deps := testDeps(t)
	svc := NewService(deps, core.ServiceConfig{})
	err := svc.Check()
	assert.NoError(t, err)
}
