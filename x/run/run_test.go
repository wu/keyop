package run

import (
	"bytes"
	"context"
	"keyop/core"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_run_cancels_and_executes_checks_once_immediately(t *testing.T) {
	// capture logs
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	// cancel shortly after starting
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	osProvider := core.FakeOsProvider{Host: "test-host"}

	deps := core.Dependencies{}
	deps.SetOsProvider(osProvider)
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetMessenger(core.NewMessenger(logger, osProvider))

	serviceConfigs := []core.ServiceConfig{
		{
			Name: "heartbeat",
			Freq: 1 * time.Second,
			Type: "heartbeat",
			Pubs: map[string]core.ChannelInfo{
				"events": {Name: "heartbeat", Description: "Heartbeat events"},
			},
		},
	}

	done := make(chan error, 1)
	go func() {
		done <- run(deps, serviceConfigs)
	}()

	// give the goroutine a moment to start and immediately execute serviceConfigs
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		// expect run to return context.Canceled on explicit cancel
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return within timeout")
	}

	logs := buf.String()

	// it should log that run was called
	assert.Contains(t, logs, "\"msg\":\"run called\"")

	// heartbeat logs an INFO with msg "heartbeat"
	if !strings.Contains(logs, "\"msg\":\"heartbeat\"") {
		t.Fatalf("expected heartbeat log in output; got logs: %s", logs)
	}
}
