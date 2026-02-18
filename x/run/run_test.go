package run

import (
	"bytes"
	"context"
	"keyop/core"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_run_logs_error_when_service_type_not_registered(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	osProvider := core.FakeOsProvider{Host: "test-host"}
	deps := core.Dependencies{}
	deps.SetOsProvider(osProvider)
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetMessenger(core.NewMessenger(logger, osProvider))
	deps.SetStateStore(&core.NoOpStateStore{})

	serviceConfigs := []core.ServiceConfig{
		{
			Name: "bad",
			Type: "not-registered",
		},
	}

	done := make(chan error, 1)
	go func() { done <- run(deps, serviceConfigs) }()

	// allow goroutine to attempt to construct the service and log error
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.ErrorContains(t, err, "service type not registered")
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return within timeout")
	}

	logs := buf.String()
	assert.Contains(t, logs, "\"msg\":\"service type not registered\"")
	assert.Contains(t, logs, "\"type\":\"not-registered\"")
}
