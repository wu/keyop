package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
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
	defer os.RemoveAll(tmpDir)

	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)), OsProvider{})
	m.dataDir = tmpDir

	var ch1Msg Message
	var ch2Msg Message
	err = m.Subscribe("test1", "alpha", func(msg Message) error { ch1Msg = msg; return nil })
	assert.NoError(t, err)
	err = m.Subscribe("test2", "alpha", func(msg Message) error { ch2Msg = msg; return nil })
	assert.NoError(t, err)

	// Send in a goroutine to avoid blocking on unbuffered channels
	go func() {
		_ = m.Send(Message{ChannelName: "alpha", Text: "hello"}, nil)
	}()

	time.Sleep(500 * time.Millisecond)

	// Both subscribers should receive the same message
	assert.Equal(t, "hello", ch1Msg.Text)
	assert.Equal(t, "hello", ch2Msg.Text)

}

func TestMessenger_Send_IsolatedByChannel(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_test_iso")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)), OsProvider{})
	m.dataDir = tmpDir

	var ch1Msg Message
	var ch2Msg Message
	err = m.Subscribe("test", "a", func(msg Message) error { ch1Msg = msg; return nil })
	assert.NoError(t, err)
	err = m.Subscribe("test", "b", func(msg Message) error { ch2Msg = msg; return nil })
	assert.NoError(t, err)

	// Send to channel "a" only
	go func() { _ = m.Send(Message{ChannelName: "a", Text: "foo"}, nil) }()

	time.Sleep(500 * time.Millisecond)

	assert.Equal(t, "foo", ch1Msg.Text)
	assert.Equal(t, "", ch2Msg.Text)
}

func TestMessenger_Send_OrderPreserved(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_test_order")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)), OsProvider{})
	m.dataDir = tmpDir

	var messages []Message
	err = m.Subscribe("test", "ordered", func(msg Message) error { messages = append(messages, msg); return nil })
	assert.NoError(t, err)

	// Send three messages in order in a single goroutine
	for i := 1; i <= 3; i++ {
		_ = m.Send(Message{ChannelName: "ordered", Text: fmt.Sprintf("%d", i)}, nil)
	}

	time.Sleep(500 * time.Millisecond)

	if assert.Len(t, messages, 3) {
		assert.Equal(t, "1", messages[0].Text)
		assert.Equal(t, "2", messages[1].Text)
		assert.Equal(t, "3", messages[2].Text)
	}
}

func TestMessenger_Send_NoSubscribers_NoError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_test_none")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	m := NewMessenger(slog.New(slog.NewJSONHandler(os.Stderr, nil)), OsProvider{})
	m.dataDir = tmpDir

	// Should not block and should return nil
	err = m.Send(Message{ChannelName: "nobody", Text: "ignored"}, nil)
	assert.NoError(t, err)
}

func TestMessenger_Send_SerializesDataToJSON(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_test_json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	m := NewMessenger(&FakeLogger{}, OsProvider{})
	m.dataDir = tmpDir
	m.hostname = "host-1"

	var gotMessage Message
	err = m.Subscribe("test", "json", func(msg Message) error { gotMessage = msg; return nil })
	assert.NoError(t, err)

	// Define a struct to ensure stable JSON field order
	type payload struct {
		K string `json:"k"`
		N int    `json:"n"`
	}
	p := payload{K: "v", N: 123}

	go func() {
		_ = m.Send(Message{ChannelName: "json", Text: "with-data"}, p)
	}()

	time.Sleep(500 * time.Millisecond)

	assert.Equal(t, "host-1", gotMessage.Hostname)
	assert.False(t, gotMessage.Timestamp.IsZero())

	b, _ := json.Marshal(p)
	assert.Equal(t, string(b), gotMessage.Data)
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
	err = m.Subscribe("test", "test", func(msg Message) error { gotMessage = msg; return nil })
	assert.NoError(t, err)

	go func() { _ = m.Send(Message{ChannelName: "test", Text: "ping"}, nil) }()

	time.Sleep(500 * time.Millisecond)

	assert.Equal(t, "", gotMessage.Hostname)
	assert.Equal(t, "ping", gotMessage.Text)
}

