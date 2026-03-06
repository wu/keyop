package process

import (
	"fmt"
	"keyop/core"
	"keyop/core/testutil"
	"log/slog"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestProcessService(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.OsProvider{}) // Use real OS for process execution
	messenger := testutil.NewFakeMessenger()
	deps.SetMessenger(messenger)
	// use a real file state store in temp dir for pid persistence
	tmp := t.TempDir()
	deps.SetStateStore(core.NewFileStateStore(tmp, deps.MustGetOsProvider()))

	// no pid file; rely on state store
	// cleanup handled by t.TempDir()

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
		},
	}

	svc := NewService(deps, cfg).(*Service)

	t.Run("Start process", func(t *testing.T) {
		err := svc.Check()
		assert.NoError(t, err)
		// read pid from state store
		var pid int
		err = deps.MustGetStateStore().Load(fmt.Sprintf("process_%s_pid", cfg.Name), &pid)
		assert.NoError(t, err)
		assert.NotZero(t, pid)

		// expect a process_start and a process_status(ok)
		if !assert.GreaterOrEqual(t, len(messenger.SentMessages), 2) {
			t.Fatalf("unexpected messages: %#v", messenger.SentMessages)
		}
		// find process_start and process_status messages
		var sawStart, sawOk bool
		for _, m := range messenger.SentMessages {
			if m.Event == "process_start" && m.Text != "" && strings.Contains(m.Text, "started") {
				sawStart = true
			}
			if m.Event == "process_status" && m.Status == "ok" {
				sawOk = true
			}
		}
		assert.True(t, sawStart, "did not see process_start message")
		assert.True(t, sawOk, "did not see process_status ok message")
	})

	t.Run("Process still running", func(t *testing.T) {
		messenger.SentMessages = nil
		err := svc.Check()
		assert.NoError(t, err)
		// expect a single process_status with ok
		if !assert.Len(t, messenger.SentMessages, 1) {
			t.Fatalf("unexpected messages: %#v", messenger.SentMessages)
		}
		assert.Equal(t, "process_status", messenger.SentMessages[0].Event)
		assert.Equal(t, "ok", messenger.SentMessages[0].Status)
	})

	t.Run("Restart process if died", func(t *testing.T) {
		// read current pid from the state store
		var oldPid int
		if err := deps.MustGetStateStore().Load(fmt.Sprintf("process_%s_pid", cfg.Name), &oldPid); err != nil {
			t.Fatalf("failed to load pid: %v", err)
		}
		assert.NotZero(t, oldPid)

		// Kill the process by PID to simulate an external kill signal
		if err := syscall.Kill(oldPid, syscall.SIGKILL); err != nil {
			t.Fatalf("failed to kill pid %d: %v", oldPid, err)
		}

		// give the system a moment
		time.Sleep(500 * time.Millisecond)

		messenger.SentMessages = nil
		if err := svc.Check(); err != nil {
			t.Fatalf("check failed: %v", err)
		}

		var newPid int
		if err := deps.MustGetStateStore().Load(fmt.Sprintf("process_%s_pid", cfg.Name), &newPid); err != nil {
			t.Fatalf("failed to load new pid: %v", err)
		}
		assert.NotZero(t, newPid)
		assert.NotEqual(t, oldPid, newPid)
		// expect at least a process_start and a final process_status ok
		if !assert.GreaterOrEqual(t, len(messenger.SentMessages), 2) {
			t.Fatalf("unexpected messages: %#v", messenger.SentMessages)
		}
		var sawStart, sawOk bool
		for _, m := range messenger.SentMessages {
			if m.Event == "process_start" {
				sawStart = true
			}
			if m.Event == "process_status" && m.Status == "ok" {
				sawOk = true
			}
		}
		assert.True(t, sawStart, "did not see process_start message")
		assert.True(t, sawOk, "did not see final process_status ok message")

	})

	// Cleanup: kill any process we started
	var finalPid int
	_ = deps.MustGetStateStore().Load(fmt.Sprintf("process_%s_pid", cfg.Name), &finalPid)
	if finalPid != 0 {
		_ = syscall.Kill(finalPid, syscall.SIGKILL)
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
}
