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

}

func TestValidateConfig(t *testing.T) {
	makeSvc := func(cfg core.ServiceConfig) Service {
		return Service{Cfg: cfg}
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
		errs := svc.validateConfig()
		assert.Len(t, errs, 0)
	})

	t.Run("missing name", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Name: "",
			Type: "heartbeat",
			Pubs: map[string]core.ChannelInfo{
				"events": {Name: "events-topic"},
			},
		}
		errs := makeSvc(cfg).validateConfig()
		assert.NotEmpty(t, errs)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "required field 'name' is empty") {
				found = true
			}
		}
		assert.True(t, found, "expected missing name error")
	})

	t.Run("missing type", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Name: "hb",
			Type: "",
			Pubs: map[string]core.ChannelInfo{
				"events": {Name: "events-topic"},
			},
		}
		errs := makeSvc(cfg).validateConfig()
		assert.NotEmpty(t, errs)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "required field 'type' is empty") {
				found = true
			}
		}
		assert.True(t, found, "expected missing type error")
	})

	t.Run("nil pubs", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Name: "hb",
			Type: "heartbeat",
			Pubs: nil,
		}
		errs := makeSvc(cfg).validateConfig()
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
		errs := makeSvc(cfg).validateConfig()
		assert.NotEmpty(t, errs)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "required publish channel 'events' is missing") {
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
		errs := makeSvc(cfg).validateConfig()
		assert.NotEmpty(t, errs)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "required publish channel 'events' is missing a name") {
				found = true
			}
		}
		assert.True(t, found, "expected events channel missing name error")
	})
}
