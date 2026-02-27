package heartbeat

import (
	"context"
	"keyop/core"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func (m *mockMessenger) Subscribe(_ context.Context, _ string, _ string, _ string, _ string, _ time.Duration, _ func(core.Message) error) error {
	return nil
}

func (m *mockMessenger) SubscribeExtended(_ context.Context, _ string, _ string, _ string, _ string, _ time.Duration, _ func(core.Message, string, int64) error) error {
	return nil
}

func (m *mockMessenger) SetReaderState(_ string, _ string, _ string, _ int64) error { return nil }
func (m *mockMessenger) SeekToEnd(_ string, _ string) error                         { return nil }
func (m *mockMessenger) SetDataDir(_ string)                                        {}
func (m *mockMessenger) SetHostname(_ string)                                       {}
func (m *mockMessenger) GetStats() core.MessengerStats                              { return core.MessengerStats{} }

func makeDeps(t *testing.T, messenger core.MessengerApi) core.Dependencies {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.OsProvider{})
	deps.SetMessenger(messenger)
	return deps
}

func makeCfg(name string, extraConfig map[string]interface{}) core.ServiceConfig {
	cfg := core.ServiceConfig{
		Name: name,
		Type: "heartbeat",
		Pubs: map[string]core.ChannelInfo{},
	}
	if extraConfig != nil {
		cfg.Config = extraConfig
	}
	return cfg
}

// TestHeartbeatCheck verifies the messages produced by Check().
// Because restartNotified is a package-level var, the very first call to Check()
// across all tests will also emit a "restart" event.
func TestHeartbeatCheck(t *testing.T) {
	// Reset so this test controls the restart message.
	restartNotified = false

	messenger := &mockMessenger{}
	deps := makeDeps(t, messenger)
	cfg := makeCfg("hb-service", nil)

	svc := NewService(deps, cfg).(Service)
	err := svc.Check()
	require.NoError(t, err)

	// First call: restart + uptime + uptime-metric = 3 messages.
	require.Len(t, messenger.messages, 3)

	events := make(map[string]core.Message)
	for _, msg := range messenger.messages {
		events[msg.Event] = msg
	}

	// All messages use cfg.Name as channel.
	for _, msg := range messenger.messages {
		assert.Equal(t, "hb-service", msg.ChannelName)
		assert.Equal(t, "hb-service", msg.ServiceName)
		assert.Equal(t, "heartbeat", msg.ServiceType)
	}

	// restart event
	restart, ok := events["restart"]
	assert.True(t, ok, "expected a restart event")
	assert.NotEmpty(t, restart.Text)

	// uptime event
	uptime, ok := events["uptime_check"]
	assert.True(t, ok, "expected an uptime event")
	assert.Equal(t, "hb-service", uptime.MetricName)
	assert.NotEmpty(t, uptime.Text)
	assert.NotNil(t, uptime.Data)

	// uptime_metric event
	metric, ok := events["uptime_metric"]
	assert.True(t, ok, "expected an uptime-metric event")
	assert.Equal(t, "hb-service", metric.MetricName)
}

func TestHeartbeatCheck_SubsequentCall_NoRestart(t *testing.T) {
	// Ensure restartNotified is true so the restart message is suppressed.
	restartNotified = true

	messenger := &mockMessenger{}
	deps := makeDeps(t, messenger)
	cfg := makeCfg("hb-service", nil)

	svc := NewService(deps, cfg).(Service)
	err := svc.Check()
	require.NoError(t, err)

	// Only uptime + uptime-metric = 2 messages.
	require.Len(t, messenger.messages, 2)

	events := make(map[string]core.Message)
	for _, msg := range messenger.messages {
		events[msg.Event] = msg
	}
	assert.Contains(t, events, "uptime_check")
	assert.Contains(t, events, "uptime_metric")
	assert.NotContains(t, events, "restart")
}

func TestHeartbeatCheck_CustomMetricName(t *testing.T) {
	restartNotified = true

	messenger := &mockMessenger{}
	deps := makeDeps(t, messenger)
	cfg := makeCfg("hb-service", map[string]interface{}{
		"metricName": "custom.heartbeat.name",
	})

	svc := NewService(deps, cfg).(Service)
	err := svc.Check()
	require.NoError(t, err)

	require.Len(t, messenger.messages, 2)
	for _, msg := range messenger.messages {
		assert.Equal(t, "custom.heartbeat.name", msg.MetricName)
	}
}

func TestHeartbeatCheck_DefaultMetricName(t *testing.T) {
	restartNotified = true

	messenger := &mockMessenger{}
	deps := makeDeps(t, messenger)
	cfg := makeCfg("my-heartbeat", nil)

	svc := NewService(deps, cfg).(Service)
	err := svc.Check()
	require.NoError(t, err)

	for _, msg := range messenger.messages {
		if msg.Event == "uptime" || msg.Event == "uptime-metric" {
			assert.Equal(t, "my-heartbeat", msg.MetricName)
		}
	}
}

func TestHeartbeatCheck_CorrelationShared(t *testing.T) {
	restartNotified = true

	messenger := &mockMessenger{}
	deps := makeDeps(t, messenger)
	cfg := makeCfg("hb-service", nil)

	svc := NewService(deps, cfg).(Service)
	require.NoError(t, svc.Check())

	require.Len(t, messenger.messages, 2)
	corr := messenger.messages[0].Correlation
	assert.NotEmpty(t, corr)
	for _, msg := range messenger.messages {
		assert.Equal(t, corr, msg.Correlation, "all messages should share the same correlation id")
	}
}

func TestHeartbeatValidateConfig(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)

	svc := Service{Cfg: core.ServiceConfig{Name: "hb"}, Deps: deps}
	// ValidateConfig returns nil — no required channels.
	assert.Nil(t, svc.ValidateConfig())
}
