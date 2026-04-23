package core_test

import (
	"bytes"
	"context"
	"github.com/wu/keyop/core"
	"github.com/wu/keyop/core/adapter"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDependencies_MustGetLogger_PanicsWhenUnset(t *testing.T) {
	var d core.Dependencies
	assert.Panics(t, func() { _ = d.MustGetLogger() })
}

func TestDependencies_MustGetLogger_ReturnsWhenSet(t *testing.T) {
	var d core.Dependencies
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	d.SetLogger(logger)

	got := d.MustGetLogger()
	assert.Equal(t, logger, got)

	// use the logger to ensure it behaves and does not panic
	got.Info("test", "key", "value")
}

func TestDependencies_MustGetContext_PanicsWhenUnset(t *testing.T) {
	var d core.Dependencies
	assert.Panics(t, func() { _ = d.MustGetContext() })
}

func TestDependencies_MustGetContext_ReturnsWhenSet(t *testing.T) {
	var d core.Dependencies
	type ctxKey string
	ctx := context.WithValue(context.Background(), ctxKey("k"), "v")
	d.SetContext(ctx)

	got := d.MustGetContext()
	assert.Equal(t, ctx, got)
}

func TestDependencies_MustGetOs_PanicsWhenUnset(t *testing.T) {
	var d core.Dependencies
	assert.Panics(t, func() { _ = d.MustGetOsProvider() })
}

func TestDependencies_MustGetOs_ReturnsWhenSet(t *testing.T) {
	var d core.Dependencies
	osProvider := adapter.OsProvider{}
	d.SetOsProvider(osProvider)

	got := d.MustGetOsProvider()
	assert.Equal(t, osProvider, got)
}

func TestDependencies_MustGetCancel_PanicsWhenUnset(t *testing.T) {
	var d core.Dependencies
	assert.Panics(t, func() { _ = d.MustGetCancel() })
}

func TestDependencies_SetCancel_AndMustGetCancel_ReturnsAndCallable(t *testing.T) {
	var d core.Dependencies
	called := false
	cancel := func() { called = true }

	d.SetCancel(cancel)

	got := d.MustGetCancel()
	assert.NotNil(t, got)

	// Invoke the returned cancel and ensure our side-effect is observed
	got()
	assert.True(t, called, "expected cancel function to be invoked")
}
