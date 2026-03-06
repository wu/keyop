//nolint:revive
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMessenger_DeadLetterQueue(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_dlq_test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove %v: %v", tmpDir, err)
		}
	})

	logger := &FakeLogger{}
	osProvider := &OsProvider{}
	m := NewMessenger(logger, osProvider)
	m.SetDataDir(tmpDir)
	// Set low max retries for fast test
	m.maxRetryAttempts = 2

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	channel := "test-dlq-chan"
	dlqChannel := "_dlq." + channel
	source := "test-subscriber"

	failCount := 0
	require.NoError(t, m.Subscribe(ctx, source, channel, "test", "test", 0, func(msg Message) error {
		failCount++
		return errors.New("permanent failure")
	}))

	// Send message
	msg := Message{
		ChannelName: channel,
		Text:        "fail me",
	}
	if err := m.Send(msg); err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	// Wait for message to hit DLQ
	// 1 initial try + 2 retries = 3 attempts total. Then DLQ.
	dlqReceived := make(chan Message, 1)
	require.NoError(t, m.Subscribe(ctx, "dlq-reader", dlqChannel, "test", "dlq-reader", 0, func(dlqMsg Message) error {
		dlqReceived <- dlqMsg
		return nil
	}))

	select {
	case dlqMsg := <-dlqReceived:
		if dlqMsg.Text != "fail me" {
			t.Errorf("Expected DLQ message text 'fail me', got '%s'", dlqMsg.Text)
		}
		// Check for DLQ headers in some way if possible, or just verify it's there
		// Since ToMessage maps metadata, we can check for trace or just the fact it arrived.
	case <-ctx.Done():
		t.Fatalf("Timed out waiting for DLQ message. Fail count was %d", failCount)
	}

	if failCount != 3 {
		t.Errorf("Expected 3 failed attempts (1 initial + 2 retries), got %d", failCount)
	}
}

func TestMessenger_BackwardCompatibility(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "messenger_compat_test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove %v: %v", tmpDir, err)
		}
	})

	logger := &FakeLogger{}
	osProvider := &OsProvider{}
	m := NewMessenger(logger, osProvider)
	m.SetDataDir(tmpDir)

	channel := "compat-test"

	// Manually write a legacy message (raw JSON of Message struct) to the queue file
	dateStr := time.Now().Format("20060102")
	fileName := filepath.Join(tmpDir, fmt.Sprintf("%s_queue_%s.log", channel, dateStr))
	if err := os.MkdirAll(tmpDir, 0750); err != nil { //nolint:gosec // restrict permissions for test directory
		t.Fatal(err)
	}

	legacyMsg := Message{
		Uuid:        "legacy-uuid",
		ChannelName: channel,
		Text:        "I am legacy",
		Timestamp:   time.Now().Add(-time.Minute),
	}
	msgBytes, _ := json.Marshal(legacyMsg)
	if err := os.WriteFile(fileName, append(msgBytes, '\n'), 0600); err != nil { //nolint:gosec // restrict permissions for test file
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	received := make(chan Message, 1)
	require.NoError(t, m.Subscribe(ctx, "compat-sub", channel, "test", "test", 0, func(msg Message) error {
		received <- msg
		return nil
	}))

	select {
	case msg := <-received:
		if msg.Uuid != "legacy-uuid" {
			t.Errorf("Expected UUID 'legacy-uuid', got '%s'", msg.Uuid)
		}
		if msg.Text != "I am legacy" {
			t.Errorf("Expected text 'I am legacy', got '%s'", msg.Text)
		}
	case <-ctx.Done():
		t.Fatal("Timed out waiting for legacy message")
	}
}

func TestEnvelope_ToMessage_Mapping(t *testing.T) {
	now := time.Now()
	env := Envelope{
		Version:   EnvelopeV1,
		ID:        "env-id",
		Timestamp: now,
		Topic:     "test-topic",
		Source:    "test-source",
		Payload: Message{
			Text:   "inner-text",
			Status: "inner-status",
		},
	}

	msg := env.ToMessage()
	if msg.Uuid != "env-id" {
		t.Errorf("Expected UUID 'env-id', got '%s'", msg.Uuid)
	}
	if msg.Text != "inner-text" {
		t.Errorf("Expected text 'inner-text', got '%s'", msg.Text)
	}
	if msg.Timestamp != now {
		t.Errorf("Expected timestamp %v, got %v", now, msg.Timestamp)
	}
}
