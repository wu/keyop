package process

import (
	"context"
	"fmt"
	"keyop/core"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type mockMessenger struct {
	messages []core.Message
	mu       sync.Mutex
}

func (m *mockMessenger) Send(msg core.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockMessenger) Subscribe(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
	return nil
}

func (m *mockMessenger) SubscribeExtended(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message, string, int64) error) error {
	return nil
}

func (m *mockMessenger) SetReaderState(channelName string, readerName string, fileName string, offset int64) error {
	return nil
}

func (m *mockMessenger) SeekToEnd(channelName string, readerName string) error {
	return nil
}

func (m *mockMessenger) SetDataDir(dir string) {}

func (m *mockMessenger) GetStats() core.MessengerStats {
	return core.MessengerStats{}
}

func TestProcessService(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.OsProvider{}) // Use real OS for process execution
	messenger := &mockMessenger{}
	deps.SetMessenger(messenger)

	pidFile := "test.pid"
	//goland:noinspection GoUnhandledErrorResult
	defer os.Remove(pidFile)

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

		assert.Len(t, messenger.messages, 1)
		assert.Contains(t, messenger.messages[0].Text, "status started")
	})

	t.Run("Process still running", func(t *testing.T) {
		messenger.messages = nil
		err := svc.Check()
		assert.NoError(t, err)
		assert.Len(t, messenger.messages, 1)
		assert.Contains(t, messenger.messages[0].Text, "status running")
	})

	t.Run("Restart process if died", func(t *testing.T) {
		oldPid := svc.cmd.Process.Pid
		// Kill the process
		err := svc.cmd.Process.Kill()
		assert.NoError(t, err)
		//goland:noinspection GoUnhandledErrorResult
		svc.cmd.Process.Wait()

		messenger.messages = nil
		err = svc.Check()
		assert.NoError(t, err)
		assert.NotEqual(t, oldPid, svc.cmd.Process.Pid)
		assert.Len(t, messenger.messages, 1)
		assert.Contains(t, messenger.messages[0].Text, "status restarted")

		// Check PID file updated
		pidData, err := os.ReadFile(pidFile)
		assert.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("%d", svc.cmd.Process.Pid), string(pidData))
	})

	// Cleanup
	if svc.cmd != nil && svc.cmd.Process != nil {
		//goland:noinspection GoUnhandledErrorResult
		svc.cmd.Process.Kill()
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
