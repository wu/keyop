package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestClient_Chat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("Expected path /api/chat, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/x-ndjson")
		resp1 := ChatResponse{
			Message: Message{Role: "assistant", Content: "Hello"},
			Done:    false,
		}
		resp2 := ChatResponse{
			Message: Message{Role: "assistant", Content: " world!"},
			Done:    true,
		}
		if err := json.NewEncoder(w).Encode(resp1); err != nil {
			t.Fatalf("failed to encode resp1: %v", err)
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
		if err := json.NewEncoder(w).Encode(resp2); err != nil {
			t.Fatalf("failed to encode resp2: %v", err)
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	// Parse host and port from server URL. Use url.Parse for robustness.
	u, _ := url.Parse(server.URL)
	host := "127.0.0.1"
	port := 0
	if u != nil {
		host = u.Hostname()
		if p := u.Port(); p != "" {
			if p2, err := strconv.Atoi(p); err == nil {
				port = p2
			}
		}
	}

	client := NewClient(host, port, 1*time.Second, true)
	messages := []Message{{Role: "user", Content: "Hi"}}

	var received string
	updated, err := client.Chat(context.Background(), "test-model", messages, func(s string) error {
		received += s
		return nil
	})

	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if received != "Hello world!" {
		t.Errorf("Expected 'Hello world!', got '%s'", received)
	}

	if len(updated) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(updated))
	}
	if updated[1].Content != "Hello world!" {
		t.Errorf("Expected last message content 'Hello world!', got '%s'", updated[1].Content)
	}
}

func TestClient_Summarize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		resp := ChatResponse{
			Message: Message{Role: "assistant", Content: "This is a summary."},
			Done:    true,
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("failed to encode resp: %v", err)
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	u, _ := url.Parse(server.URL)
	host := "127.0.0.1"
	port := 0
	if u != nil {
		host = u.Hostname()
		if p := u.Port(); p != "" {
			if p2, err := strconv.Atoi(p); err == nil {
				port = p2
			}
		}
	}

	client := NewClient(host, port, 1*time.Second, true)
	messages := []Message{{Role: "user", Content: "Talk 1"}, {Role: "assistant", Content: "Reply 1"}}

	summary, err := client.Summarize(context.Background(), "test-model", messages)
	if err != nil {
		t.Fatalf("Summarize failed: %v", err)
	}

	expectedPrefix := "Summary of previous conversation: "
	if summary.Content != expectedPrefix+"This is a summary." {
		t.Errorf("Unexpected summary content: %s", summary.Content)
	}
	if summary.Role != "system" {
		t.Errorf("Expected role system, got %s", summary.Role)
	}
}

func TestClient_Chat_NoStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := ChatResponse{
			Message: Message{Role: "assistant", Content: "Hello world!"},
			Done:    true,
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("failed to encode resp: %v", err)
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	u, _ := url.Parse(server.URL)
	host := "127.0.0.1"
	port := 0
	if u != nil {
		host = u.Hostname()
		if p := u.Port(); p != "" {
			if p2, err := strconv.Atoi(p); err == nil {
				port = p2
			}
		}
	}

	client := NewClient(host, port, 1*time.Second, false)
	messages := []Message{{Role: "user", Content: "Hi"}}

	var received string
	updated, err := client.Chat(context.Background(), "test-model", messages, func(s string) error {
		received += s
		return nil
	})

	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if received != "Hello world!" {
		t.Errorf("Expected 'Hello world!', got '%s'", received)
	}

	if len(updated) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(updated))
	}
}

func TestClient_Chat_NoStream_NoNewline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := ChatResponse{
			Message: Message{Role: "assistant", Content: "Hello world!"},
			Done:    true,
		}
		data, _ := json.Marshal(resp)
		if _, err := w.Write(data); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
		// No trailing newline specifically
	}))
	t.Cleanup(server.Close)

	u, _ := url.Parse(server.URL)
	host := "127.0.0.1"
	port := 0
	if u != nil {
		host = u.Hostname()
		if p := u.Port(); p != "" {
			if p2, err := strconv.Atoi(p); err == nil {
				port = p2
			}
		}
	}

	client := NewClient(host, port, 1*time.Second, false)
	messages := []Message{{Role: "user", Content: "Hi"}}

	var received string
	updated, err := client.Chat(context.Background(), "test-model", messages, func(s string) error {
		received += s
		return nil
	})

	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if received != "Hello world!" {
		t.Errorf("Expected 'Hello world!', got '%s'", received)
	}

	if len(updated) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(updated))
	}
}

func TestClient_Chat_Stream_NoNewline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		resp1 := ChatResponse{
			Message: Message{Role: "assistant", Content: "Hello"},
			Done:    false,
		}
		resp2 := ChatResponse{
			Message: Message{Role: "assistant", Content: " world!"},
			Done:    true,
		}
		if err := json.NewEncoder(w).Encode(resp1); err != nil {
			t.Fatalf("failed to encode resp: %v", err)
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
		data, _ := json.Marshal(resp2)
		if _, err := w.Write(data); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
		// NO NEWLINE after second part
	}))
	t.Cleanup(server.Close)

	u, _ := url.Parse(server.URL)
	host := "127.0.0.1"
	port := 0
	if u != nil {
		host = u.Hostname()
		if p := u.Port(); p != "" {
			if p2, err := strconv.Atoi(p); err == nil {
				port = p2
			}
		}
	}

	client := NewClient(host, port, 1*time.Second, true)
	messages := []Message{{Role: "user", Content: "Hi"}}

	var received string
	updated, err := client.Chat(context.Background(), "test-model", messages, func(s string) error {
		received += s
		return nil
	})

	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if received != "Hello world!" {
		t.Errorf("Expected 'Hello world!', got '%s'", received)
	}

	if len(updated) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(updated))
	}
}
