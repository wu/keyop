//nolint:revive,gosec
package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetState_Basic verifies that SetState repositions the reader and that
// the next Dequeue starts from the specified offset.
func TestSetState_Basic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_extra_setstate")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	pq, err := NewPersistentQueue("testq_ss", tmpDir, OsProvider{}, &FakeLogger{})
	require.NoError(t, err)

	require.NoError(t, pq.Enqueue("alpha"))
	require.NoError(t, pq.Enqueue("beta"))
	require.NoError(t, pq.Enqueue("gamma"))

	// Read all three so we know the filename and advance past them.
	_, fn, off0, err := pq.Dequeue(context.Background(), "test_reader1")
	require.NoError(t, err) // "alpha"; off0 = offset of "beta"
	require.NoError(t, pq.Ack("test_reader1"))

	_, _, _, err = pq.Dequeue(context.Background(), "test_reader1")
	require.NoError(t, err) // "beta"
	require.NoError(t, pq.Ack("test_reader1"))

	_, _, _, err = pq.Dequeue(context.Background(), "test_reader1")
	require.NoError(t, err) // "gamma"
	require.NoError(t, pq.Ack("test_reader1"))

	// Rewind to the start of "beta" (off0 points there because it is the
	// nextOffset returned after reading "alpha").
	err = pq.SetState("test_reader1", fn, off0)
	require.NoError(t, err)

	item, _, _, err := pq.Dequeue(context.Background(), "test_reader1")
	require.NoError(t, err)
	assert.Equal(t, "beta", item)
}

// TestSetState_PersistsAcrossRestart verifies that state saved by SetState
// survives a queue restart (new instance on the same directory).
func TestSetState_PersistsAcrossRestart(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_extra_setstate_persist")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	pq, err := NewPersistentQueue("testq_ssp", tmpDir, OsProvider{}, &FakeLogger{})
	require.NoError(t, err)

	require.NoError(t, pq.Enqueue("one"))
	require.NoError(t, pq.Enqueue("two"))
	require.NoError(t, pq.Enqueue("three"))

	// Advance reader to after "one"; capture the filename and that offset.
	_, fn, offAfterOne, err := pq.Dequeue(context.Background(), "test_reader1")
	require.NoError(t, err)
	require.NoError(t, pq.Ack("test_reader1"))

	// Advance reader past "two" and "three".
	_, _, _, err = pq.Dequeue(context.Background(), "test_reader1")
	require.NoError(t, err)
	require.NoError(t, pq.Ack("test_reader1"))
	_, _, _, err = pq.Dequeue(context.Background(), "test_reader1")
	require.NoError(t, err)
	require.NoError(t, pq.Ack("test_reader1"))

	// Rewind to just before "two" and persist.
	require.NoError(t, pq.SetState("test_reader1", fn, offAfterOne))

	// Restart: new queue instance, same dir.
	pq2, err := NewPersistentQueue("testq_ssp", tmpDir, OsProvider{}, &FakeLogger{})
	require.NoError(t, err)

	item, _, _, err := pq2.Dequeue(context.Background(), "test_reader1")
	require.NoError(t, err)
	assert.Equal(t, "two", item)
}

// TestSetState_WSReader_InMemory verifies that SetState for a ws_ reader does
// NOT write a state file and that the state does not survive a restart.
func TestSetState_WSReader_InMemory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_extra_setstate_ws")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	pq, err := NewPersistentQueue("testq_ssw", tmpDir, OsProvider{}, &FakeLogger{})
	require.NoError(t, err)

	require.NoError(t, pq.Enqueue("msg1"))

	// Read once to learn the file name and position after the entry.
	_, fn, offEnd, err := pq.Dequeue(context.Background(), "helper_reader")
	require.NoError(t, err)
	require.NoError(t, pq.Ack("helper_reader"))

	// Set ws_ reader state to the end-of-file position.
	err = pq.SetState("ws_client1", fn, offEnd)
	require.NoError(t, err)

	// No state file should exist for the ws_ reader.
	wsStateFile := filepath.Join(tmpDir, "reader_state_testq_ssw_ws_client1.json")
	_, statErr := os.Stat(wsStateFile)
	assert.True(t, os.IsNotExist(statErr), "ws_ reader state must not be written to disk")

	// After a restart the ws_ reader's in-memory state is gone, so it starts
	// from the beginning of the queue and picks up "msg1" again.
	pq2, err := NewPersistentQueue("testq_ssw", tmpDir, OsProvider{}, &FakeLogger{})
	require.NoError(t, err)

	item, _, _, err := pq2.Dequeue(context.Background(), "ws_client1")
	require.NoError(t, err)
	assert.Equal(t, "msg1", item)
}

