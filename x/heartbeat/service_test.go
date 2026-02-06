package heartbeat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"keyop/core"
	"log/slog"
	"os"
	"strings"
	"sync"
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
	opts := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	logger := slog.New(slog.NewJSONHandler(&buf, opts))

	osProvider := core.OsProvider{}
	deps := core.Dependencies{}
	deps.SetOsProvider(osProvider)
	deps.SetLogger(logger)
	messenger := core.NewMessenger(logger, osProvider)
	tmpDir := t.TempDir()
	messenger.SetDataDir(tmpDir)
	deps.SetMessenger(messenger)

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

	assert.Equal(t, "DEBUG", heartbeatMsg.Level, "expected DEBUG level")

	uptime := time.Since(startTime).Round(time.Second)
	assert.True(t, heartbeatMsg.Heartbeat.UptimeSeconds >= 0, "uptime seconds is 0 or greater")
	assert.True(t, heartbeatMsg.Heartbeat.UptimeSeconds < int64(uptime.Seconds()+5), "approximate uptime seconds")
	assert.True(t, heartbeatMsg.Heartbeat.UptimeSeconds > int64(uptime.Seconds()-5), "approximate uptime seconds")

}

type mockMessenger struct {
	messages []core.Message
	mu       sync.Mutex
}

func (m *mockMessenger) Send(msg core.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	return nil
}

//goland:noinspection GoUnusedParameter
func (m *mockMessenger) Subscribe(sourceName string, channelName string, messageHandler func(core.Message) error) error {
	return nil
}

func TestHeartbeatMetricName(t *testing.T) {
	deps := core.Dependencies{}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps.SetLogger(logger)

	t.Run("no metricPrefix", func(t *testing.T) {
		messenger := &mockMessenger{}
		deps.SetMessenger(messenger)
		cfg := core.ServiceConfig{
			Name: "hb-service",
			Type: "heartbeat",
			Pubs: map[string]core.ChannelInfo{
				"events":  {Name: "events-topic"},
				"metrics": {Name: "metrics-topic"},
				"errors":  {Name: "errors-topic"},
			},
		}
		svc := NewService(deps, cfg).(Service)
		err := svc.Check()
		assert.NoError(t, err)

		assert.Len(t, messenger.messages, 2)
		for _, msg := range messenger.messages {
			fmt.Printf("Checking heartbeat metric for service %s: %v\n", svc.Cfg.Name, msg)
			assert.Equal(t, "hb-service", msg.MetricName)
		}
	})

	t.Run("with metricPrefix", func(t *testing.T) {
		messenger := &mockMessenger{}
		deps.SetMessenger(messenger)
		cfg := core.ServiceConfig{
			Name: "hb-service",
			Type: "heartbeat",
			Pubs: map[string]core.ChannelInfo{
				"events":  {Name: "events-topic"},
				"metrics": {Name: "metrics-topic"},
				"errors":  {Name: "errors-topic"},
			},
			Config: map[string]interface{}{
				"metricPrefix": "env.prod.",
			},
		}
		svc := NewService(deps, cfg).(Service)
		err := svc.Check()
		assert.NoError(t, err)

		assert.Len(t, messenger.messages, 2)
		for _, msg := range messenger.messages {
			assert.Equal(t, "env.prod.hb-service", msg.MetricName)
		}
	})
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
				"events":  {Name: "events-topic"},
				"metrics": {Name: "metrics-topic"},
				"errors":  {Name: "errors-topic"},
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
