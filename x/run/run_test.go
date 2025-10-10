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

	deps := core.Dependencies{Logger: logger, Hostname: "test-host", Context: ctx}

	done := make(chan error, 1)
	go func() {
		done <- run(deps)
	}()

	// give the goroutine a moment to start and execute the immediate checks
	time.Sleep(1 * time.Second)
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

	// executeChecks should have run immediately: heartbeat.Check logs an INFO with msg "Check"
	// and temp.Check likely logs either INFO or ERROR depending on environment. We only assert heartbeat presence.
	if !strings.Contains(logs, "\"msg\":\"Check\"") {
		t.Fatalf("expected heartbeat Check log in output; got logs: %s", logs)
	}
}
