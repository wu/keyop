package heartbeat

import (
	"bytes"
	"encoding/json"
	"keyop/core"
	"log/slog"
	"os"
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

}

func TestValidateConfig(t *testing.T) {
	makeSvc := func(cfg core.ServiceConfig) Service {
		logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		deps := core.Dependencies{}
		deps.SetLogger(logger)
		return Service{Cfg: cfg, Deps: deps}
	}

	t.Run("valid config", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Name: "hb",
			Type: "heartbeat",
			Pubs: map[string]core.ChannelInfo{
				"events": {Name: "events-topic"},
			},
		}
		svc := makeSvc(cfg)
		errs := svc.ValidateConfig()
		assert.Len(t, errs, 0)
	})

	t.Run("nil pubs", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Name: "hb",
			Type: "heartbeat",
			Pubs: nil,
		}
		errs := makeSvc(cfg).ValidateConfig()
		assert.NotEmpty(t, errs)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "required field 'pubs' is empty") {
				found = true
			}
		}
		assert.True(t, found, "expected nil pubs error")
	})

	t.Run("missing events channel", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Name: "hb",
			Type: "heartbeat",
			Pubs: map[string]core.ChannelInfo{
				"other": {Name: "other"},
			},
		}
		errs := makeSvc(cfg).ValidateConfig()
		assert.NotEmpty(t, errs)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "required pubs channel 'events' is missing") {
				found = true
			}
		}
		assert.True(t, found, "expected missing events channel error")
	})

	t.Run("events channel missing name", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Name: "hb",
			Type: "heartbeat",
			Pubs: map[string]core.ChannelInfo{
				"events": {Name: ""},
			},
		}
		errs := makeSvc(cfg).ValidateConfig()
		assert.NotEmpty(t, errs)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "required pubs channel 'events' is missing a name") {
				found = true
			}
		}
		assert.True(t, found, "expected events channel missing name error")
	})
}
