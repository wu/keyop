package process

import (
	"fmt"
	"keyop/core"
	"keyop/core/testutil"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProcessService(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.OsProvider{}) // Use real OS for process execution
	messenger := testutil.NewFakeMessenger()
	deps.SetMessenger(messenger)

	pidFile := "test.pid"
	//goland:noinspection GoUnhandledErrorResult
	t.Cleanup(func() {
		if err := os.Remove(pidFile); err != nil {
			t.Logf("failed to remove %s: %v", pidFile, err)
		}
	})

	cfg := core.ServiceConfig{
		Name: "test-process",
		Type: "process",
		Pubs: map[string]core.ChannelInfo{
			"events":  {Name: "events"},
			"metrics": {Name: "metrics"},
			"errors":  {Name: "errors"},
		},
		Config: map[string]interface{}{
			"command": "sleep 10",
			"pidFile": pidFile,
		},
	}

	svc := NewService(deps, cfg).(*Service)

	t.Run("Start process", func(t *testing.T) {
		err := svc.Check()
		assert.NoError(t, err)
		assert.NotNil(t, svc.cmd)
		assert.NotNil(t, svc.cmd.Process)

		// Check PID file
		pidData, err := os.ReadFile(pidFile)
		assert.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("%d", svc.cmd.Process.Pid), string(pidData))

		assert.Len(t, messenger.SentMessages, 1)
		assert.Contains(t, messenger.SentMessages[0].Text, "status started")
	})

	t.Run("Process still running", func(t *testing.T) {
		messenger.SentMessages = nil
		err := svc.Check()
		assert.NoError(t, err)
		assert.Len(t, messenger.SentMessages, 1)
		assert.Contains(t, messenger.SentMessages[0].Text, "status running")
	})

	t.Run("Restart process if died", func(t *testing.T) {
		oldPid := svc.cmd.Process.Pid
		// Kill the process
		err := svc.cmd.Process.Kill()
		assert.NoError(t, err)
		//goland:noinspection GoUnhandledErrorResult
		if _, err := svc.cmd.Process.Wait(); err != nil {
			t.Logf("svc.cmd.Process.Wait failed: %v", err)
		}

		messenger.SentMessages = nil
		err = svc.Check()
		assert.NoError(t, err)
		assert.NotEqual(t, oldPid, svc.cmd.Process.Pid)
		assert.Len(t, messenger.SentMessages, 1)
		assert.Contains(t, messenger.SentMessages[0].Text, "status restarted")

		// Check PID file updated
		pidData, err := os.ReadFile(pidFile)
		assert.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("%d", svc.cmd.Process.Pid), string(pidData))
	})

	// Cleanup
	if svc.cmd != nil && svc.cmd.Process != nil {
		//goland:noinspection GoUnhandledErrorResult
		if err := svc.cmd.Process.Kill(); err != nil {
			t.Logf("svc.cmd.Process.Kill failed: %v", err)
		}
	}
}

func TestValidateConfig(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)

	t.Run("valid config", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Pubs: map[string]core.ChannelInfo{
				"events":  {Name: "e"},
				"metrics": {Name: "m"},
				"errors":  {Name: "er"},
			},
			Config: map[string]interface{}{
				"command": "ls",
				"pidFile": "ls.pid",
			},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.Empty(t, errs)
	})

	t.Run("missing command", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Pubs: map[string]core.ChannelInfo{
				"events":  {Name: "e"},
				"metrics": {Name: "m"},
				"errors":  {Name: "er"},
			},
			Config: map[string]interface{}{
				"pidFile": "ls.pid",
			},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "command")
	})

	t.Run("missing pidFile", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Pubs: map[string]core.ChannelInfo{
				"events":  {Name: "e"},
				"metrics": {Name: "m"},
				"errors":  {Name: "er"},
			},
			Config: map[string]interface{}{
				"command": "ls",
			},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "pidFile")
	})
}
