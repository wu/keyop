package util

import (
	"context"
	"keyop/core"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_InitializeDependencies_ReturnsInitializedDependencies(t *testing.T) {
	deps := InitializeDependencies(true)

	// Check context is set and not nil
	ctx := deps.MustGetContext()
	assert.NotNil(t, ctx)
	_, ok := ctx.(context.Context)
	assert.True(t, ok)

	// Check logger is set and not nil
	logger := deps.MustGetLogger()
	assert.NotNil(t, logger)
	_, ok = logger.(*slog.Logger)
	assert.True(t, ok)

	// Check osProvider is set and not nil
	osProvider := deps.MustGetOsProvider()
	assert.NotNil(t, osProvider)

	// Check messenger is set and not nil
	messenger := deps.MustGetMessenger()
	assert.NotNil(t, messenger)

	// Check state store is set and has correct data directory
	stateStore := deps.MustGetStateStore()
	assert.NotNil(t, stateStore)
	fileStateStore, ok := stateStore.(*core.FileStateStore)
	assert.True(t, ok)

	assert.Contains(t, fileStateStore.DataDir, ".keyop/data")
}
