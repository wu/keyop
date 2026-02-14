package macosMessages

import (
	"context"
	"keyop/core"
	"log/slog"
	"os"
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
		os.RemoveAll(tmpDir)
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
		config      map[string]interface{}
		expectError bool
	}{
		{
			name: "valid config",
			subs: map[string]core.ChannelInfo{
				"alerts": {Name: "notifyMacos-channel"},
			},
			config: map[string]interface{}{
				"address": "test-buddy",
			},
			expectError: false,
		},
		{
			name: "missing address",
			subs: map[string]core.ChannelInfo{
				"alerts": {Name: "notifyMacos-channel"},
			},
			config:      map[string]interface{}{},
			expectError: true,
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
				Subs:   tt.subs,
				Config: tt.config,
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
		Name: "notifyMacos-test",
		Subs: map[string]core.ChannelInfo{
			"alerts": {Name: "notifyMacos-channel"},
		},
		Config: map[string]interface{}{
			"address": "test-buddy",
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
			Name: "notifyMacos-test",
			Subs: map[string]core.ChannelInfo{
				"alerts": {Name: "notifyMacos-channel"},
			},
			Config: map[string]interface{}{
				"address": "test-buddy",
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
			Name: "notifyMacos-test",
			Type: "notifyMacos-type",
			Subs: map[string]core.ChannelInfo{
				"alerts": {Name: "notifyMacos-channel"},
			},
			Config: map[string]interface{}{
				"address": "target-buddy",
			},
		}
		svc := NewService(deps, cfg).(*Service)
		err := svc.Initialize()
		assert.NoError(t, err)

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
		assert.Contains(t, capturedArgs[1], "notifyMacos-type")
		assert.Contains(t, capturedArgs[1], "notifyMacos-test")
		assert.Contains(t, capturedArgs[1], `buddy "target-buddy"`)
	})
}
