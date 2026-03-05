package heartbeat

import (
	"log/slog"
	"os"
	"testing"

	"keyop/core"

	testutil "keyop/core/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
// The restart event should be emitted once per service instance on the first check.
func TestHeartbeatCheck(t *testing.T) {
	messenger := &testutil.FakeMessenger{}
	deps := makeDeps(t, messenger)
	cfg := makeCfg("hb-service", nil)

	svc := NewService(deps, cfg).(*Service)
	err := svc.Check()
	require.NoError(t, err)

	// First call: restart + uptime + uptime-metric = 3 messages.
	require.Len(t, messenger.SentMessages, 3)

	events := make(map[string]core.Message)
	for _, msg := range messenger.SentMessages {
		events[msg.Event] = msg
	}

	// All messages use cfg.Name as channel.
	for _, msg := range messenger.SentMessages {
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

	// Verify typed payload
	hb, ok := uptime.Data.(HeartbeatEvent)
	assert.True(t, ok, "expected HeartbeatEvent in Data")
	assert.NotEmpty(t, hb.Uptime)

	// uptime_metric event
	metric, ok := events["uptime_metric"]
	assert.True(t, ok, "expected an uptime-metric event")
	assert.Equal(t, "hb-service", metric.MetricName)
}

func TestHeartbeatCheck_SubsequentCall_NoRestart(t *testing.T) {
	messenger := &testutil.FakeMessenger{}
	deps := makeDeps(t, messenger)
	cfg := makeCfg("hb-service", nil)

	svc := NewService(deps, cfg).(*Service)

	// First call sets the instance restart state and may emit a restart event.
	require.NoError(t, svc.Check())
	// Clear captured messages so we can assert on the subsequent check only.
	messenger.Reset()

	// Second call should not emit a restart event; only uptime messages are expected.
	require.NoError(t, svc.Check())
	require.Len(t, messenger.SentMessages, 2)

	events := make(map[string]core.Message)
	for _, msg := range messenger.SentMessages {
		events[msg.Event] = msg
	}
	assert.Contains(t, events, "uptime_check")
	assert.Contains(t, events, "uptime_metric")
	assert.NotContains(t, events, "restart")
}

func TestHeartbeatCheck_CustomMetricName(t *testing.T) {
	messenger := &testutil.FakeMessenger{}
	deps := makeDeps(t, messenger)
	cfg := makeCfg("hb-service", map[string]interface{}{
		"metricName": "custom.heartbeat.name",
	})

	svc := NewService(deps, cfg).(*Service)
	// Prime the instance to consume the restart event, then clear messages.
	require.NoError(t, svc.Check())
	messenger.Reset()

	require.NoError(t, svc.Check())
	require.Len(t, messenger.SentMessages, 2)
	for _, msg := range messenger.SentMessages {
		assert.Equal(t, "custom.heartbeat.name", msg.MetricName)
	}
}

func TestHeartbeatCheck_DefaultMetricName(t *testing.T) {
	messenger := &testutil.FakeMessenger{}
	deps := makeDeps(t, messenger)
	cfg := makeCfg("my-heartbeat", nil)

	svc := NewService(deps, cfg).(*Service)
	// Prime the instance to consume the restart event, then clear messages.
	require.NoError(t, svc.Check())
	messenger.Reset()

	require.NoError(t, svc.Check())

	for _, msg := range messenger.SentMessages {
		if msg.Event == "uptime_check" || msg.Event == "uptime_metric" {
			assert.Equal(t, "my-heartbeat", msg.MetricName)
		}
	}
}

func TestHeartbeatCheck_CorrelationShared(t *testing.T) {
	messenger := &testutil.FakeMessenger{}
	deps := makeDeps(t, messenger)
	cfg := makeCfg("hb-service", nil)

	svc := NewService(deps, cfg).(*Service)
	// Prime and clear restart event.
	require.NoError(t, svc.Check())
	messenger.Reset()

	require.NoError(t, svc.Check())

	require.Len(t, messenger.SentMessages, 2)
	corr := messenger.SentMessages[0].Correlation
	assert.NotEmpty(t, corr)
	for _, msg := range messenger.SentMessages {
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
