package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Compile-time check that *Messenger implements MessengerApi
var _ MessengerApi = (*Messenger)(nil)

func TestMessenger_SubscribeAndSend_ToMultipleSubscribers(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_test")
	if err != nil {
		t.Fatal(err)
	}
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)), OsProvider{})
	m.dataDir = tmpDir

	var ch1Msg Message
	var ch2Msg Message
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = m.Subscribe(ctx, "test1", "alpha", "testType", "test1", 0, func(msg Message) error { ch1Msg = msg; return nil })
	assert.NoError(t, err)
	err = m.Subscribe(ctx, "test2", "alpha", "testType", "test2", 0, func(msg Message) error { ch2Msg = msg; return nil })
	assert.NoError(t, err)

	// Send in a goroutine to avoid blocking on unbuffered channels
	go func() {
		_ = m.Send(Message{ChannelName: "alpha", Text: "hello"})
	}()

	time.Sleep(1 * time.Second)

	// Both subscribers should receive the same message
	assert.Equal(t, "hello", ch1Msg.Text)
	assert.Equal(t, "hello", ch2Msg.Text)

}

func TestMessenger_Send_IsolatedByChannel(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_test_iso")
	if err != nil {
		t.Fatal(err)
	}
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)), OsProvider{})
	m.dataDir = tmpDir

	var ch1Msg Message
	var ch2Msg Message
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = m.Subscribe(ctx, "test", "a", "testType", "test", 0, func(msg Message) error { ch1Msg = msg; return nil })
	assert.NoError(t, err)
	err = m.Subscribe(ctx, "test", "b", "testType", "test", 0, func(msg Message) error { ch2Msg = msg; return nil })
	assert.NoError(t, err)

	// Send to channel "a" only
	go func() { _ = m.Send(Message{ChannelName: "a", Text: "foo"}) }()

	time.Sleep(1 * time.Second)

	assert.Equal(t, "foo", ch1Msg.Text)
	assert.Equal(t, "", ch2Msg.Text)
}

func TestMessenger_Send_OrderPreserved(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_test_order")
	if err != nil {
		t.Fatal(err)
	}
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)), OsProvider{})
	m.dataDir = tmpDir

	var messages []Message
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = m.Subscribe(ctx, "test", "ordered", "testType", "test", 0, func(msg Message) error { messages = append(messages, msg); return nil })
	assert.NoError(t, err)

	// Send three messages in order in a single goroutine
	for i := 1; i <= 3; i++ {
		_ = m.Send(Message{ChannelName: "ordered", Text: fmt.Sprintf("%d", i)})
	}

	time.Sleep(1 * time.Second)

	if assert.Len(t, messages, 3) {
		assert.Equal(t, "1", messages[0].Text)
		assert.Equal(t, "2", messages[1].Text)
		assert.Equal(t, "3", messages[2].Text)
	}
}

func TestMessenger_Send_DiscardDuplicateRoute(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_test_discard")
	if err != nil {
		t.Fatal(err)
	}
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)), OsProvider{})
	m.dataDir = tmpDir

	channelName := "discard-test"
	hostname, _ := m.osProvider.Hostname()
	// get short hostname
	if idx := strings.Index(hostname, "."); idx != -1 {
		hostname = hostname[:idx]
	}
	addRoute := fmt.Sprintf("%s:%s", hostname, channelName)

	var received bool
	err = m.Subscribe(context.Background(), "test", channelName, "testType", "test", 0, func(msg Message) error {
		received = true
		return nil
	})
	assert.NoError(t, err)

	// Send message that already has the route
	msg := Message{
		ChannelName: channelName,
		Text:        "should be discarded",
		Route:       []string{addRoute},
	}

	err = m.Send(msg)
	assert.NoError(t, err)

	time.Sleep(200 * time.Millisecond)
	assert.False(t, received, "Message should have been discarded and not received by subscriber")
}

func TestMessenger_Send_NoSubscribers_NoError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_test_none")
	if err != nil {
		t.Fatal(err)
	}
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)), OsProvider{})
	m.dataDir = tmpDir

	// Should not block and should return nil
	err = m.Send(Message{ChannelName: "nobody", Text: "ignored"})
	assert.NoError(t, err)
}

