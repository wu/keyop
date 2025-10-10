package heartbeat

import (
	"bytes"
	"encoding/json"
	"keyop/core"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type logMsg struct {
	Time      string `json:"time"`
	Level     string `json:"level"`
	Msg       string `json:"msg"`
	Heartbeat Event  `json:"data"`
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

func TestHeartbeatCmd(t *testing.T) {

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	deps := core.Dependencies{Logger: logger, Hostname: "test-host"}

	cmd := NewCmd(deps)

	err := cmd.Execute()
	assert.NoError(t, err, "Execute() error = %v, want nil", err)

	messages, err := parseLogMessages(buf.String())
	assert.NoError(t, err, "parseLogMessages() error = %v, want nil", err)

	assert.Equal(t, 1, len(messages), "expected 1 log message")
	assert.Equal(t, "heartbeat", messages[0].Msg, "expected heartbeat message")
	assert.Equal(t, "INFO", messages[0].Level, "expected INFO level")

	uptime := time.Since(startTime).Round(time.Second)
	assert.True(t, messages[0].Heartbeat.UptimeSeconds >= 0, "uptime seconds is 0 or greater")
	assert.True(t, messages[0].Heartbeat.UptimeSeconds < int64(uptime.Seconds()+5), "approximate uptime seconds")
	assert.True(t, messages[0].Heartbeat.UptimeSeconds > int64(uptime.Seconds()-5), "approximate uptime seconds")

	assert.Equal(t, messages[0].Heartbeat.Hostname, "test-host", "shortHostname should be present in heartbeat message")

}