func TestMessenger_Send_FailedToSerializeData_LogsErrorAndSendsWithoutData(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_test_failed_serialize")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	fl := &FakeLogger{}
	m := NewMessenger(fl, OsProvider{})
	m.dataDir = tmpDir

	var gotMessage Message
	err = m.Subscribe("test", "bad", func(msg Message) error { gotMessage = msg; return nil })
	assert.NoError(t, err)

	// Use a channel value which json.Marshal cannot serialize to trigger an error
	bad := make(chan int)
	go func() { _ = m.Send(Message{ChannelName: "bad", Text: "oops"}, bad) }()

	time.Sleep(500 * time.Millisecond)

	assert.Equal(t, "oops", gotMessage.Text)

	// Ensure the error was logged with the expected message and args
	assert.Equal(t, "Failed to serialize data", fl.lastErrMsg)
	if assert.Len(t, fl.lastErrArgs, 2) {
		assert.Equal(t, "error", fl.lastErrArgs[0])
		if _, ok := fl.lastErrArgs[1].(error); !ok {
			t.Fatalf("expected an error type for logger arg[1]")
		}
	}
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
	err := m.Send(Message{ChannelName: "test", Text: "foo"}, nil)
	assert.Error(t, err)
	assert.Equal(t, myErr, err)
}

func TestMessenger_Send_EnqueueError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_test_enqueue_err")
	if err != nil {
		t.Fatal(err)
	}
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

	err = m.Send(Message{ChannelName: "fail-channel", Text: "foo"}, nil)
	assert.Error(t, err)
}

func TestMessenger_Subscribe_GoroutineErrors(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_test_sub_err")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	fl := &FakeLogger{}
	m := NewMessenger(fl, OsProvider{})
	m.dataDir = tmpDir

	// 1. Dequeue error
	// Trigger error in loadState which is called by Dequeue
	myErr := errors.New("loadState error")
	osProv := FakeOsProvider{
		OpenFileFunc: func(name string, flag int, perm os.FileMode) (FileApi, error) {
			if strings.Contains(name, "reader_state_err-chan_source.json") {
				return nil, myErr
			}
			return OsProvider{}.OpenFile(name, flag, perm)
		},
	}

	// We need to let initializePersistentQueue succeed first.
	err = m.initializePersistentQueue("err-chan")
	assert.NoError(t, err)

	m.osProvider = osProv
	// Clear the existing queue to force re-initialization with the bad OsProvider
	m.queues = make(map[string]*PersistentQueue)

	err = m.Subscribe("source", "err-chan", func(msg Message) error { return nil })
	assert.NoError(t, err)

	assert.Eventually(t, func() bool {
		return fl.lastErrMsg == "Failed to dequeue message"
	}, 2*time.Second, 100*time.Millisecond)

	// 2. Unmarshal error
	// To trigger this, we need a malformed JSON in the queue.
	// We can manually write to the log file.
	m.osProvider = OsProvider{} // Back to real OS
	m.queues = make(map[string]*PersistentQueue)
	err = m.initializePersistentQueue("bad-json")
	assert.NoError(t, err)

	logPath := fmt.Sprintf("%s/bad-json_queue_%s.log", tmpDir, time.Now().Format("20060102"))
	err = os.WriteFile(logPath, []byte("invalid json\n"), 0644)
	assert.NoError(t, err)

	err = m.Subscribe("source", "bad-json", func(msg Message) error { return nil })
	assert.NoError(t, err)

	assert.Eventually(t, func() bool {
		return fl.lastErrMsg == "Failed to unmarshal dequeued message"
	}, 2*time.Second, 100*time.Millisecond)

	// 3. Handler error
	err = m.initializePersistentQueue("handler-err")
	assert.NoError(t, err)

	handlerErr := errors.New("handler failed")
	err = m.Subscribe("source", "handler-err", func(msg Message) error { return handlerErr })
	assert.NoError(t, err)

	_ = m.Send(Message{ChannelName: "handler-err", Text: "trigger"}, nil)

	assert.Eventually(t, func() bool {
		return fl.lastErrMsg == "Message handler returned error"
	}, 2*time.Second, 100*time.Millisecond)
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
