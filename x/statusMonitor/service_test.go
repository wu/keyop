package statusMonitor

import (
	"keyop/core"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDeps(t *testing.T) core.Dependencies {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}

	tmpDir, err := os.MkdirTemp("", "statusMonitor_test")
	require.NoError(t, err)
	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	osProvider := core.OsProvider{}
	deps.SetOsProvider(osProvider)
	deps.SetLogger(logger)

	messenger := core.NewMessenger(logger, osProvider)
	messenger.SetDataDir(tmpDir)
	deps.SetMessenger(messenger)

	stateStore := core.NewFileStateStore(tmpDir, osProvider)
	deps.SetStateStore(stateStore)

	return deps
}

func TestService_ValidateConfig(t *testing.T) {
	deps := testDeps(t)

	tests := []struct {
		name        string
		subs        map[string]core.ChannelInfo
		pubs        map[string]core.ChannelInfo
		expectError bool
	}{
		{
			name:        "valid config",
			subs:        map[string]core.ChannelInfo{"status": {Name: "status"}},
			pubs:        map[string]core.ChannelInfo{"alerts": {Name: "alerts"}},
			expectError: false,
		},
		{
			name:        "missing status sub",
			subs:        map[string]core.ChannelInfo{},
			pubs:        map[string]core.ChannelInfo{"alerts": {Name: "alerts"}},
			expectError: true,
		},
		{
			name:        "missing alerts pub",
			subs:        map[string]core.ChannelInfo{"status": {Name: "status"}},
			pubs:        map[string]core.ChannelInfo{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := core.ServiceConfig{
				Subs: tt.subs,
				Pubs: tt.pubs,
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

func TestService_Workflow(t *testing.T) {
	deps := testDeps(t)
	cfg := core.ServiceConfig{
		Name: "monitor",
		Type: "statusMonitor",
		Subs: map[string]core.ChannelInfo{
			"status": {Name: "status-chan", MaxAge: 0},
		},
		Pubs: map[string]core.ChannelInfo{
			"alerts": {Name: "alerts-chan"},
		},
	}

	svc := NewService(deps, cfg)
	err := svc.Initialize()
	require.NoError(t, err)

	messenger := deps.MustGetMessenger()

	alertMsgs := make(chan core.Message, 10)
	err = messenger.Subscribe("test-listener", "alerts-chan", 0, func(msg core.Message) error {
		alertMsgs <- msg
		return nil
	})
	require.NoError(t, err)

	// 1. Send OK status - should NOT alert
	err = messenger.Send(core.Message{
		ChannelName: "status-chan",
		ServiceName: "svc1",
		ServiceType: "type1",
		Status:      "ok",
		Text:        "All good",
	})
	require.NoError(t, err)

	// Wait a bit for processing
	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, alertMsgs)

	// 2. Send WARNING status - should alert
	err = messenger.Send(core.Message{
		ChannelName: "status-chan",
		ServiceName: "svc1",
		ServiceType: "type1",
		Status:      "warning",
		Text:        "Getting hot",
	})
	require.NoError(t, err)

	select {
	case msg := <-alertMsgs:
		assert.Equal(t, "warning", msg.Status)
		assert.Contains(t, msg.Text, "ALERT")
		assert.Contains(t, msg.Text, "svc1")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for alert")
	}

	// 3. Send CRITICAL status - should alert (transition from warning)
	err = messenger.Send(core.Message{
		ChannelName: "status-chan",
		ServiceName: "svc1",
		ServiceType: "type1",
		Status:      "critical",
		Text:        "On fire!",
	})
	require.NoError(t, err)

	select {
	case msg := <-alertMsgs:
		assert.Equal(t, "critical", msg.Status)
		assert.Contains(t, msg.Text, "changed from warning to critical")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for critical alert")
	}

	// 4. Send WARNING status - should alert (transition from critical)
	err = messenger.Send(core.Message{
		ChannelName: "status-chan",
		ServiceName: "svc1",
		ServiceType: "type1",
		Status:      "warning",
		Text:        "Back to warning",
	})
	require.NoError(t, err)

	select {
	case msg := <-alertMsgs:
		assert.Equal(t, "warning", msg.Status)
		assert.Contains(t, msg.Text, "changed from critical to warning")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for warning alert")
	}

	// 5. Send OK status - should recovery alert
	err = messenger.Send(core.Message{
		ChannelName: "status-chan",
		ServiceName: "svc1",
		ServiceType: "type1",
		Status:      "ok",
		Text:        "Phew",
	})
	require.NoError(t, err)

	select {
	case msg := <-alertMsgs:
		assert.Equal(t, "ok", msg.Status)
		assert.Contains(t, msg.Text, "RECOVERY")
		assert.Contains(t, msg.Text, "svc1")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for recovery")
	}
}

func TestService_Persistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "statusMonitor_persist_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	osProvider := core.OsProvider{}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg := core.ServiceConfig{
		Name: "monitor",
		Type: "statusMonitor",
		Subs: map[string]core.ChannelInfo{
			"status": {Name: "status-chan"},
		},
		Pubs: map[string]core.ChannelInfo{
			"alerts": {Name: "alerts-chan"},
		},
	}

	// 1. First run: set some state and save it
	{
		deps := core.Dependencies{}
		deps.SetOsProvider(osProvider)
		deps.SetLogger(logger)
		messenger := core.NewMessenger(logger, osProvider)
		messenger.SetDataDir(tmpDir)
		deps.SetMessenger(messenger)
		stateStore := core.NewFileStateStore(tmpDir, osProvider)
		deps.SetStateStore(stateStore)

		svc := NewService(deps, cfg)
		err := svc.Initialize()
		require.NoError(t, err)

		// Directly call messageHandler to simulate receiving a message
		err = svc.(*Service).messageHandler(core.Message{
			ServiceType: "type1",
			ServiceName: "svc1",
			Status:      "warning",
			Text:        "Issue detected",
		})
		require.NoError(t, err)
	}

	// 2. Second run: initialize and check if state was loaded
	{
		deps := core.Dependencies{}
		deps.SetOsProvider(osProvider)
		deps.SetLogger(logger)
		messenger := core.NewMessenger(logger, osProvider)
		messenger.SetDataDir(tmpDir)
		deps.SetMessenger(messenger)
		stateStore := core.NewFileStateStore(tmpDir, osProvider)
		deps.SetStateStore(stateStore)

		svc := NewService(deps, cfg)
		err := svc.Initialize()
		require.NoError(t, err)

		svcImpl := svc.(*Service)
		assert.Equal(t, "warning", svcImpl.states["type1:svc1"])
	}
}
