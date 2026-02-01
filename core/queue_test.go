package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPersistentQueue_Basic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_test")
	require.NoError(t, err)
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	osProvider := OsProvider{}
	logger := &FakeLogger{}

	pq, err := NewPersistentQueue("test_queue", tmpDir, osProvider, logger)
	require.NoError(t, err)

	err = pq.Enqueue("item1")
	require.NoError(t, err)

	err = pq.Enqueue("item2")
	require.NoError(t, err)

	item, err := pq.Dequeue("test")
	require.NoError(t, err)
	assert.Equal(t, "item1", item)

	item, err = pq.Dequeue("test")
	require.NoError(t, err)
	assert.Equal(t, "item2", item)
}

func TestPersistentQueue_Persistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_test_persist")
	require.NoError(t, err)
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	osProvider := OsProvider{}
	logger := &FakeLogger{}

	// First session
	pq, err := NewPersistentQueue("test_queue", tmpDir, osProvider, logger)
	require.NoError(t, err)

	err = pq.Enqueue("item1")
	require.NoError(t, err)

	item, err := pq.Dequeue("test")
	require.NoError(t, err)
	assert.Equal(t, "item1", item)

	err = pq.Enqueue("item2")
	require.NoError(t, err)

	// Second session (restart)
	pq2, err := NewPersistentQueue("test_queue", tmpDir, osProvider, logger)
	require.NoError(t, err)

	item, err = pq2.Dequeue("test")
	require.NoError(t, err)
	assert.Equal(t, "item2", item)
}

func TestPersistentQueue_Rotation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_test_rotation")
	require.NoError(t, err)
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	osProvider := OsProvider{}
	logger := &FakeLogger{}

	pq, err := NewPersistentQueue("test_queue", tmpDir, osProvider, logger)
	require.NoError(t, err)

	// Manually create an old file
	oldDate := time.Now().AddDate(0, 0, -1).Format("20060102")
	oldFile := filepath.Join(tmpDir, "test_queue_queue_"+oldDate+".log")
	err = os.WriteFile(oldFile, []byte("old_item\n"), 0644)
	require.NoError(t, err)

	// Enqueue something today
	err = pq.Enqueue("new_item")
	require.NoError(t, err)

	// Should read old_item first
	item, err := pq.Dequeue("test")
	require.NoError(t, err)
	assert.Equal(t, "old_item", item)

	// Should then read new_item (rotation)
	item, err = pq.Dequeue("test")
	require.NoError(t, err)
	assert.Equal(t, "new_item", item)
}

func TestPersistentQueue_Blocking(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_test_blocking")
	require.NoError(t, err)
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	osProvider := OsProvider{}
	logger := &FakeLogger{}

	pq, err := NewPersistentQueue("test_queue", tmpDir, osProvider, logger)
	require.NoError(t, err)

	resChan := make(chan string)
	go func() {
		item, _ := pq.Dequeue("test")
		resChan <- item
	}()

	// Wait a bit to ensure it's blocking
	select {
	case <-resChan:
		t.Fatal("should have blocked")
	case <-time.After(100 * time.Millisecond):
		// OK
	}

	err = pq.Enqueue("blocked_item")
	require.NoError(t, err)

	select {
	case item := <-resChan:
		assert.Equal(t, "blocked_item", item)
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for item")
	}
}

func TestPersistentQueue_DequeueBeforeEnqueue(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_test_before")
	require.NoError(t, err)
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	osProvider := OsProvider{}
	logger := &FakeLogger{}

	pq, err := NewPersistentQueue("test_queue", tmpDir, osProvider, logger)
	require.NoError(t, err)

	resChan := make(chan string)
	go func() {
		item, err := pq.Dequeue("test")
		if err != nil {
			t.Errorf("Dequeue error: %v", err)
		}
		resChan <- item
	}()

	// Dequeue is called, but no files exist. It should be blocking.
	select {
	case <-resChan:
		t.Fatal("should have blocked as no items are available")
	case <-time.After(200 * time.Millisecond):
		// Expected to block
	}

	// Now enqueue something
	err = pq.Enqueue("item_after_dequeue")
	require.NoError(t, err)

	select {
	case item := <-resChan:
		assert.Equal(t, "item_after_dequeue", item)
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for item")
	}
}

