package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Compile-time check that *FakeLogger implements Logger
var _ Logger = (*FakeLogger)(nil)

func TestFakeLogger_ErrorStoresMsgAndArgs(t *testing.T) {
	fl := &FakeLogger{}

	fl.Error("something happened", "code", 42, "ok", true)

	assert.Equal(t, "something happened", fl.lastErrMsg)
	assert.Equal(t, []any{"code", 42, "ok", true}, fl.lastErrArgs)
}

func TestFakeLogger_OtherLevelsDoNotModifyState(t *testing.T) {
	fl := &FakeLogger{}

	fl.Error("initial", "k", "v")
	assert.Equal(t, "initial", fl.lastErrMsg)
	assert.Equal(t, []any{"k", "v"}, fl.lastErrArgs)

	// Other levels should be no-ops and not panic
	fl.Debug("debug", 1)
	fl.Info("info", 2)
	fl.Warn("warn", 3)

	// check other levels did not modify error state
	assert.Equal(t, "debug", fl.lastDebugMsg)
	assert.Equal(t, []any{1}, fl.lastDebugArgs)

	assert.Equal(t, "info", fl.lastInfoMsg)
	assert.Equal(t, []any{2}, fl.lastInfoArgs)

	assert.Equal(t, "warn", fl.lastWarnMsg)
	assert.Equal(t, []any{3}, fl.lastWarnArgs)

	// error state remains unchanged
	assert.Equal(t, "initial", fl.lastErrMsg)
	assert.Equal(t, []any{"k", "v"}, fl.lastErrArgs)
}
