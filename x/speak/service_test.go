package speak

import (
	"keyop/core"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDeps(t *testing.T) core.Dependencies {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}

	tmpDir, err := os.MkdirTemp("", "speak_test")
	require.NoError(t, err)
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
		subs        map[string]core.ChannelInfo
		expectError bool
	}{
		{
			name: "valid config",
			subs: map[string]core.ChannelInfo{
				"speech": {Name: "speech-channel"},
			},
			expectError: false,
		},
		{
			name:        "missing speech subscription",
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
	deps := testDeps(t)
	cfg := core.ServiceConfig{
		Name: "speak-test",
		Subs: map[string]core.ChannelInfo{
			"speech": {Name: "speech-channel"},
		},
	}
	svc := NewService(deps, cfg)
	err := svc.Initialize()
	assert.NoError(t, err)
}

func TestService_MessageHandler(t *testing.T) {
	// We can't easily test the actual 'say' command execution without mocking exec.Command,
	// but we can at least ensure it handles messages without crashing.
	// In a real environment, this might fail if 'say' is not available.

	deps := testDeps(t)
	cfg := core.ServiceConfig{
		Name: "speak-test",
		Subs: map[string]core.ChannelInfo{
			"speech": {Name: "speech-channel"},
		},
	}
	svc := NewService(deps, cfg).(*Service)

	t.Run("empty text", func(t *testing.T) {
		msg := core.Message{Text: ""}
		err := svc.messageHandler(msg)
		assert.NoError(t, err)
	})

	// Note: We skip testing non-empty text because it would try to run 'say' which might not exist on all systems (though it should on macOS).
	// If this was a more critical service, we would mock the exec call.
}
