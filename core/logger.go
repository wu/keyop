package core

import (
	"sync"
)

type Logger interface {
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

type FakeLogger struct {
	mu            sync.RWMutex
	lastDebugMsg  string
	lastDebugArgs []any
	lastInfoMsg   string
	lastInfoArgs  []any
	lastWarnMsg   string
	lastWarnArgs  []any
	lastErrMsg    string
	lastErrArgs   []any
}

func (d *FakeLogger) Debug(msg string, args ...any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastDebugMsg = msg
	d.lastDebugArgs = args
}
func (d *FakeLogger) Info(msg string, args ...any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastInfoMsg = msg
	d.lastInfoArgs = args
}
func (d *FakeLogger) Warn(msg string, args ...any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastWarnMsg = msg
	d.lastWarnArgs = args
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
