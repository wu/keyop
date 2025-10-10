package run

import (
	"bytes"
	"context"
	"keyop/core"
	"keyop/x/heartbeat"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_run_cancels_and_executes_checks_once_immediately(t *testing.T) {
	// prepare a temp working directory with a minimal config.yaml (heartbeat only)
	dir := t.TempDir()
	cfg := "- name: heartbeat\n  freq: 1s\n  x: heartbeat\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	oldWD, _ := os.Getwd()
	//goland:noinspection GoUnhandledErrorResult
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// capture logs
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	// cancel shortly after starting
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := core.Dependencies{Logger: logger, Hostname: "test-host", Context: ctx}

	serviceConfigs := []ServiceConfig{
		{
			Name: "heartbeat",
			Freq: 1 * time.Second,
			Type: "heartbeat",
			NewFunc: func(deps core.Dependencies) core.Service {
				deps.Logger.Info("heartbeat")
				return heartbeat.Service{Deps: deps}
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
