package macosNotes

import (
	"fmt"
	"keyop/core"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestService_Check(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping test on non-macOS platform")
	}

	fakeLogger := &core.FakeLogger{}
	fakeMessenger := &core.FakeMessenger{}
	fakeOs := &core.FakeOsProvider{
		CommandFunc: func(name string, arg ...string) core.CommandApi {
			return &core.FakeCommand{
				CombinedOutputFunc: func() ([]byte, error) {
					if name == "osascript" && len(arg) > 1 && arg[0] == "-e" && arg[1] == `tell application "Notes" to get body of note "TestNote"` {
						return []byte("Note content"), nil
					}
					return nil, fmt.Errorf("unexpected command")
				},
			}
		},
	}

	deps := core.Dependencies{}
	deps.SetLogger(fakeLogger)
	deps.SetMessenger(fakeMessenger)
	deps.SetOsProvider(fakeOs)

	cfg := core.ServiceConfig{
		Name: "testNotes",
		Type: "macosNotes",
		Pubs: map[string]core.ChannelInfo{
			"events": {Name: "events_channel"},
		},
		Config: map[string]any{
			"note_name": "TestNote",
		},
	}

	svc := NewService(deps, cfg)
	err := svc.Initialize()
	assert.NoError(t, err)

	err = svc.Check()
	assert.NoError(t, err)
}

func TestService_ValidateConfig(t *testing.T) {
	fakeLogger := &core.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetLogger(fakeLogger)

	tests := []struct {
		name     string
		cfg      core.ServiceConfig
		expected int
	}{
		{
			name: "valid config",
			cfg: core.ServiceConfig{
				Config: map[string]any{
					"note_name": "TestNote",
				},
			},
			expected: 0,
		},
		{
			name: "missing note_name",
			cfg: core.ServiceConfig{
				Config: map[string]any{},
			},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(deps, tt.cfg)
			errs := svc.ValidateConfig()
			assert.Len(t, errs, tt.expected)
		})
	}
}
