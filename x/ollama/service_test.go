package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"keyop/core"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func testDeps(t *testing.T) core.Dependencies {
	logger := &core.FakeLogger{}
	deps := core.Dependencies{}

	ctx, cancel := context.WithCancel(context.Background())
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	t.Cleanup(cancel)

	tmpDir, err := os.MkdirTemp("", "ollama_test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	fakeOs := core.FakeOsProvider{
		Host: "test-host",
		Home: tmpDir,
		OpenFileFunc: func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
			return os.OpenFile(name, flag, perm)
		},
		MkdirAllFunc: os.MkdirAll,
		StatFunc:     os.Stat,
		ReadFileFunc: os.ReadFile,
		ReadDirFunc:  os.ReadDir,
	}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(logger)
	messenger := core.NewMessenger(logger, deps.MustGetOsProvider())
	messenger.SetDataDir(tmpDir)

	deps.SetMessenger(messenger)

	state := core.NewFileStateStore(tmpDir, deps.MustGetOsProvider())
	deps.SetStateStore(state)

	return deps
}

func TestService_ValidateConfig(t *testing.T) {
	deps := testDeps(t)

	tests := []struct {
		name        string
		config      map[string]interface{}
		subs        map[string]core.ChannelInfo
		pubs        map[string]core.ChannelInfo
		expectError bool
	}{
		{
			name: "valid config",
			config: map[string]interface{}{
				"host": "localhost",
				"port": 11434,
			},
			subs: map[string]core.ChannelInfo{
				"requests": {Name: "ollama-req"},
			},
			pubs: map[string]core.ChannelInfo{
				"responses": {Name: "ollama-resp"},
			},
			expectError: false,
		},
		{
			name: "missing host",
			config: map[string]interface{}{
				"port": 11434,
			},
			subs: map[string]core.ChannelInfo{
				"requests": {Name: "ollama-req"},
			},
			pubs: map[string]core.ChannelInfo{
				"responses": {Name: "ollama-resp"},
			},
			expectError: true,
		},
		{
			name: "missing responses pub",
			config: map[string]interface{}{
				"host": "localhost",
				"port": 11434,
			},
			subs: map[string]core.ChannelInfo{
				"requests": {Name: "ollama-req"},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := core.ServiceConfig{
				Config: tt.config,
				Subs:   tt.subs,
				Pubs:   tt.pubs,
			}
			svc := NewService(deps, cfg)
			errs := svc.ValidateConfig()

			if tt.expectError {
				assert.NotEmpty(t, errs)
			} else {
				assert.Empty(t, errs)
			}
		})
	}
}

