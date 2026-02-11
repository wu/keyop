package systemd

import (
	"keyop/core"
	"strings"
	"testing"
)

func TestServiceManagement(t *testing.T) {
	tests := []struct {
		name        string
		action      func(core.Dependencies) error
		expectedCmd string
		expectedLog string
	}{
		{
			name: "Start service",
			action: func(deps core.Dependencies) error {
				return runSystemctl(deps, "start")
			},
			expectedCmd: "systemctl start keyop.service",
			expectedLog: "Start keyop service",
		},
		{
			name: "Stop service",
			action: func(deps core.Dependencies) error {
				return runSystemctl(deps, "stop")
			},
			expectedCmd: "systemctl stop keyop.service",
			expectedLog: "Stop keyop service",
		},
		{
			name: "Restart service",
			action: func(deps core.Dependencies) error {
				return runSystemctl(deps, "restart")
			},
			expectedCmd: "systemctl restart keyop.service",
			expectedLog: "Restart keyop service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeOs := &core.FakeOsProvider{}
			logger := &core.FakeLogger{}
			deps := core.Dependencies{}
			deps.SetOsProvider(fakeOs)
			deps.SetLogger(logger)

			var executedCmd string
			fakeOs.CommandFunc = func(name string, arg ...string) core.CommandApi {
				executedCmd = name + " " + strings.Join(arg, " ")
				return &core.FakeCommand{}
			}

			err := tt.action(deps)
			if err != nil {
				t.Fatalf("action failed: %v", err)
			}

			if executedCmd != tt.expectedCmd {
				t.Errorf("expected command %s, got %s", tt.expectedCmd, executedCmd)
			}
		})
	}
}
