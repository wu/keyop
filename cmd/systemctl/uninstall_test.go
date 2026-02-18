package systemctl

import (
	"keyop/core"
	"strings"
	"testing"
)

func TestUninstallSystemd(t *testing.T) {
	fakeOs := &core.FakeOsProvider{}
	logger := &core.FakeLogger{}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(logger)

	var removedFile string
	fakeOs.RemoveFunc = func(name string) error {
		removedFile = name
		return nil
	}

	var commands []string
	fakeOs.CommandFunc = func(name string, arg ...string) core.CommandApi {
		commands = append(commands, name+" "+strings.Join(arg, " "))
		return &core.FakeCommand{}
	}

	err := uninstallSystemd(deps)
	if err != nil {
		t.Fatalf("uninstallSystemd failed: %v", err)
	}

	if removedFile != "/etc/systemd/system/keyop.service" {
		t.Errorf("expected /etc/systemd/system/keyop.service to be removed, got %s", removedFile)
	}

	expectedCommands := []string{
		"systemctl stop keyop.service",
		"systemctl disable keyop.service",
		"systemctl daemon-reload",
	}

	if len(commands) != len(expectedCommands) {
		t.Fatalf("expected %d commands, got %d: %v", len(expectedCommands), len(commands), commands)
	}

	for i, cmd := range expectedCommands {
		if commands[i] != cmd {
			t.Errorf("expected command %d to be %s, got %s", i, cmd, commands[i])
		}
	}
}
