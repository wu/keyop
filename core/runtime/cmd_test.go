package runtime

import (
	"context"
	"github.com/wu/keyop/core"
	"github.com/wu/keyop/core/adapter"
	"github.com/wu/keyop/core/testutil"
	"log/slog"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestNewCmd_ReturnsCobraCommand(t *testing.T) {
	deps := core.Dependencies{}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps.SetLogger(logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(adapter.OsProvider{})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	cmd := NewCmd(deps)

	assert.NotNil(t, cmd)
	assert.IsType(t, &cobra.Command{}, cmd)
	assert.Equal(t, "run", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
}

func TestNewCmd_HasRunFunction(t *testing.T) {
	deps := core.Dependencies{}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps.SetLogger(logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(adapter.OsProvider{})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	cmd := NewCmd(deps)

	assert.NotNil(t, cmd.RunE)
}

func TestNewCmd_ExecuteWithMissingConfig(t *testing.T) {
	// Use a path that definitely doesn't exist
	t.Setenv("KEYOP_CONF_DIR", "/nonexistent/keyop/conf/dir")

	deps := core.Dependencies{}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps.SetLogger(logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(adapter.OsProvider{})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	cmd := NewCmd(deps)

	// Execute the command - should fail due to missing config directory
	err := cmd.RunE(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config directory does not exist")
}

func TestNewCmd_ExecuteWithValidConfigNoServices(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	deps := core.Dependencies{}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps.SetLogger(logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(adapter.OsProvider{})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	cmd := NewCmd(deps)

	// Execute the command - should fail due to no services configured
	err := cmd.RunE(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no services configured")
}

func TestNewCmd_ExecuteWithValidServiceConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	// Create a minimal service config
	svcCfg := `
name: test-service
services: heartbeat
`
	svcPath := dir + "/test-service.yaml"
	err := os.WriteFile(svcPath, []byte(svcCfg), 0o600)
	assert.NoError(t, err)

	deps := core.Dependencies{}
	logger := &testutil.FakeLogger{}
	deps.SetLogger(logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(adapter.OsProvider{})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	// Register the heartbeat service
	core.RegisterService("heartbeat", func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return &mockServiceForRun{}
	})

	cmd := NewCmd(deps)

	// Execute the command - should proceed through config and service registration
	// It might fail at the run stage since we're not setting up a full kernel,
	// but we should get past the config loading
	err = cmd.RunE(cmd, []string{})
	// The error might be from the run function or from not having a messenger,
	// but we should not get a config loading error
	if err != nil {
		assert.NotContains(t, err.Error(), "no services configured")
		assert.NotContains(t, err.Error(), "config directory does not exist")
	}
}
