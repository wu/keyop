package pingMonitor

import (
	"errors"
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

func (m *mockMessenger) Subscribe(sourceName string, channelName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
	return nil
}

func TestCheck(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	t.Run("successful ping", func(t *testing.T) {
		deps := core.Dependencies{}
		deps.SetLogger(logger)
		messenger := &mockMessenger{}
		deps.SetMessenger(messenger)

		fakeOs := &core.FakeOsProvider{
			CommandFunc: func(name string, arg ...string) core.CommandApi {
				return &core.FakeCommand{
					CombinedOutputFunc: func() ([]byte, error) {
						return []byte("64 bytes from 8.8.8.8: icmp_seq=1 ttl=117 time=12.3 ms"), nil
					},
				}
			},
		}
		deps.SetOsProvider(fakeOs)

		cfg := core.ServiceConfig{
			Name: "net-mon",
			Type: "pingMonitor",
			Pubs: map[string]core.ChannelInfo{
				"status":  {Name: "status-topic"},
				"metrics": {Name: "metrics-topic"},
			},
			Config: map[string]interface{}{
				"host": "google.com",
			},
		}
		svc := NewService(deps, cfg)
		err := svc.Check()
		assert.NoError(t, err)

		assert.Len(t, messenger.messages, 2)

		foundEvent := false
		foundMetric := false
		for _, msg := range messenger.messages {
			if msg.ChannelName == "status-topic" {
				foundEvent = true
				assert.Contains(t, msg.Text, "successful")
				assert.Contains(t, msg.Text, "12.3")
			}
			if msg.ChannelName == "metrics-topic" {
				foundMetric = true
				assert.Equal(t, 12.3, msg.Metric)
				assert.Equal(t, "net-mon.ping_time", msg.MetricName)
			}
		}
		assert.True(t, foundEvent, "Expected status message")
		assert.True(t, foundMetric, "Expected metrics message")
	})

	t.Run("successful ping with custom metric name", func(t *testing.T) {
		deps := core.Dependencies{}
		deps.SetLogger(logger)
		messenger := &mockMessenger{}
		deps.SetMessenger(messenger)

		fakeOs := &core.FakeOsProvider{
			CommandFunc: func(name string, arg ...string) core.CommandApi {
				return &core.FakeCommand{
					CombinedOutputFunc: func() ([]byte, error) {
						return []byte("64 bytes from 8.8.8.8: icmp_seq=1 ttl=117 time=12.3 ms"), nil
					},
				}
			},
		}
		deps.SetOsProvider(fakeOs)

		cfg := core.ServiceConfig{
			Name: "net-mon",
			Type: "pingMonitor",
			Pubs: map[string]core.ChannelInfo{
				"status":  {Name: "status-topic"},
				"metrics": {Name: "metrics-topic"},
			},
			Config: map[string]interface{}{
				"host":        "google.com",
				"metric_name": "custom.ping.latency",
			},
		}
		svc := NewService(deps, cfg)
		err := svc.Check()
		assert.NoError(t, err)

		foundMetric := false
		for _, msg := range messenger.messages {
			if msg.ChannelName == "metrics-topic" {
				foundMetric = true
				assert.Equal(t, 12.3, msg.Metric)
				assert.Equal(t, "custom.ping.latency", msg.MetricName)
			}
		}
		assert.True(t, foundMetric, "Expected metrics message with custom name")
	})

	t.Run("failed ping sets status", func(t *testing.T) {
		deps := core.Dependencies{}
		deps.SetLogger(logger)
		messenger := &mockMessenger{}
		deps.SetMessenger(messenger)

		fakeOs := &core.FakeOsProvider{
			CommandFunc: func(name string, arg ...string) core.CommandApi {
				return &core.FakeCommand{
					CombinedOutputFunc: func() ([]byte, error) {
						return []byte("ping: cannot resolve unreachable.host: Unknown host"), errors.New("ping failed")
					},
				}
			},
		}
		deps.SetOsProvider(fakeOs)

		cfg := core.ServiceConfig{
			Name: "net-mon",
			Type: "pingMonitor",
			Pubs: map[string]core.ChannelInfo{
				"status":  {Name: "status-topic"},
				"metrics": {Name: "metrics-topic"},
			},
			Config: map[string]interface{}{
				"host": "unreachable.host",
			},
		}
		svc := NewService(deps, cfg)
		err := svc.Check()
		assert.NoError(t, err)
		assert.Len(t, messenger.messages, 1)
		assert.Equal(t, "status-topic", messenger.messages[0].ChannelName)
		assert.Contains(t, messenger.messages[0].Text, "unreachable.host")
	})
}

func TestValidateConfig(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	t.Run("valid config", func(t *testing.T) {
		deps := core.Dependencies{}
		deps.SetLogger(logger)
		cfg := core.ServiceConfig{
			Name: "net-mon",
			Pubs: map[string]core.ChannelInfo{
				"status":  {Name: "status-topic"},
				"metrics": {Name: "metrics-topic"},
			},
			Config: map[string]interface{}{
				"host": "google.com",
			},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.Empty(t, errs)
	})

	t.Run("missing host", func(t *testing.T) {
		deps := core.Dependencies{}
		deps.SetLogger(logger)
		cfg := core.ServiceConfig{
			Name: "net-mon",
			Pubs: map[string]core.ChannelInfo{
				"status":  {Name: "status-topic"},
				"metrics": {Name: "metrics-topic"},
			},
			Config: map[string]interface{}{},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "host is required") {
				found = true
			}
		}
		assert.True(t, found, "expected host required error")
	})

	t.Run("missing status channel", func(t *testing.T) {
		deps := core.Dependencies{}
		deps.SetLogger(logger)
		cfg := core.ServiceConfig{
			Name: "net-mon",
			Pubs: map[string]core.ChannelInfo{},
			Config: map[string]interface{}{
				"host": "google.com",
			},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.NotEmpty(t, errs)
		found := false
		for _, e := range errs {
			if contains(e.Error(), "required pubs channel 'status' is missing") {
				found = true
			}
		}
		assert.True(t, found)
	})

}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
