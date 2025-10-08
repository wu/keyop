package heartbeat

import (
	"bytes"
	"encoding/json"
	"keyop/core"
	"log/slog"
	"strings"
	"testing"
)

type logMsg struct {
	Time  string `json:"time"`
	Level string `json:"level"`
	Msg   string `json:"msg"`
}

func parseLogMessages(logs string) ([]logMsg, error) {
	var messages []logMsg
	lines := strings.Split(strings.TrimSpace(logs), "\n")

	for _, line := range lines {
		var msg logMsg
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

func TestHeartbeat_RunE_LogsAndReturnsNil(t *testing.T) {

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	deps := core.Dependencies{Logger: logger}

	cmd := NewHeartbeatCmd(deps)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	messages, err := parseLogMessages(buf.String())
	if err != nil {
		t.Fatalf("failed to parse log messages: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("expected 1 info log, got %d", len(messages))
	}

	if messages[0].Msg != "heartbeat called" {
		t.Fatalf("unexpected log message: got %q, want %q", messages[0].Msg, "heartbeat called")
	}
}
