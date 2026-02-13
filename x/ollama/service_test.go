package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"keyop/core"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestService_Persistence(t *testing.T) {
	deps := testDeps(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req OllamaRequest
		json.NewDecoder(r.Body).Decode(&req)
		resp := OllamaResponse{
			Response: "OK",
			Done:     true,
			Context:  []int{1, 2, 3},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	var host string
	var port int
	fmt.Sscanf(ts.URL, "http://%s", &host)
	idx := strings.LastIndex(host, ":")
	if idx != -1 {
		fmt.Sscanf(host[idx+1:], "%d", &port)
		host = host[:idx]
	}

	cfg := core.ServiceConfig{
		Name: "ollama-persist",
		Config: map[string]interface{}{
			"host": host,
			"port": port,
		},
		Subs: map[string]core.ChannelInfo{
			"requests": {Name: "ollama-req"},
		},
		Pubs: map[string]core.ChannelInfo{
			"responses": {Name: "ollama-resp"},
		},
	}

	// First initialization and request to save context
	svc1 := NewService(deps, cfg).(*Service)
	err := svc1.messageHandler(core.Message{Text: "Save context"})
	assert.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3}, svc1.Context)

	// Second initialization - should load context
	svc2 := NewService(deps, cfg).(*Service)
	err = svc2.Initialize()
	assert.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3}, svc2.Context, "Context should be persisted")
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

func TestService_MessageHandler_Batching(t *testing.T) {
	deps := testDeps(t)
	messenger := deps.MustGetMessenger()

	// Mock Ollama server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/generate", r.URL.Path)
		w.Header().Set("Content-Type", "application/x-ndjson")

		var req OllamaRequest
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, "llama3.3", req.Model)

		responses := []OllamaResponse{
			{Response: "Hello ", Done: false},
			{Response: "world", Done: false},
			{Response: "!", Done: true, Context: []int{1, 2, 3}},
		}

		for _, resp := range responses {
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer ts.Close()

	// Extract host and port from test server
	var host string
	var port int
	fmt.Sscanf(ts.URL, "http://%s", &host)
	idx := strings.LastIndex(host, ":")
	if idx != -1 {
		fmt.Sscanf(host[idx+1:], "%d", &port)
		host = host[:idx]
	}

	cfg := core.ServiceConfig{
		Name: "ollama-test",
		Config: map[string]interface{}{
			"host":      host,
			"port":      port,
			"model":     "llama3.3",
			"batchSize": 10, // Small batch size to trigger multiple sends
		},
		Subs: map[string]core.ChannelInfo{
			"requests": {Name: "ollama-req"},
		},
		Pubs: map[string]core.ChannelInfo{
			"responses": {Name: "ollama-resp"},
		},
	}

	svc := NewService(deps, cfg).(*Service)

	receivedMessages := make(chan core.Message, 10)
	messenger.Subscribe(context.Background(), "test", "ollama-resp", 0, func(msg core.Message) error {
		receivedMessages <- msg
		return nil
	})

	// Trigger message handler
	msg := core.Message{
		ChannelName: "ollama-req",
		Text:        "Hi",
	}
	err := svc.messageHandler(msg)
	assert.NoError(t, err)

	// We expect "Hello world" to be the first batch (length 11 >= 10)
	// and "!" to be the second batch (sent at the end)

	select {
	case m1 := <-receivedMessages:
		assert.Equal(t, "Hello world", m1.Text)
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for first batch")
	}

	select {
	case m2 := <-receivedMessages:
		assert.Equal(t, "!", m2.Text)
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for second batch")
	}
}

func TestService_MessageHandler_Context(t *testing.T) {
	deps := testDeps(t)

	var capturedContexts [][]int
	var capturedPrompts []string
	// Mock Ollama server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")

		var req OllamaRequest
		json.NewDecoder(r.Body).Decode(&req)
		capturedContexts = append(capturedContexts, req.Context)
		capturedPrompts = append(capturedPrompts, req.Prompt)

		resp := OllamaResponse{
			Response: "Response to " + req.Prompt,
			Done:     true,
			Context:  []int{len(capturedPrompts)}, // Mock context
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	var host string
	var port int
	fmt.Sscanf(ts.URL, "http://%s", &host)
	idx := strings.LastIndex(host, ":")
	if idx != -1 {
		fmt.Sscanf(host[idx+1:], "%d", &port)
		host = host[:idx]
	}

	cfg := core.ServiceConfig{
		Name: "ollama-test",
		Config: map[string]interface{}{
			"host": host,
			"port": port,
		},
		Subs: map[string]core.ChannelInfo{
			"requests": {Name: "ollama-req"},
		},
		Pubs: map[string]core.ChannelInfo{
			"responses": {Name: "ollama-resp"},
		},
	}

	svc := NewService(deps, cfg).(*Service)

	// First request
	err := svc.messageHandler(core.Message{Text: "First"})
	assert.NoError(t, err)
	assert.Len(t, capturedPrompts, 1)
	assert.Equal(t, "First", capturedPrompts[0])
	assert.Nil(t, capturedContexts[0])

	// Second request - should include context from first response
	err = svc.messageHandler(core.Message{Text: "Second"})
	assert.NoError(t, err)
	assert.Len(t, capturedPrompts, 2)
	assert.Equal(t, "Second", capturedPrompts[1])
	assert.Equal(t, []int{1}, capturedContexts[1])
}
