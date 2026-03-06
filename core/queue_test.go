//nolint:revive,gosec
package core

import (
	"context"
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
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	osProvider := OsProvider{}
	logger := &FakeLogger{}

	pq, err := NewPersistentQueue("test_queue", tmpDir, osProvider, logger)
	require.NoError(t, err)

	err = pq.Enqueue("item1")
	require.NoError(t, err)

	err = pq.Enqueue("item2")
	require.NoError(t, err)

	item, _, _, err := pq.Dequeue(context.Background(), "test")
	require.NoError(t, err)
	assert.Equal(t, "item1", item)
	err = pq.Ack("test")
	require.NoError(t, err)

	item, _, _, err = pq.Dequeue(context.Background(), "test")
	require.NoError(t, err)
	assert.Equal(t, "item2", item)
	err = pq.Ack("test")
	require.NoError(t, err)
}

func TestPersistentQueue_Persistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_test_persist")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	osProvider := OsProvider{}
	logger := &FakeLogger{}

	// First session
	pq, err := NewPersistentQueue("test_queue", tmpDir, osProvider, logger)
	require.NoError(t, err)

	err = pq.Enqueue("item1")
	require.NoError(t, err)

	item, _, _, err := pq.Dequeue(context.Background(), "test")
	require.NoError(t, err)
	assert.Equal(t, "item1", item)
	err = pq.Ack("test")
	require.NoError(t, err)

	err = pq.Enqueue("item2")
	require.NoError(t, err)

	// Second session (restart)
	pq2, err := NewPersistentQueue("test_queue", tmpDir, osProvider, logger)
	require.NoError(t, err)

	item, _, _, err = pq2.Dequeue(context.Background(), "test")
	require.NoError(t, err)
	assert.Equal(t, "item2", item)
	err = pq2.Ack("test")
	require.NoError(t, err)
}

func TestPersistentQueue_Ack(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_test_ack")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	osProvider := OsProvider{}
	logger := &FakeLogger{}

	pq, err := NewPersistentQueue("test_queue", tmpDir, osProvider, logger)
	require.NoError(t, err)

	err = pq.Enqueue("item1")
	require.NoError(t, err)
	err = pq.Enqueue("item2")
	require.NoError(t, err)

	// Dequeue item1
	item, _, _, err := pq.Dequeue(context.Background(), "reader1")
	require.NoError(t, err)
	assert.Equal(t, "item1", item)

	// Dequeue again without Ack, should get item1 again
	item, _, _, err = pq.Dequeue(context.Background(), "reader1")
	require.NoError(t, err)
	assert.Equal(t, "item1", item)

	// Ack item1
	err = pq.Ack("reader1")
	require.NoError(t, err)

	// Dequeue should now get item2
	item, _, _, err = pq.Dequeue(context.Background(), "reader1")
	require.NoError(t, err)
	assert.Equal(t, "item2", item)

	// Dequeue again without Ack, should get item2 again
	item, _, _, err = pq.Dequeue(context.Background(), "reader1")
	require.NoError(t, err)
	assert.Equal(t, "item2", item)

	// Ack item2
	err = pq.Ack("reader1")
	require.NoError(t, err)

	// Next Dequeue should block (we'll check with a timeout)
	resChan := make(chan string, 1)
	go func() {
		item, _, _, err := pq.Dequeue(context.Background(), "reader1")
		if err != nil {
			t.Errorf("Dequeue error: %v", err)
			resChan <- ""
			return
		}
		resChan <- item
	}()

	select {
	case <-resChan:
		t.Fatal("should have blocked")
	case <-time.After(100 * time.Millisecond):
		// OK
	}
}