// TestSeekToEnd_SkipsExistingEntries verifies that SeekToEnd causes all
// already-enqueued entries to be skipped by the reader.
func TestSeekToEnd_SkipsExistingEntries(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_extra_seek")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	pq, err := NewPersistentQueue("testq_seek", tmpDir, OsProvider{}, &FakeLogger{})
	require.NoError(t, err)

	require.NoError(t, pq.Enqueue("e1"))
	require.NoError(t, pq.Enqueue("e2"))
	require.NoError(t, pq.Enqueue("e3"))

	require.NoError(t, pq.SeekToEnd("test_reader1"))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, _, _, err = pq.Dequeue(ctx, "test_reader1")
	assert.ErrorIs(t, err, context.DeadlineExceeded, "reader should block because all entries were skipped")
}

// TestSeekToEnd_EmptyQueue verifies that SeekToEnd on an empty queue
// succeeds and that entries enqueued afterwards are still delivered.
func TestSeekToEnd_EmptyQueue(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_extra_seek_empty")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	pq, err := NewPersistentQueue("testq_seekempty", tmpDir, OsProvider{}, &FakeLogger{})
	require.NoError(t, err)

	// Seek on empty queue must not error.
	require.NoError(t, pq.SeekToEnd("test_reader1"))

	// Entries added after the seek should be visible.
	require.NoError(t, pq.Enqueue("new_entry"))

	item, _, _, err := pq.Dequeue(context.Background(), "test_reader1")
	require.NoError(t, err)
	assert.Equal(t, "new_entry", item)
}

// TestSeekToEnd_MultipleReaders verifies that SeekToEnd only affects the
// specified reader; other readers continue from their own positions.
func TestSeekToEnd_MultipleReaders(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_extra_seek_multi")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	pq, err := NewPersistentQueue("testq_seekm", tmpDir, OsProvider{}, &FakeLogger{})
	require.NoError(t, err)

	require.NoError(t, pq.Enqueue("x1"))
	require.NoError(t, pq.Enqueue("x2"))
	require.NoError(t, pq.Enqueue("x3"))

	// Only reader_a is seeked to end.
	require.NoError(t, pq.SeekToEnd("test_reader_a"))

	// reader_a should see nothing.
	ctxA, cancelA := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelA()
	_, _, _, err = pq.Dequeue(ctxA, "test_reader_a")
	assert.ErrorIs(t, err, context.DeadlineExceeded, "reader_a should see no pre-existing entries")

	// reader_b (never seeked) should still get all original entries.
	item, _, _, err := pq.Dequeue(context.Background(), "test_reader_b")
	require.NoError(t, err)
	assert.Equal(t, "x1", item)

	require.NoError(t, pq.Ack("test_reader_b"))
	item, _, _, err = pq.Dequeue(context.Background(), "test_reader_b")
	require.NoError(t, err)
	assert.Equal(t, "x2", item)
}

// TestNewPersistentQueue_MkdirFail verifies that NewPersistentQueue returns
// an error when the directory cannot be created (a regular file blocks
// the creation of a subdirectory).
func TestNewPersistentQueue_MkdirFail(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_extra_mkdirfail")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	// Place a regular file where a directory would need to be created.
	blocker := filepath.Join(tmpDir, "not_a_dir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0600))

	// Trying to use blocker as a parent directory must fail.
	_, err = NewPersistentQueue("q", filepath.Join(blocker, "sub"), OsProvider{}, &FakeLogger{})
	assert.Error(t, err, "should fail when the directory path cannot be created")
}

// TestSetState_WSReader_InMemory_SameSession additionally confirms that within
// the SAME queue instance the ws_ state set via SetState is respected
// immediately by the next Dequeue call.
func TestSetState_WSReader_SameSession(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "queue_extra_ws_same")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	pq, err := NewPersistentQueue("testq_wss", tmpDir, OsProvider{}, &FakeLogger{})
	require.NoError(t, err)

	require.NoError(t, pq.Enqueue("p1"))
	require.NoError(t, pq.Enqueue("p2"))

	// Capture filename and offset after "p1".
	_, fn, offAfterP1, err := pq.Dequeue(context.Background(), "ws_sametest")
	require.NoError(t, err)
	require.NoError(t, pq.Ack("ws_sametest"))

	// Advance past "p2".
	_, _, offAfterP2, err := pq.Dequeue(context.Background(), "ws_sametest")
	require.NoError(t, err)
	require.NoError(t, pq.Ack("ws_sametest"))

	// Seek ws_ reader back to just before "p2" in the same session.
	require.NoError(t, pq.SetState("ws_sametest", fn, offAfterP1))
	_ = offAfterP2 // not used further

	item, _, _, err := pq.Dequeue(context.Background(), "ws_sametest")
	require.NoError(t, err)
	assert.Equal(t, "p2", item)
}
