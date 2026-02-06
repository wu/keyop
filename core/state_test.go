package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileStateStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	osProvider := OsProvider{}
	store := NewFileStateStore(tmpDir, osProvider)

	t.Run("Save and Load Time", func(t *testing.T) {
		key := "test_time"
		val := time.Now().Round(time.Second).UTC()

		err := store.Save(key, val)
		assert.NoError(t, err)

		var loaded time.Time
		err = store.Load(key, &loaded)
		assert.NoError(t, err)
		assert.True(t, val.Equal(loaded))
	})

	t.Run("Save and Load Struct", func(t *testing.T) {
		type TestData struct {
			Name  string
			Value int
		}
		key := "test_struct"
		val := TestData{Name: "hello", Value: 42}

		err := store.Save(key, val)
		assert.NoError(t, err)

		var loaded TestData
		err = store.Load(key, &loaded)
		assert.NoError(t, err)
		assert.Equal(t, val, loaded)
	})

	t.Run("Load non-existent", func(t *testing.T) {
		key := "does_not_exist"
		var loaded string
		err := store.Load(key, &loaded)
		assert.NoError(t, err)
		assert.Empty(t, loaded)
	})

	t.Run("MkdirAll called", func(t *testing.T) {
		nestedDir := filepath.Join(tmpDir, "nested")
		store2 := NewFileStateStore(nestedDir, osProvider)

		err := store2.Save("key", "val")
		assert.NoError(t, err)

		_, err = os.Stat(nestedDir)
		assert.NoError(t, err)
	})
}
