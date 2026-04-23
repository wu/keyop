package testutil

import (
	"sync"
)

// FakeLogger is a test implementation of core.Logger that captures log calls for assertions.
type FakeLogger struct {
	mu            sync.RWMutex
	LastDebugMsg  string
	LastDebugArgs []any
	LastInfoMsg   string
	LastInfoArgs  []any
	LastWarnMsg   string
	LastWarnArgs  []any
	lastErrMsg    string
	lastErrArgs   []any
}

func (d *FakeLogger) Debug(msg string, args ...any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.LastDebugMsg = msg
	d.LastDebugArgs = args
}
func (d *FakeLogger) Info(msg string, args ...any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.LastInfoMsg = msg
	d.LastInfoArgs = args
}
func (d *FakeLogger) Warn(msg string, args ...any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.LastWarnMsg = msg
	d.LastWarnArgs = args
}
func (d *FakeLogger) Error(msg string, args ...any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastErrMsg = msg
	d.lastErrArgs = args
}

func (d *FakeLogger) LastErrMsg() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastErrMsg
}

func (d *FakeLogger) LastErrArgs() []any {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastErrArgs
}
