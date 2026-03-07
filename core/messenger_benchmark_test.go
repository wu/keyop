//nolint:revive
package core

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkMessengerThroughput measures end-to-end throughput for the Messenger using
// the on-disk persistent queue. It is a full integration benchmark that writes
// queue files to a temporary directory and subscribes to them using the real
// PersistentQueue reader. The benchmark publishes b.N messages and waits for the
// subscriber to process them, reporting allocations and time.
func BenchmarkMessengerThroughput(b *testing.B) {
	// Use a temp dir so test artifacts don't land in the user's real data dir.
	tmpDir, err := os.MkdirTemp("", "messenger_bench")
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	m := NewMessenger(logger, OsProvider{})
	m.SetDataDir(tmpDir)

	// Use a source name that contains "test" so the queue reader uses the
	// shorter polling interval optimized for tests.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var received int64
	if err := m.Subscribe(ctx, "bench_test_reader", "bench-throughput", "bench", "bench", 0, func(msg Message) error {
		atomic.AddInt64(&received, 1)
		return nil
	}); err != nil {
		b.Fatalf("failed to subscribe: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := m.Send(Message{ChannelName: "bench-throughput", Text: "ping"}); err != nil {
			b.Fatalf("send failed: %v", err)
		}
	}

	// Wait for all messages to be processed, with a timeout scaled by N to
	// avoid flakiness on slower machines.
	maxWait := 10*time.Second + time.Duration(b.N/1000)*time.Second
	deadline := time.After(maxWait)
	for atomic.LoadInt64(&received) < int64(b.N) {
		select {
		case <-deadline:
			b.Fatalf("timeout waiting for messages: got %d expected %d", received, b.N)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	b.StopTimer()
}