func TestMessenger_Send_DataPassedInMessage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_test_json")
	if err != nil {
		t.Fatal(err)
	}
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	m := NewMessenger(&FakeLogger{}, OsProvider{})
	m.dataDir = tmpDir
	m.hostname = "host-1"

	var gotMessage Message
	err = m.Subscribe(context.Background(), "test", "json", "testType", "test", 0, func(msg Message) error { gotMessage = msg; return nil })
	assert.NoError(t, err)

	// Define a struct
	type payload struct {
		K string `json:"k"`
		N int    `json:"n"`
	}
	p := payload{K: "v", N: 123}

	go func() {
		_ = m.Send(Message{ChannelName: "json", Text: "with-data", Data: p})
	}()

	time.Sleep(1 * time.Second)

	assert.Equal(t, "host-1", gotMessage.Hostname)
	assert.False(t, gotMessage.Timestamp.IsZero())

	// Verify Data contains the payload
	if assert.NotNil(t, gotMessage.Data) {
		data, ok := gotMessage.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("expected Data to be map[string]interface{}, got %T", gotMessage.Data)
		}

		t.Logf("Data type: %T, Data: %+v\n", gotMessage.Data, data)
		assert.Equal(t, "v", data["k"])
		assert.Equal(t, 123.0, data["n"])
	}
}

func TestNewMessenger_LoggerNotInitialized(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when logger is nil")
		}
	}()
	NewMessenger(nil, FakeOsProvider{Host: "test"})
}

func TestNewMessenger_OsProviderNotInitialized(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when osProvider is nil")
		}
	}()
	NewMessenger(&FakeLogger{}, nil)
}

func TestNewMessenger_HostnameError_LoggedAndEmptyHostname(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_test_hostname")
	if err != nil {
		t.Fatal(err)
	}
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	fl := &FakeLogger{}
	testErr := errors.New("hostname lookup failed")

	// We use FakeOsProvider to trigger the error, but we need to fix it later to use OsProvider for queue
	m := NewMessenger(fl, FakeOsProvider{Host: "ignored", Err: testErr})
	m.dataDir = tmpDir
	m.osProvider = OsProvider{} // Swap to real OS provider for queue operations

	// Verify error was logged with expected message and args
	assert.Equal(t, "Failed to determine hostname during initialization", fl.lastErrMsg)
	if assert.Len(t, fl.lastErrArgs, 2) {
		assert.Equal(t, "error", fl.lastErrArgs[0])
		assert.Equal(t, testErr, fl.lastErrArgs[1])
	}

	// Verify that resulting messenger uses empty hostname when sending
	var gotMessage Message
	err = m.Subscribe(context.Background(), "test", "test", "testType", "test", 0, func(msg Message) error { gotMessage = msg; return nil })
	assert.NoError(t, err)

	go func() { _ = m.Send(Message{ChannelName: "test", Text: "ping"}) }()

	time.Sleep(1 * time.Second)

	assert.Equal(t, "", gotMessage.Hostname)
	assert.Equal(t, "ping", gotMessage.Text)
}

func TestMessenger_SetDataDir(t *testing.T) {
	m := &Messenger{}
	m.SetDataDir("new-dir")
	assert.Equal(t, "new-dir", m.dataDir)
}

func TestMessenger_InitializePersistentQueue_Error(t *testing.T) {
	fl := &FakeLogger{}
	// Make MkdirAll fail to trigger error in NewPersistentQueue
	myErr := errors.New("mkdir failed")
	osProv := FakeOsProvider{
		MkdirAllFunc: func(path string, perm os.FileMode) error {
			return myErr
		},
	}
	m := NewMessenger(fl, osProv)
	err := m.Send(Message{ChannelName: "test", Text: "foo"})
	assert.Error(t, err)
	assert.Equal(t, myErr, err)
}

func TestMessenger_Send_EnqueueError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_test_enqueue_err")
	if err != nil {
		t.Fatal(err)
	}
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	fl := &FakeLogger{}
	m := NewMessenger(fl, OsProvider{})
	m.dataDir = tmpDir

	// We need to initialize the queue first, then we can mock it or make it fail.
	// Actually, PersistentQueue uses OsProvider, so we can mock OpenFile to fail after initialization.

	err = m.initializePersistentQueue("fail-channel")
	assert.NoError(t, err)

	// Now make subsequent operations on this channel fail if we can.
	// PersistentQueue.Enqueue calls OpenFile if logFile is nil or for next file.
	// But it keeps it open.

	// Let's use a more direct approach by injecting a FakeOsProvider that fails.
	myErr := errors.New("write error")
	osProv := FakeOsProvider{
		OpenFileFunc: func(name string, flag int, perm os.FileMode) (FileApi, error) {
			return nil, myErr
		},
	}
	m.osProvider = osProv
	// Clear the existing queue to force re-initialization with the bad OsProvider
	m.queues = make(map[string]*PersistentQueue)

	err = m.Send(Message{ChannelName: "fail-channel", Text: "foo"})
	assert.Error(t, err)
}

