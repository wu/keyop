package core

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDependencies_MustGetLogger_PanicsWhenUnset(t *testing.T) {
	var d Dependencies
	assert.Panics(t, func() { _ = d.MustGetLogger() })
}

func TestDependencies_MustGetLogger_ReturnsWhenSet(t *testing.T) {
	var d Dependencies
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	d.SetLogger(logger)

	got := d.MustGetLogger()
	assert.Equal(t, logger, got)

	// use the logger to ensure it behaves and does not panic
	got.Info("test", "key", "value")
}

func TestDependencies_MustGetContext_PanicsWhenUnset(t *testing.T) {
	var d Dependencies
	assert.Panics(t, func() { _ = d.MustGetContext() })
}

func TestDependencies_MustGetContext_ReturnsWhenSet(t *testing.T) {
	var d Dependencies
	ctx := context.WithValue(context.Background(), "k", "v")
	d.SetContext(ctx)

	got := d.MustGetContext()
	assert.Equal(t, ctx, got)
}

func TestDependencies_MustGetOs_PanicsWhenUnset(t *testing.T) {
	var d Dependencies
	assert.Panics(t, func() { _ = d.MustGetOsProvider() })
}

func TestDependencies_MustGetOs_ReturnsWhenSet(t *testing.T) {
	var d Dependencies
	osProvider := OsProvider{}
	d.SetOsProvider(osProvider)

	got := d.MustGetOsProvider()
	assert.Equal(t, osProvider, got)
}

func TestDependencies_MustGetMessenger_PanicsWhenUnset(t *testing.T) {
	var d Dependencies
	assert.Panics(t, func() { _ = d.MustGetMessenger() })
}

func TestDependencies_MustGetMessenger_ReturnsWhenSet(t *testing.T) {
	var d Dependencies

	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)), FakeOsProvider{Host: "test-host"})
	d.SetMessenger(m)

	got := d.MustGetMessenger()
	assert.Equal(t, m, got)
}

func TestDependencies_MustGetCancel_PanicsWhenUnset(t *testing.T) {
	var d Dependencies
	assert.Panics(t, func() { _ = d.MustGetCancel() })
}

func TestDependencies_SetCancel_AndMustGetCancel_ReturnsAndCallable(t *testing.T) {
	var d Dependencies
	called := false
	cancel := func() { called = true }

	d.SetCancel(cancel)

	got := d.MustGetCancel()
	assert.NotNil(t, got)

	// Invoke the returned cancel and ensure our side-effect is observed
	got()
	assert.True(t, called, "expected cancel function to be invoked")
}
