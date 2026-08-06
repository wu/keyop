package runtime

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizePprofAddr(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "bare port binds loopback", in: "6060", want: "127.0.0.1:6060"},
		{name: "colon port binds loopback", in: ":6060", want: "127.0.0.1:6060"},
		{name: "surrounding space trimmed", in: " :6060 ", want: "127.0.0.1:6060"},
		{name: "explicit host preserved", in: "0.0.0.0:6060", want: "0.0.0.0:6060"},
		{name: "explicit loopback preserved", in: "127.0.0.1:7000", want: "127.0.0.1:7000"},
		{name: "ipv6 host preserved", in: "[::1]:6060", want: "[::1]:6060"},
		{name: "empty stays empty", in: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizePprofAddr(tt.in))
		})
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "loopback v4", in: "127.0.0.1:6060", want: true},
		{name: "loopback v4 non standard", in: "127.0.0.5:6060", want: true},
		{name: "loopback v6", in: "[::1]:6060", want: true},
		{name: "localhost", in: "localhost:6060", want: true},
		{name: "all interfaces", in: "0.0.0.0:6060", want: false},
		{name: "routable address", in: "192.168.1.10:6060", want: false},
		{name: "unresolved hostname", in: "keyop.example.com:6060", want: false},
		{name: "malformed", in: "not-an-address", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isLoopbackAddr(tt.in))
		})
	}
}

func TestWithServiceLabelSetsLabel(t *testing.T) {
	var got string
	var found bool
	var ran bool

	withServiceLabel(context.Background(), "cpuMonitor", func(ctx context.Context) {
		ran = true
		got, found = pprof.Label(ctx, serviceLabel)
	})

	assert.True(t, ran, "labelled function must be invoked")
	assert.True(t, found, "service label must be set on the context")
	assert.Equal(t, "cpuMonitor", got)
}

// Task.Ctx is a plain struct field, so the helper must tolerate a nil context
// rather than panicking inside pprof.Do.
func TestWithServiceLabelNilContext(t *testing.T) {
	var ran bool
	var got string
	var found bool

	withServiceLabel(nil, "ping", func(ctx context.Context) { //nolint:staticcheck // exercising the nil-context guard
		ran = true
		got, found = pprof.Label(ctx, serviceLabel)
	})

	assert.True(t, ran)
	assert.True(t, found)
	assert.Equal(t, "ping", got)
}

func TestStartPprofServerDisabledByDefault(t *testing.T) {
	t.Setenv(pprofAddrEnv, "")
	deps := getDefaultTestDeps()
	defer deps.MustGetCancel()()

	// No listener, no panic, no error: the feature is opt-in.
	startPprofServer(deps)
}

func TestStartPprofServerServesProfilesAndShutsDown(t *testing.T) {
	port := freePort(t)
	t.Setenv(pprofAddrEnv, fmt.Sprintf("%d", port))

	deps := getDefaultTestDeps()
	cancel := deps.MustGetCancel()

	startPprofServer(deps)

	addr := fmt.Sprintf("http://127.0.0.1:%d/debug/pprof/", port)
	body := getWithRetry(t, addr)
	assert.Contains(t, body, "goroutine", "pprof index should list the goroutine profile")

	// Cancelling the dependency context shuts the listener down.
	cancel()

	assert.Eventually(t, func() bool {
		client := &http.Client{Timeout: 500 * time.Millisecond}
		resp, err := client.Get(addr) //nolint:noctx // short-lived probe in test
		if err != nil {
			return true
		}
		_ = resp.Body.Close()
		return false
	}, 5*time.Second, 50*time.Millisecond, "listener should stop serving after context cancel")
}

func TestStartPprofServerSurvivesBindFailure(t *testing.T) {
	// Hold a port so the listener cannot bind; startup must continue regardless,
	// since profiling must never stop the daemon from running.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	t.Setenv(pprofAddrEnv, ln.Addr().String())

	deps := getDefaultTestDeps()
	defer deps.MustGetCancel()()

	startPprofServer(deps)
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())
	return port
}

func getWithRetry(t *testing.T, url string) string {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}

	var body string
	require.Eventually(t, func() bool {
		resp, err := client.Get(url) //nolint:noctx // short-lived probe in test
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return false
		}
		body = string(b)
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 50*time.Millisecond, "pprof listener did not become ready")

	return body
}
