package heartbeat

import (
	"keyop/core"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

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
func (m *mockMessenger) Subscribe(sourceName string, channelName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
	return nil
}

func TestHeartbeatMetricName(t *testing.T) {
	deps := core.Dependencies{}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps.SetLogger(logger)
	deps.SetOsProvider(core.OsProvider{})

	t.Run("no metricName", func(t *testing.T) {
		messenger := &mockMessenger{}
		deps.SetMessenger(messenger)
		cfg := core.ServiceConfig{
			Name: "hb-service",
			Type: "heartbeat",
			Pubs: map[string]core.ChannelInfo{
				"events":  {Name: "events-topic"},
				"metrics": {Name: "metrics-topic"},
				"errors":  {Name: "errors-topic"},
				"alerts":  {Name: "alerts-topic"},
			},
		}
		svc := NewService(deps, cfg).(Service)
		err := svc.Check()
		assert.NoError(t, err)

		assert.Len(t, messenger.messages, 3)
		foundMetric := false
		for _, msg := range messenger.messages {
			if msg.ChannelName == "metrics-topic" {
				assert.Equal(t, "hb-service", msg.MetricName)
				foundMetric = true
			}
		}
		if !foundMetric {
			t.Error("expected to find a message sent to metrics-topic")
		}
	})

	t.Run("with metricName", func(t *testing.T) {
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
				"metricName": "custom.heartbeat.name",
			},
		}
		svc := NewService(deps, cfg).(Service)
		err := svc.Check()
		assert.NoError(t, err)

		assert.Len(t, messenger.messages, 2)
		for _, msg := range messenger.messages {
			assert.Equal(t, "custom.heartbeat.name", msg.MetricName)
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
				"alerts":  {Name: "alerts-topic"},
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
