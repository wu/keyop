package notify

import (
	"context"
	"fmt"
	"keyop/core"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDeps(t *testing.T, osProvider core.OsProviderApi) core.Dependencies {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}

	ctx, cancel := context.WithCancel(context.Background())
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	t.Cleanup(cancel)

	tmpDir, err := os.MkdirTemp("", "notify_test")
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove %s: %v", tmpDir, err)
		}
	})

	if osProvider == nil {
		osProvider = core.OsProvider{}
	}
	deps.SetOsProvider(osProvider)
	deps.SetLogger(logger)
	messenger := core.NewMessenger(logger, deps.MustGetOsProvider())
	messenger.SetDataDir(tmpDir)

	deps.SetMessenger(messenger)

	return deps
}

func TestService_ValidateConfig(t *testing.T) {
	deps := testDeps(t, nil)

	tests := []struct {
		name        string
		subs        map[string]core.ChannelInfo
		expectError bool
	}{
		{
			name: "valid config",
			subs: map[string]core.ChannelInfo{
				"alerts": {Name: "notify-channel"},
			},
			expectError: false,
		},
		{
			name:        "missing notifications subscription",
			subs:        map[string]core.ChannelInfo{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := core.ServiceConfig{
				Subs: tt.subs,
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
	deps := testDeps(t, nil)
	cfg := core.ServiceConfig{
		Name: "notify-test",
		Subs: map[string]core.ChannelInfo{
			"alerts": {Name: "notify-channel"},
		},
	}
	svc := NewService(deps, cfg)
	err := svc.Initialize()
	assert.NoError(t, err)
}

func TestService_MessageHandler(t *testing.T) {
	t.Run("empty text", func(t *testing.T) {
		deps := testDeps(t, nil)
		cfg := core.ServiceConfig{
			Name: "notify-test",
			Subs: map[string]core.ChannelInfo{
				"alerts": {Name: "notify-channel"},
			},
		}
		svc := NewService(deps, cfg).(*Service)
		msg := core.Message{Text: ""}
		err := svc.messageHandler(msg)
		assert.NoError(t, err)
	})

	t.Run("send notification", func(t *testing.T) {
		var capturedName string
		var capturedArgs []string

		fakeOs := core.FakeOsProvider{
			CommandFunc: func(name string, arg ...string) core.CommandApi {
				capturedName = name
				capturedArgs = arg
				return &core.FakeCommand{}
			},
		}

		deps := testDeps(t, fakeOs)
		cfg := core.ServiceConfig{
			Name: "notify-test",
			Type: "notify-type",
			Subs: map[string]core.ChannelInfo{
				"alerts": {Name: "notify-channel"},
			},
		}
		svc := NewService(deps, cfg).(*Service)
		err := svc.Initialize()
		require.NoError(t, err)

		msg := core.Message{
			ServiceName: cfg.Name,
			ServiceType: cfg.Type,
			Text:        "hello world",
		}
		err = svc.messageHandler(msg)
		assert.NoError(t, err)

		assert.Equal(t, "osascript", capturedName)
		assert.Len(t, capturedArgs, 2)
		assert.Equal(t, "-e", capturedArgs[0])

		assert.Contains(t, capturedArgs[1], "hello world")
		assert.Contains(t, capturedArgs[1], "notify-test")
	})

	// rate limiting behavior
	t.Run("rate limit", func(t *testing.T) {
		var captured []string

		fakeOs := core.FakeOsProvider{
			CommandFunc: func(_ string, arg ...string) core.CommandApi {
				captured = append(captured, arg[1])
				return &core.FakeCommand{}
			},
		}

		deps := testDeps(t, fakeOs)
		cfg := core.ServiceConfig{
			Name: "notify-test",
			Subs: map[string]core.ChannelInfo{
				"alerts": {Name: "notify-channel"},
			},
		}
		svc := NewService(deps, cfg).(*Service)

		// Initialize to set up limiter
		err := svc.Initialize()
		require.NoError(t, err)

		// send 7 messages quickly
		for i := 0; i < 7; i++ {
			msg := core.Message{Text: fmt.Sprintf("msg %d", i+1)}
			err := svc.messageHandler(msg)
			require.NoError(t, err)
		}

		// Expect 5 notifications + 1 rate-limit summary = 6 calls to 'osascript'
		require.Equal(t, 6, len(captured))

		found := false
		for _, c := range captured {
			if strings.Contains(c, "Too many notifications") {
				found = true
				break
			}
		}
		assert.True(t, found)
	})
}