func TestPersistentQueue_Rotation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_test_rotation")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	osProvider := OsProvider{}
	logger := &FakeLogger{}

	pq, err := NewPersistentQueue("test_queue", tmpDir, osProvider, logger)
	require.NoError(t, err)

	// Manually create an old file
	oldDate := time.Now().AddDate(0, 0, -1).Format("20060102")
	oldFile := filepath.Join(tmpDir, "test_queue_queue_"+oldDate+".log")
	err = os.WriteFile(oldFile, []byte("old_item\n"), 0600)
	require.NoError(t, err)

	// Enqueue something today
	err = pq.Enqueue("new_item")
	require.NoError(t, err)

	// Should read old_item first
	item, _, _, err := pq.Dequeue(context.Background(), "test")
	require.NoError(t, err)
	assert.Equal(t, "old_item", item)
	err = pq.Ack("test")
	require.NoError(t, err)

	// Should then read new_item (rotation)
	item, _, _, err = pq.Dequeue(context.Background(), "test")
	require.NoError(t, err)
	assert.Equal(t, "new_item", item)
	err = pq.Ack("test")
	require.NoError(t, err)
}

func TestPersistentQueue_Blocking(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_test_blocking")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	osProvider := OsProvider{}
	logger := &FakeLogger{}

	pq, err := NewPersistentQueue("test_queue", tmpDir, osProvider, logger)
	require.NoError(t, err)

	resChan := make(chan string, 1)
	go func() {
		item, _, _, err := pq.Dequeue(context.Background(), "test")
		if err != nil {
			t.Errorf("Dequeue error: %v", err)
			resChan <- ""
			return
		}
		if err := pq.Ack("test"); err != nil {
			t.Logf("Ack error: %v", err)
		}
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
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	osProvider := OsProvider{}
	logger := &FakeLogger{}

	pq, err := NewPersistentQueue("test_queue", tmpDir, osProvider, logger)
	require.NoError(t, err)

	resChan := make(chan string, 1)
	go func() {
		item, _, _, err := pq.Dequeue(context.Background(), "test")
		if err != nil {
			t.Errorf("Dequeue error: %v", err)
		}
		if err := pq.Ack("test"); err != nil {
			assert.NoError(t, err)
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
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	osProvider := OsProvider{}
	logger := &FakeLogger{}

	pq, err := NewPersistentQueue("test_queue", tmpDir, osProvider, logger)
	require.NoError(t, err)

	err = pq.Enqueue("item1")
	require.NoError(t, err)
	err = pq.Enqueue("item2")
	require.NoError(t, err)

	// Reader 1 reads item1
	item, _, _, err := pq.Dequeue(context.Background(), "reader1")
	require.NoError(t, err)
	assert.Equal(t, "item1", item)
	err = pq.Ack("reader1")
	require.NoError(t, err)

	// Reader 2 reads item1 (should be independent)
	item, _, _, err = pq.Dequeue(context.Background(), "reader2")
	require.NoError(t, err)
	assert.Equal(t, "item1", item)
	err = pq.Ack("reader2")
	require.NoError(t, err)

	// Reader 1 reads item2
	item, _, _, err = pq.Dequeue(context.Background(), "reader1")
	require.NoError(t, err)
	assert.Equal(t, "item2", item)
	err = pq.Ack("reader1")
	require.NoError(t, err)

	// Reader 2 reads item2
	item, _, _, err = pq.Dequeue(context.Background(), "reader2")
	require.NoError(t, err)
	assert.Equal(t, "item2", item)
	err = pq.Ack("reader2")
	require.NoError(t, err)
}

func TestPersistentQueue_MultipleQueues(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_test_multi_queues")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

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

	item, _, _, err := pq1.Dequeue(context.Background(), "reader")
	require.NoError(t, err)
	assert.Equal(t, "q1_item", item)
	err = pq1.Ack("reader")
	require.NoError(t, err)

	item, _, _, err = pq2.Dequeue(context.Background(), "reader")
	require.NoError(t, err)
	assert.Equal(t, "q2_item", item)
	err = pq2.Ack("reader")
	require.NoError(t, err)

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

func TestPersistentQueue_MissingFileInState(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_test_missing")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	osProvider := OsProvider{}
	logger := &FakeLogger{}

	pq, err := NewPersistentQueue("test_queue", tmpDir, osProvider, logger)
	require.NoError(t, err)

	// 1. Enqueue an item
	err = pq.Enqueue("item1")
	require.NoError(t, err)

	// 2. Dequeue it to establish state, but don't Ack it yet.
	// Actually, Dequeue sets pq.pending.
	item, _, _, err := pq.Dequeue(context.Background(), "reader1")
	require.NoError(t, err)
	assert.Equal(t, "item1", item)

	// 3. Ack it to save state
	err = pq.Ack("reader1")
	require.NoError(t, err)

	// 4. Enqueue another item
	// We want this to go into the same file so we can delete it and test the "file missing" case.
	err = pq.Enqueue("item2")
	require.NoError(t, err)

	// Now state points to after item1 (and include item2 in that file).
	files, err := filepath.Glob(filepath.Join(tmpDir, "test_queue_queue_*.log"))
	require.NoError(t, err)
	require.NotEmpty(t, files)

	// 5. Dequeue reader1 to set pending to item2, then Ack it to save state pointing to end of file.
	item, _, _, err = pq.Dequeue(context.Background(), "reader1")
	require.NoError(t, err)
	assert.Equal(t, "item2", item)
	err = pq.Ack("reader1")
	require.NoError(t, err)

	// Now we simulate the file being deleted.
	err = os.Remove(files[0])
	require.NoError(t, err)

	// 6. Enqueue item3. We'll manually create a file with a NEWER date to ensure it's a different file.
	newDate := time.Now().Add(24 * time.Hour).Format("20060102")
	newFile := filepath.Join(tmpDir, "test_queue_queue_"+newDate+".log")
	err = os.WriteFile(newFile, []byte("item3\n"), 0600)
	require.NoError(t, err)

	// 7. Dequeue should handle the missing file error, log it, and find item3.
	item, _, _, err = pq.Dequeue(context.Background(), "reader1")
	require.NoError(t, err)
	assert.Equal(t, "item3", item)
}

func TestPersistentQueue_WSReaderPersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_test_ws_persistence")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	osProvider := OsProvider{}
	logger := &FakeLogger{}

	// 1. Create queue and add item
	pq, err := NewPersistentQueue("test_queue", tmpDir, osProvider, logger)
	require.NoError(t, err)

	err = pq.Enqueue("item1")
	require.NoError(t, err)

	// 2. Read as non-ws reader and ack
	_, _, _, err = pq.Dequeue(context.Background(), "normal_reader")
	require.NoError(t, err)
	err = pq.Ack("normal_reader")
	require.NoError(t, err)

	// 3. Read as ws reader and ack
	_, _, _, err = pq.Dequeue(context.Background(), "ws_reader")
	require.NoError(t, err)
	err = pq.Ack("ws_reader")
	require.NoError(t, err)

	// 4. Verify normal_reader state file exists
	stateFile := filepath.Join(tmpDir, "reader_state_test_queue_normal_reader.json")
	_, err = os.Stat(stateFile)
	assert.NoError(t, err, "normal reader state should be persisted")

	// 5. Verify ws_reader state file does NOT exist
	wsStateFile := filepath.Join(tmpDir, "reader_state_test_queue_ws_reader.json")
	_, err = os.Stat(wsStateFile)
	assert.True(t, os.IsNotExist(err), "ws reader state should NOT be persisted")

	// 6. Restart queue (new instance)
	pq2, err := NewPersistentQueue("test_queue", tmpDir, osProvider, logger)
	require.NoError(t, err)

	// 7. normal_reader should have NO messages (already acked and persisted)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _, _, err = pq2.Dequeue(ctx, "normal_reader")
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// 8. ws_reader SHOULD have item1 again (acked but NOT persisted)
	item, _, _, err := pq2.Dequeue(context.Background(), "ws_reader")
	require.NoError(t, err)
	assert.Equal(t, "item1", item)
}