func TestMessenger_Subscribe_GoroutineErrors(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_test_sub_err")
	if err != nil {
		t.Fatal(err)
	}
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	fl := &FakeLogger{}
	m := NewMessenger(fl, OsProvider{})
	m.dataDir = tmpDir

	// 1. Dequeue error
	// Trigger error in loadState which is called by Dequeue
	myErr := errors.New("loadState error")
	osProv := FakeOsProvider{
		StatFunc: func(name string) (os.FileInfo, error) {
			if strings.Contains(name, "reader_state_err-test-chan_test-source.json") {
				return nil, myErr
			}
			return os.Stat(name)
		},
		OpenFileFunc: func(name string, flag int, perm os.FileMode) (FileApi, error) {
			if strings.Contains(name, "reader_state_err-test-chan_test-source.json") {
				return nil, myErr
			}
			return OsProvider{}.OpenFile(name, flag, perm)
		},
		MkdirAllFunc: func(path string, perm os.FileMode) error {
			return os.MkdirAll(path, perm)
		},
		ReadDirFunc: func(name string) ([]os.DirEntry, error) {
			return os.ReadDir(name)
		},
	}

	m.osProvider = osProv
	// Pre-initialize the queue with the fake os provider
	err = m.initializePersistentQueue("err-test-chan")
	assert.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Using the SAME source "test-source" that triggers the error in our fake os provider
	err = m.Subscribe(ctx, "test-source", "err-test-chan", "testType", "test", 0, func(msg Message) error { return nil })
	assert.NoError(t, err)

	assert.Eventually(t, func() bool {
		return fl.LastErrMsg() == "Failed to dequeue message"
	}, 2*time.Second, 100*time.Millisecond)

	// 2. Unmarshal error
	// To trigger this, we need a malformed JSON in the queue.
	// We can manually write to the log file.
	cancel() // Stop previous subscription

	m.osProvider = OsProvider{} // Back to real OS
	m.queues = make(map[string]*PersistentQueue)
	err = m.initializePersistentQueue("bad-test-json")
	assert.NoError(t, err)

	logPath := fmt.Sprintf("%s/bad-test-json_queue_%s.log", tmpDir, time.Now().Format("20060102"))
	err = os.WriteFile(logPath, []byte("invalid json\n"), 0644)
	assert.NoError(t, err)

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	err = m.Subscribe(ctx2, "test-source", "bad-test-json", "testType", "test", 0, func(msg Message) error { return nil })
	assert.NoError(t, err)

	assert.Eventually(t, func() bool {
		return fl.LastErrMsg() == "Failed to unmarshal dequeued message"
	}, 2*time.Second, 100*time.Millisecond)

	// 3. Handler error
	cancel2() // Stop previous subscription
	// Use a fresh messenger and logger to avoid interference
	m2 := NewMessenger(&FakeLogger{}, OsProvider{})
	m2.dataDir = tmpDir
	err = m2.initializePersistentQueue("handler-test-err")
	assert.NoError(t, err)

	handlerErr := errors.New("handler failed")
	ctx3, cancel3 := context.WithCancel(context.Background())
	defer cancel3()
	err = m2.Subscribe(ctx3, "test-source", "handler-test-err", "testType", "test", 0, func(msg Message) error { return handlerErr })
	assert.NoError(t, err)

	_ = m2.Send(Message{ChannelName: "handler-test-err", Text: "trigger"})

	assert.Eventually(t, func() bool {
		return m2.logger.(*FakeLogger).LastErrMsg() == "Message handler returned error, retrying"
	}, 2*time.Second, 100*time.Millisecond)
}

func TestMessenger_Subscribe_RetryOnHandlerError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_retry_test")
	if err != nil {
		t.Fatal(err)
	}
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	fl := &FakeLogger{}
	m := NewMessenger(fl, OsProvider{})
	m.dataDir = tmpDir

	var callCount int32
	handlerErr := errors.New("temporary handler failure")

	err = m.Subscribe(context.Background(), "retry-test-source", "retry-test-chan", "testType", "test", 0, func(msg Message) error {
		count := atomic.AddInt32(&callCount, 1)
		if count < 3 {
			return handlerErr
		}
		return nil
	})
	assert.NoError(t, err)

	_ = m.Send(Message{ChannelName: "retry-test-chan", Text: "retry-me"})

	assert.Eventually(t, func() bool {
		return atomic.LoadInt32(&callCount) >= 3
	}, 10*time.Second, 100*time.Millisecond, "Expected at least 3 calls due to retries")
}