func TestPersistentQueue_MultipleReaders(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_test_multi")
	require.NoError(t, err)
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	osProvider := OsProvider{}
	logger := &FakeLogger{}

	pq, err := NewPersistentQueue("test_queue", tmpDir, osProvider, logger)
	require.NoError(t, err)

	err = pq.Enqueue("item1")
	require.NoError(t, err)
	err = pq.Enqueue("item2")
	require.NoError(t, err)

	// Reader 1 reads item1
	item, err := pq.Dequeue("reader1")
	require.NoError(t, err)
	assert.Equal(t, "item1", item)

	// Reader 2 reads item1 (should be independent)
	item, err = pq.Dequeue("reader2")
	require.NoError(t, err)
	assert.Equal(t, "item1", item)

	// Reader 1 reads item2
	item, err = pq.Dequeue("reader1")
	require.NoError(t, err)
	assert.Equal(t, "item2", item)

	// Reader 2 reads item2
	item, err = pq.Dequeue("reader2")
	require.NoError(t, err)
	assert.Equal(t, "item2", item)
}

func TestPersistentQueue_MultipleQueues(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_test_multi_queues")
	require.NoError(t, err)
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	osProvider := OsProvider{}
	logger := &FakeLogger{}

	pq1, err := NewPersistentQueue("q1", tmpDir, osProvider, logger)
	require.NoError(t, err)

	pq2, err := NewPersistentQueue("q2", tmpDir, osProvider, logger)
	require.NoError(t, err)

	err = pq1.Enqueue("q1_item")
	require.NoError(t, err)

	err = pq2.Enqueue("q2_item")
	require.NoError(t, err)

	item, err := pq1.Dequeue("reader")
	require.NoError(t, err)
	assert.Equal(t, "q1_item", item)

	item, err = pq2.Dequeue("reader")
	require.NoError(t, err)
	assert.Equal(t, "q2_item", item)

	// Verify files exist with correct names
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)

	foundQ1 := false
	foundQ2 := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "q1_queue_") {
			foundQ1 = true
		}
		if strings.HasPrefix(entry.Name(), "q2_queue_") {
			foundQ2 = true
		}
	}
	assert.True(t, foundQ1, "q1 file not found")
	assert.True(t, foundQ2, "q2 file not found")
}

func TestPersistentQueue_Errors(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_test_errors")
	require.NoError(t, err)
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	logger := &FakeLogger{}

	// 1. NewPersistentQueue error
	osProv := FakeOsProvider{
		MkdirAllFunc: func(path string, perm os.FileMode) error {
			return os.ErrPermission
		},
	}
	_, err = NewPersistentQueue("test", tmpDir, osProv, logger)
	assert.Error(t, err)

	// 2. loadState error
	osProv = FakeOsProvider{
		OpenFileFunc: func(name string, flag int, perm os.FileMode) (FileApi, error) {
			if strings.Contains(name, "reader_state") {
				return nil, os.ErrPermission
			}
			return OsProvider{}.OpenFile(name, flag, perm)
		},
	}
	pq, err := NewPersistentQueue("test", tmpDir, OsProvider{}, logger)
	require.NoError(t, err)
	pq.osProvider = osProv
	_, err = pq.Dequeue("reader")
	assert.Error(t, err)

	// 3. listQueueFiles error
	osProv = FakeOsProvider{
		ReadDirFunc: func(dirname string) ([]os.DirEntry, error) {
			return nil, os.ErrPermission
		},
	}
	pq, err = NewPersistentQueue("test", tmpDir, OsProvider{}, logger)
	require.NoError(t, err)
	pq.osProvider = osProv
	_, err = pq.Dequeue("reader")
	assert.Error(t, err)

	// 4. saveState error
	pq, err = NewPersistentQueue("test_save", tmpDir, OsProvider{}, logger)
	require.NoError(t, err)
	err = pq.Enqueue("item")
	require.NoError(t, err)

	osProv = FakeOsProvider{
		OpenFileFunc: func(name string, flag int, perm os.FileMode) (FileApi, error) {
			if strings.Contains(name, "reader_state") {
				return nil, os.ErrPermission
			}
			return OsProvider{}.OpenFile(name, flag, perm)
		},
	}
	pq.osProvider = osProv
	_, err = pq.Dequeue("reader")
	assert.Error(t, err)
}
