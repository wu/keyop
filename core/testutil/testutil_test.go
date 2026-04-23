package testutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// FakeLogger additional tests
func TestFakeLogger_ConcurrentAccess(t *testing.T) {
	fl := &FakeLogger{}

	// Test that concurrent access is safe
	done := make(chan bool, 2)

	go func() {
		fl.Error("error1", "a", "b")
		done <- true
	}()

	go func() {
		fl.Info("info1", "c", "d")
		done <- true
	}()

	<-done
	<-done

	// Verify no panics occurred and state is consistent
	assert.NotEmpty(t, fl.LastErrMsg())
}

func TestFakeLogger_AllLevels(t *testing.T) {
	fl := &FakeLogger{}

	fl.Debug("debug msg", "k1", "v1")
	fl.Info("info msg", "k2", "v2")
	fl.Warn("warn msg", "k3", "v3")
	fl.Error("error msg", "k4", "v4")

	assert.Equal(t, "debug msg", fl.LastDebugMsg)
	assert.Equal(t, []any{"k1", "v1"}, fl.LastDebugArgs)

	assert.Equal(t, "info msg", fl.LastInfoMsg)
	assert.Equal(t, []any{"k2", "v2"}, fl.LastInfoArgs)

	assert.Equal(t, "warn msg", fl.LastWarnMsg)
	assert.Equal(t, []any{"k3", "v3"}, fl.LastWarnArgs)

	assert.Equal(t, "error msg", fl.LastErrMsg())
	assert.Equal(t, []any{"k4", "v4"}, fl.LastErrArgs())
}

// FakeOsProvider additional tests
func TestFakeOsProvider_ReadFileDefaultBehavior(t *testing.T) {
	f := FakeOsProvider{}
	data, err := f.ReadFile("any")
	assert.Nil(t, data)
	assert.Error(t, err)
}

func TestFakeOsProvider_RemoveAllDefaultBehavior(t *testing.T) {
	f := FakeOsProvider{}
	err := f.RemoveAll("any")
	assert.NoError(t, err)
}

// FakeFile additional tests
func TestFakeFile_CloseDefault(t *testing.T) {
	f := &FakeFile{}
	err := f.Close()
	assert.NoError(t, err)
}

func TestFakeFile_WriteString(t *testing.T) {
	f := &FakeFile{}
	n, err := f.WriteString("hello")
	// No ReadWriteSeeker set, so error expected
	assert.Error(t, err)
	assert.Equal(t, 0, n)
}

// FakeCommand additional tests
func TestFakeCommand_AllMethods(t *testing.T) {
	f := &FakeCommand{}

	err := f.Run()
	assert.NoError(t, err)

	out, err := f.Output()
	assert.NoError(t, err)
	assert.Nil(t, out)

	out, err = f.CombinedOutput()
	assert.NoError(t, err)
	assert.Nil(t, out)
}

func TestFakeCommand_WithCallbacks(t *testing.T) {
	f := &FakeCommand{
		RunFunc: func() error {
			return assert.AnError
		},
		OutputFunc: func() ([]byte, error) {
			return []byte("output"), nil
		},
		CombinedOutputFunc: func() ([]byte, error) {
			return []byte("combined"), nil
		},
	}

	err := f.Run()
	assert.ErrorIs(t, err, assert.AnError)

	out, err := f.Output()
	assert.NoError(t, err)
	assert.Equal(t, "output", string(out))

	out, err = f.CombinedOutput()
	assert.NoError(t, err)
	assert.Equal(t, "combined", string(out))
}

// FakeMessenger additional tests
func TestFakeNewMessenger_DefaultInstanceName(t *testing.T) {
	m := NewFakeMessenger()
	assert.Equal(t, "", m.InstanceName())
}

func TestFakeNewMessenger_InstanceNameCustom(t *testing.T) {
	m := NewFakeMessenger()
	m.InstanceNameValue = "test-instance"
	assert.Equal(t, "test-instance", m.InstanceName())
}

func TestFakeNewMessenger_Close(t *testing.T) {
	m := NewFakeMessenger()
	err := m.Close()
	assert.NoError(t, err)
}

func TestFakeNewMessenger_MultipleMethods(t *testing.T) {
	m := NewFakeMessenger()

	// RegisterPayloadType should succeed
	err := m.RegisterPayloadType("test.v1", nil)
	assert.NoError(t, err)

	// Close should succeed
	err = m.Close()
	assert.NoError(t, err)
}

// NoOpStateStore tests
func TestNoOpStateStore_SaveAndLoad(t *testing.T) {
	store := &NoOpStateStore{}

	// Save should always succeed
	err := store.Save("key", "value")
	assert.NoError(t, err)

	// Load should always succeed but not actually load anything
	var loaded string
	err = store.Load("key", &loaded)
	assert.NoError(t, err)
	assert.Empty(t, loaded)
}

func TestNoOpStateStore_NoSideEffects(t *testing.T) {
	store := &NoOpStateStore{}

	// Multiple saves/loads should work without side effects
	for i := 0; i < 10; i++ {
		_ = store.Save("test", i)
		var val int
		_ = store.Load("test", &val)
		assert.Equal(t, 0, val) // Should always be zero (no-op)
	}
}