func TestMessenger_Subscribe_OrderPreservedWithRetries(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_order_retry_test")
	if err != nil {
		t.Fatal(err)
	}
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	fl := &FakeLogger{}
	m := NewMessenger(fl, OsProvider{})
	m.dataDir = tmpDir

	var received []string
	var mu sync.Mutex
	var callCount int32

	err = m.Subscribe(context.Background(), "order-test-source", "order-test-chan", "testType", "test", 0, func(msg Message) error {
		mu.Lock()
		defer mu.Unlock()

		count := atomic.AddInt32(&callCount, 1)
		if msg.Text == "first" && count == 1 {
			return errors.New("fail first once")
		}
		received = append(received, msg.Text)
		return nil
	})
	assert.NoError(t, err)

	_ = m.Send(Message{ChannelName: "order-test-chan", Text: "first"})
	_ = m.Send(Message{ChannelName: "order-test-chan", Text: "second"})

	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) == 2
	}, 10*time.Second, 100*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"first", "second"}, received)
}

func TestMessenger_Subscribe_MaxAge(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_test_maxage")
	if err != nil {
		t.Fatal(err)
	}
	//goland:noinspection GoUnhandledErrorResult
	defer os.RemoveAll(tmpDir)

	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)), OsProvider{})
	m.dataDir = tmpDir

	var received []string
	var mu sync.Mutex

	// Subscribe with a max age of 1 hour
	maxAge := 1 * time.Hour
	err = m.Subscribe(context.Background(), "test-maxage-source", "maxage-test-chan", "testType", "test", maxAge, func(msg Message) error {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, msg.Text)
		return nil
	})
	assert.NoError(t, err)

	// Send a message that is 2 hours old (should be skipped)
	oldMsg := Message{
		ChannelName: "maxage-test-chan",
		Text:        "too old",
		Timestamp:   time.Now().Add(-2 * time.Hour),
	}
	_ = m.Send(oldMsg)

	// Send a message that is 30 minutes old (should be received)
	recentMsg := Message{
		ChannelName: "maxage-test-chan",
		Text:        "recent",
		Timestamp:   time.Now().Add(-30 * time.Minute),
	}
	_ = m.Send(recentMsg)

	// Send a message with zero timestamp (should be assigned current time and received)
	newMsg := Message{
		ChannelName: "maxage-test-chan",
		Text:        "new",
	}
	_ = m.Send(newMsg)

	time.Sleep(1 * time.Second)

	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, received, 2)
	assert.NotContains(t, received, "too old")
	assert.Contains(t, received, "recent")
	assert.Contains(t, received, "new")
}

func TestMessenger_InitializePersistentQueue_NilQueues(t *testing.T) {
	fl := &FakeLogger{}
	m := &Messenger{
		logger:     fl,
		osProvider: FakeOsProvider{},
		dataDir:    "data",
	}
	// m.queues is nil here
	err := m.initializePersistentQueue("test-chan")
	assert.NoError(t, err)
	assert.NotNil(t, m.queues)
	assert.Contains(t, m.queues, "test-chan")
}

func TestMessenger_Stats(t *testing.T) {
	logger := &FakeLogger{}
	tempDir := t.TempDir()
	osProvider := OsProvider{}
	m := NewMessenger(logger, osProvider)
	dataDir := filepath.Join(tempDir, "data")
	m.SetDataDir(dataDir)

	msg1 := Message{ChannelName: "chan1", Text: "msg1"}
	msg2 := Message{ChannelName: "chan1", Text: "msg2"}
	msg3 := Message{ChannelName: "chan2", Text: "msg3"}

	err1 := m.Send(msg1)
	err2 := m.Send(msg2)
	err3 := m.Send(msg3)

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.NoError(t, err3)

	stats := m.GetStats()
	assert.Equal(t, int64(3), stats.TotalMessageCount)
	assert.Equal(t, int64(2), stats.ChannelMessageCounts["chan1"])
	assert.Equal(t, int64(1), stats.ChannelMessageCounts["chan2"])
	assert.Equal(t, int64(0), stats.TotalFailureCount)
	assert.Equal(t, int64(0), stats.TotalRetryCount)
}

func TestMessenger_RetryStats(t *testing.T) {
	logger := &FakeLogger{}
	tempDir := t.TempDir()
	osProvider := OsProvider{}
	m := NewMessenger(logger, osProvider)
	dataDir := filepath.Join(tempDir, "data")
	m.SetDataDir(dataDir)

	channelName := "test_retry_stats" // must contain "test" for fast retry
	msg := Message{ChannelName: channelName, Text: "retry_msg"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	retryCount := 0
	handler := func(m Message) error {
		if retryCount < 2 {
			retryCount++
			return fmt.Errorf("temporary error")
		}
		return nil
	}

	err := m.Subscribe(ctx, "subscriber1", channelName, "testType", "testName", 0, handler)
	assert.NoError(t, err)

	err = m.Send(msg)
	assert.NoError(t, err)

	// Wait for retries to happen and succeed
	// The sleep time in SubscribeExtended is 10ms for "test" source/channel
	time.Sleep(500 * time.Millisecond)

	stats := m.GetStats()
	assert.Equal(t, int64(2), stats.TotalRetryCount)
}
