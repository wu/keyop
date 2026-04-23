package testutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Compile-time check that *FakeLogger implements core.Logger
// (verified via the var _ in logger.go via the interface reference)

func TestFakeLogger_ErrorStoresMsgAndArgs(t *testing.T) {
	fl := &FakeLogger{}

	fl.Error("something happened", "code", 42, "ok", true)

	assert.Equal(t, "something happened", fl.LastErrMsg())
	assert.Equal(t, []any{"code", 42, "ok", true}, fl.LastErrArgs())
}

func TestFakeLogger_OtherLevelsDoNotModifyState(t *testing.T) {
	fl := &FakeLogger{}

	fl.Error("initial", "k", "v")
	assert.Equal(t, "initial", fl.LastErrMsg())
	assert.Equal(t, []any{"k", "v"}, fl.LastErrArgs())

	// Other levels should be no-ops and not panic
	fl.Debug("debug", 1)
	fl.Info("info", 2)
	fl.Warn("warn", 3)

	// check other levels did not modify error state
	assert.Equal(t, "debug", fl.LastDebugMsg)
	assert.Equal(t, []any{1}, fl.LastDebugArgs)

	assert.Equal(t, "info", fl.LastInfoMsg)
	assert.Equal(t, []any{2}, fl.LastInfoArgs)

	assert.Equal(t, "warn", fl.LastWarnMsg)
	assert.Equal(t, []any{3}, fl.LastWarnArgs)

	// error state remains unchanged
	assert.Equal(t, "initial", fl.LastErrMsg())
	assert.Equal(t, []any{"k", "v"}, fl.LastErrArgs())
}