func TestService_ChatAndBatch(t *testing.T) {
	deps := testDeps(t)
	messenger := deps.MustGetMessenger()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("Expected /api/chat, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		json.NewEncoder(w).Encode(ChatResponse{Message: Message{Role: "assistant", Content: "Hello "}})
		w.Write([]byte("\n"))
		json.NewEncoder(w).Encode(ChatResponse{Message: Message{Role: "assistant", Content: "world!"}, Done: true})
		w.Write([]byte("\n"))
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	host := u.Hostname()
	port, _ := strconv.Atoi(u.Port())

	cfg := core.ServiceConfig{
		Name: "ollama",
		Config: map[string]interface{}{
			"host":      host,
			"port":      port,
			"model":     "llama3.3",
			"batchSize": 6,
		},
		Subs: map[string]core.ChannelInfo{
			"requests": {Name: "ollama-req"},
		},
		Pubs: map[string]core.ChannelInfo{
			"responses": {Name: "ollama-resp"},
		},
	}

	svc := NewService(deps, cfg).(*Service)

	received := make(chan core.Message, 10)
	_ = messenger.Subscribe(context.Background(), "test", "ollama-resp", "ollama", "ollama", 0, func(m core.Message) error {
		received <- m
		return nil
	})

	err := svc.messageHandler(core.Message{Text: "Hi"})
	if err != nil {
		t.Fatalf("messageHandler error: %v", err)
	}

	select {
	case m := <-received:
		if m.Text != "Hello " {
			t.Fatalf("expected first batch 'Hello ', got %q", m.Text)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for first batch")
	}

	select {
	case m := <-received:
		if m.Text != "world!" {
			t.Fatalf("expected second batch 'world!', got %q", m.Text)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for second batch")
	}
}

func TestService_HistorySummarize(t *testing.T) {
	deps := testDeps(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		var req ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		// If summarization prompt detected, return a short summary in one chunk
		if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "summarize") {
			json.NewEncoder(w).Encode(ChatResponse{Message: Message{Role: "assistant", Content: "[SUMMARY]"}, Done: true})
			w.Write([]byte("\n"))
			return
		}
		// Normal chat reply
		json.NewEncoder(w).Encode(ChatResponse{Message: Message{Role: "assistant", Content: "ok"}, Done: true})
		w.Write([]byte("\n"))
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	host := u.Hostname()
	port, _ := strconv.Atoi(u.Port())

	cfg := core.ServiceConfig{
		Name: "ollama",
		Config: map[string]interface{}{
			"host":  host,
			"port":  port,
			"model": "llama3.3",
		},
		Subs: map[string]core.ChannelInfo{
			"requests": {Name: "ollama-req"},
		},
		Pubs: map[string]core.ChannelInfo{
			"responses": {Name: "ollama-resp"},
		},
	}

	svc := NewService(deps, cfg).(*Service)
	// Preload history with 21 messages so summarization triggers (on next message)
	for i := range 21 {
		svc.Messages = append(svc.Messages, Message{Role: "user", Content: fmt.Sprintf("m%d", i)})
	}

	err := svc.messageHandler(core.Message{Text: "trigger"})
	if err != nil {
		t.Fatalf("messageHandler error: %v", err)
	}

	// After summarization, first message should be a system summary
	if len(svc.Messages) == 0 || svc.Messages[0].Role != "system" || !strings.Contains(svc.Messages[0].Content, "Summary of previous conversation:") {
		t.Fatalf("expected first message to be system summary, got %+v", svc.Messages[0])
	}
}

func TestService_ConfigParameters(t *testing.T) {
	deps := testDeps(t)
	messenger := deps.MustGetMessenger()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		var req ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		// Verify guidelines are present in the messages sent to Ollama
		foundGuidelines := false
		for _, m := range req.Messages {
			if m.Role == "system" && m.Content == "You are a helpful assistant." {
				foundGuidelines = true
				break
			}
		}
		if !foundGuidelines && !strings.Contains(req.Messages[0].Content, "summarize") {
			t.Errorf("Guidelines not found in request messages")
		}

		json.NewEncoder(w).Encode(ChatResponse{Message: Message{Role: "assistant", Content: "ok"}, Done: true})
		w.Write([]byte("\n"))
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	host := u.Hostname()
	port, _ := strconv.Atoi(u.Port())

	cfg := core.ServiceConfig{
		Name: "ollama-test",
		Config: map[string]interface{}{
			"host":          host,
			"port":          port,
			"model":         "llama3.3",
			"guidelines":    "You are a helpful assistant.",
			"highWaterMark": 5,
			"lowWaterMark":  2,
		},
		Subs: map[string]core.ChannelInfo{
			"requests": {Name: "ollama-req"},
		},
		Pubs: map[string]core.ChannelInfo{
			"responses": {Name: "ollama-resp"},
		},
	}

	svc := NewService(deps, cfg).(*Service)
	assert.Equal(t, 5, svc.HighWaterMark)
	assert.Equal(t, 2, svc.LowWaterMark)
	assert.Equal(t, "You are a helpful assistant.", svc.Guidelines)

	received := make(chan core.Message, 10)
	_ = messenger.Subscribe(context.Background(), "test", "ollama-resp", "ollama", "ollama", 0, func(m core.Message) error {
		received <- m
		return nil
	})

	// 1. Test guidelines
	err := svc.messageHandler(core.Message{Text: "Hello"})
	assert.NoError(t, err)

	// 2. Test Context Persistence (uses service name)
	state := deps.MustGetStateStore()
	var savedMessages []Message
	err = state.Load("ollama-test_history", &savedMessages)
	assert.NoError(t, err)
	assert.NotEmpty(t, savedMessages)

	// 3. Test Configurable Watermarks and Notification
	// Preload with 4 messages, next one will trigger HWM (5)
	svc.Mu.Lock()
	svc.Messages = []Message{
		{Role: "user", Content: "1"},
		{Role: "assistant", Content: "2"},
		{Role: "user", Content: "3"},
		{Role: "assistant", Content: "4"},
	}
	svc.Mu.Unlock()

	err = svc.messageHandler(core.Message{Text: "5"})
	assert.NoError(t, err)

	// Check for summarization notification
	foundNotification := false
	timeout := time.After(1 * time.Second)
L:
	for {
		select {
		case m := <-received:
			if m.Text == "Summarizing conversation history for ollama-test..." {
				foundNotification = true
				break L
			}
		case <-timeout:
			break L
		}
	}
	assert.True(t, foundNotification, "Summarization notification not received")
}

func TestService_DuplicateGuidelinesBug(t *testing.T) {
	deps := testDeps(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		json.NewEncoder(w).Encode(ChatResponse{Message: Message{Role: "assistant", Content: "ok"}, Done: true})
		w.Write([]byte("\n"))
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	host := u.Hostname()
	port, _ := strconv.Atoi(u.Port())

	cfg := core.ServiceConfig{
		Name: "ollama-test",
		Config: map[string]interface{}{
			"host":       host,
			"port":       port,
			"model":      "llama3.3",
			"guidelines": "Original guidelines",
		},
		Subs: map[string]core.ChannelInfo{
			"requests": {Name: "ollama-req"},
		},
		Pubs: map[string]core.ChannelInfo{
			"responses": {Name: "ollama-resp"},
		},
	}

	svc := NewService(deps, cfg).(*Service)

	// First request: should prepend guidelines
	err := svc.messageHandler(core.Message{Text: "Hello 1"})
	assert.NoError(t, err)

	// Second request: should NOT duplicate guidelines if they are already in svc.Messages
	err = svc.messageHandler(core.Message{Text: "Hello 2"})
	assert.NoError(t, err)

	// Count system messages in svc.Messages
	systemMsgCount := 0
	for _, m := range svc.Messages {
		if m.Role == "system" && m.Content == "Original guidelines" {
			systemMsgCount++
		}
	}
	assert.Equal(t, 1, systemMsgCount, "Guidelines should only appear once in history")

	// 3. Test Guideline Update: Change guidelines in config and send request
	svc.Guidelines = "Updated guidelines"
	err = svc.messageHandler(core.Message{Text: "Hello 3"})
	assert.NoError(t, err)

	updatedSystemMsgCount := 0
	foundUpdated := false
	for _, m := range svc.Messages {
		if m.Role == "system" {
			if m.Content == "Updated guidelines" {
				foundUpdated = true
			}
			updatedSystemMsgCount++
		}
	}
	assert.Equal(t, 1, updatedSystemMsgCount, "Should still only have one system message after update")
	assert.True(t, foundUpdated, "System message should have been updated to new guidelines")
}
