package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"keyop/core"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDeps(t *testing.T) core.Dependencies {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}

	tmpDir, err := os.MkdirTemp("", "slack_test")
	require.NoError(t, err)
	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	osProvider := core.OsProvider{}
	deps.SetOsProvider(osProvider)
	deps.SetLogger(logger)

	messenger := core.NewMessenger(logger, deps.MustGetOsProvider())
	messenger.SetDataDir(tmpDir)
	deps.SetMessenger(messenger)

	stateStore := core.NewFileStateStore(tmpDir, osProvider)
	deps.SetStateStore(stateStore)

	ctx, cancel := context.WithCancel(context.Background())
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	return deps
}

func TestService_ValidateConfig(t *testing.T) {
	deps := testDeps(t)

	tests := []struct {
		name        string
		pubs        map[string]core.ChannelInfo
		subs        map[string]core.ChannelInfo
		config      map[string]interface{}
		expectError bool
	}{
		{
			name: "valid config",
			pubs: map[string]core.ChannelInfo{"alerts": {Name: "a"}},
			subs: map[string]core.ChannelInfo{"alerts": {Name: "a"}},
			config: map[string]interface{}{
				"token":     "xoxb-test",
				"appToken":  "xapp-test",
				"channelID": "C12345",
				"appID":     "A123",
			},
			expectError: false,
		},
		{
			name: "missing appToken",
			pubs: map[string]core.ChannelInfo{"alerts": {Name: "a"}},
			subs: map[string]core.ChannelInfo{"alerts": {Name: "a"}},
			config: map[string]interface{}{
				"token":     "xoxb-test",
				"channelID": "C12345",
			},
			expectError: true,
		},
		{
			name: "missing token",
			pubs: map[string]core.ChannelInfo{"alerts": {Name: "a"}},
			subs: map[string]core.ChannelInfo{"alerts": {Name: "a"}},
			config: map[string]interface{}{
				"channelID": "C12345",
			},
			expectError: true,
		},
		{
			name: "missing channelID",
			pubs: map[string]core.ChannelInfo{"alerts": {Name: "a"}},
			subs: map[string]core.ChannelInfo{"alerts": {Name: "a"}},
			config: map[string]interface{}{
				"token": "xoxb-test",
			},
			expectError: true,
		},
		{
			name: "missing pubs",
			pubs: map[string]core.ChannelInfo{},
			subs: map[string]core.ChannelInfo{"alerts": {Name: "a"}},
			config: map[string]interface{}{
				"token":     "xoxb-test",
				"channelID": "C12345",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := core.ServiceConfig{
				Pubs:   tt.pubs,
				Subs:   tt.subs,
				Config: tt.config,
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

func TestService_MessageHandler(t *testing.T) {
	deps := testDeps(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/chat.postMessage", r.URL.Path)
		assert.Equal(t, "Bearer xoxb-test", r.Header.Get("Authorization"))

		var payload map[string]interface{}
		json.NewDecoder(r.Body).Decode(&payload)
		assert.Equal(t, "C12345", payload["channel"])
		assert.Equal(t, "hello slack", payload["text"])

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok": true}`)
	}))
	defer server.Close()

	cfg := core.ServiceConfig{
		Name: "slack-test",
		Subs: map[string]core.ChannelInfo{"alerts": {Name: "alerts-ch"}},
		Config: map[string]interface{}{
			"token":     "xoxb-test",
			"appToken":  "xapp-test",
			"channelID": "C12345",
			"appID":     "A123",
		},
	}
	svc := NewService(deps, cfg).(*Service)
	svc.Initialize()
	svc.BaseURL = server.URL

	msg := core.Message{
		Text:        "hello slack",
		ServiceName: "other-service",
	}
	err := svc.messageHandler(msg)
	assert.NoError(t, err)
}

func TestService_Check(t *testing.T) {
	// This test will simulate the Socket Mode flow
	deps := testDeps(t)
	// We need a context that we can cancel to stop the Check() loop
	ctx, cancel := context.WithCancel(context.Background())
	deps.SetContext(ctx)

	// 1. Mock Slack API for apps.connections.open
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apps.connections.open" {
			assert.Equal(t, "POST", r.Method)
			assert.Equal(t, "Bearer xapp-test", r.Header.Get("Authorization"))

			// We need a WebSocket URL. We'll use another mock server for that.
			w.Header().Set("Content-Type", "application/json")
			// We can't easily know the ws:// URL of the other server yet if we start it here.
			// Actually we can start the WS server first.
		}
	}))
	defer server.Close()

	// 2. Mock WebSocket server
	upgrader := websocket.Upgrader{}
	wsReceived := make(chan string, 10)
	var wsConns []*websocket.Conn
	var wsMu sync.Mutex

	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)

		wsMu.Lock()
		wsConns = append(wsConns, conn)
		wsMu.Unlock()

		// Send hello
		conn.WriteJSON(map[string]interface{}{"type": "hello"})

		// Wait for a message from the client (the ack) or context cancel
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var envelope struct {
				Type       string          `json:"type"`
				EnvelopeID string          `json:"envelope_id"`
				Payload    json.RawMessage `json:"payload"`
			}
			json.Unmarshal(msg, &envelope)
			if envelope.EnvelopeID != "" {
				wsReceived <- envelope.EnvelopeID
			}

			// If we receive an events_api from our test client, broadcast it to other connections
			if envelope.Type == "events_api" {
				wsMu.Lock()
				for _, other := range wsConns {
					if other != conn {
						other.WriteMessage(websocket.TextMessage, msg)
					}
				}
				wsMu.Unlock()
			}
		}
	}))
	defer wsServer.Close()

	// Re-mock apps.connections.open with the correct WS URL
	wsURL := "ws" + wsServer.URL[4:] // Convert http:// to ws://

	// Create a new server because we need wsURL
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/apps.connections.open" {
			fmt.Fprintf(w, `{"ok": true, "url": "%s"}`, wsURL)
		} else if r.URL.Path == "/conversations.info" {
			assert.Equal(t, "GET", r.Method)
			assert.Equal(t, "Bearer xoxb-test", r.Header.Get("Authorization"))
			channelID := r.URL.Query().Get("channel")
			if channelID == "C999" {
				fmt.Fprint(w, `{"ok": true, "channel": {"name": "general"}}`)
			} else {
				fmt.Fprint(w, `{"ok": false, "error": "channel_not_found"}`)
			}
		} else if r.URL.Path == "/users.info" {
			assert.Equal(t, "GET", r.Method)
			assert.Equal(t, "Bearer xoxb-test", r.Header.Get("Authorization"))
			userID := r.URL.Query().Get("user")
			if userID == "U123" {
				fmt.Fprint(w, `{"ok": true, "user": {"name": "jdoe", "real_name": "John Doe"}}`)
			} else {
				fmt.Fprint(w, `{"ok": false, "error": "user_not_found"}`)
			}
		}
	}))
	defer apiServer.Close()

	cfg := core.ServiceConfig{
		Name: "slack-test",
		Pubs: map[string]core.ChannelInfo{"alerts": {Name: "alerts-ch"}},
		Config: map[string]interface{}{
			"token":     "xoxb-test",
			"appToken":  "xapp-test",
			"channelID": "C12345",
			"appID":     "A123",
		},
	}
	svc := NewService(deps, cfg).(*Service)
	svc.Initialize()
	svc.BaseURL = apiServer.URL

	receivedMessage := make(chan string, 1)
	messenger := deps.MustGetMessenger()
	messenger.Subscribe("test-subscriber", "alerts-ch", 0, func(msg core.Message) error {
		receivedMessage <- msg.Text
		return nil
	})

	// Run Check in a goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.Check()
	}()

	// Give it a moment to connect
	time.Sleep(100 * time.Millisecond)

	// Send a message through the WebSocket
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	payload := map[string]interface{}{
		"type":        "events_api",
		"envelope_id": "env123",
		"payload": map[string]interface{}{
			"event": map[string]interface{}{
				"type":    "message",
				"user":    "U123",
				"text":    "hello from socket mode",
				"ts":      "1707159300.000000",
				"channel": "C999",
			},
		},
	}
	err = conn.WriteJSON(payload)
	require.NoError(t, err)

	// Wait for the WS server to receive the ack
	select {
	case envID := <-wsReceived:
		assert.Equal(t, "env123", envID)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for message ack")
	}

	// Verify the message was sent to the alerts channel with resolved names
	select {
	case text := <-receivedMessage:
		assert.Equal(t, "Slack [#general] [John Doe]: hello from socket mode", text)
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for message on alerts channel")
	}

	// Stop the service
	cancel()
	err = <-errCh
	assert.ErrorIs(t, err, context.Canceled)
}
