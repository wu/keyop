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
	osProvider := core.FakeOsProvider{Host: "test-host"}
	deps := core.Dependencies{}
	deps.SetOsProvider(osProvider)
	deps.SetLogger(logger)
	deps.SetMessenger(core.NewMessenger(logger, osProvider))

	cmd := NewCmd(deps)

	err := cmd.Execute()
	assert.NoError(t, err, "Execute() error = %v, want nil", err)

	messages, err := parseLogMessages(buf.String())
	assert.NoError(t, err, "parseLogMessages() error = %v, want nil", err)

	// iterate through messages searching for one with Msg "heartbeat"
	var heartbeatFound bool
	var heartbeatMsg logMsg
	for _, msg := range messages {
		if msg.Msg == "heartbeat" {
			heartbeatFound = true
			heartbeatMsg = msg
			break
		}
	}

	assert.True(t, heartbeatFound, "expected to find a heartbeat log message")

	assert.Equal(t, "INFO", heartbeatMsg.Level, "expected INFO level")

	uptime := time.Since(startTime).Round(time.Second)
	assert.True(t, heartbeatMsg.Heartbeat.UptimeSeconds >= 0, "uptime seconds is 0 or greater")
	assert.True(t, heartbeatMsg.Heartbeat.UptimeSeconds < int64(uptime.Seconds()+5), "approximate uptime seconds")
	assert.True(t, heartbeatMsg.Heartbeat.UptimeSeconds > int64(uptime.Seconds()-5), "approximate uptime seconds")

	assert.Equal(t, heartbeatMsg.Heartbeat.Hostname, "test-host", "shortHostname should be present in heartbeat message")

}

func TestValidateConfig_EmptyConfig(t *testing.T) {
	svc := &Service{
		Cfg: core.ServiceConfig{},
	}
	svc.validateConfig()

	assert.Equal(t, "heartbeat", svc.Cfg.Name)
	assert.Equal(t, "heartbeat", svc.Cfg.Type)
	assert.NotNil(t, svc.Cfg.Pubs)
	assert.Contains(t, svc.Cfg.Pubs, "events")
	assert.Equal(t, "events", svc.Cfg.Pubs["events"].Name)
	assert.Equal(t, "General event channel", svc.Cfg.Pubs["events"].Description)
}

func TestValidateConfig_ExistingValues(t *testing.T) {
	svc := &Service{
		Cfg: core.ServiceConfig{
			Name: "custom",
			Type: "special",
			Pubs: map[string]core.ChannelInfo{
				"events": {
					Name:        "custom-events",
					Description: "Custom channel",
				},
			},
		},
	}
	svc.validateConfig()

	assert.Equal(t, "custom", svc.Cfg.Name)
	assert.Equal(t, "special", svc.Cfg.Type)
	assert.Equal(t, "custom-events", svc.Cfg.Pubs["events"].Name)
	assert.Equal(t, "Custom channel", svc.Cfg.Pubs["events"].Description)
}
