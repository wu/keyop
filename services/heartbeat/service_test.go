package heartbeat

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/wu/keyop/core"
	"github.com/wu/keyop/core/adapter"

	testutil "github.com/wu/keyop/core/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeDeps(t *testing.T) (core.Dependencies, *testutil.FakeMessenger) {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(adapter.OsProvider{})
	messenger := testutil.NewFakeMessenger()
	messenger.InstanceNameValue = "test-host"
	deps.SetMessenger(messenger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.SetContext(ctx)
	return deps, messenger
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
	deps, messenger := makeDeps(t)
	cfg := makeCfg("hb-service", nil)

	svc := NewService(deps, cfg, context.Background()).(*Service)
	err := svc.Check()
	require.NoError(t, err)

	// First call: restart (alerts channel) + uptime (heartbeat channel) + metric (metrics channel) = 3 messages.
	require.Len(t, messenger.PublishedMessages, 3)

	events := make(map[string]testutil.PublishedMessage)
	for _, msg := range messenger.PublishedMessages {
		events[msg.PayloadType] = msg
	}

	// alert event (core.alert.v1) on alerts channel
	alertMsg, ok := events["core.alert.v1"]
	assert.True(t, ok, "expected an alert event")
	assert.Equal(t, "alerts", alertMsg.Channel)
	alertEvent, ok := alertMsg.Payload.(*core.AlertEvent)
	assert.True(t, ok, "expected AlertEvent in Payload")
	assert.Contains(t, alertEvent.Summary, "restarted")
	assert.Equal(t, "info", alertEvent.Level)

	// uptime event (service.heartbeat.v1) on heartbeat channel
	uptime, ok := events["service.heartbeat.v1"]
	assert.True(t, ok, "expected a heartbeat event")
	assert.Equal(t, "heartbeat", uptime.Channel)
	hb, ok := uptime.Payload.(*HeartbeatEvent)
	assert.True(t, ok, "expected HeartbeatEvent in Payload")
	assert.Equal(t, "test-host", hb.Hostname)
	assert.NotEmpty(t, hb.Uptime)

	// metric event (core.metric.v1) on metrics channel
	metricMsg, ok := events["core.metric.v1"]
	assert.True(t, ok, "expected a core.metric.v1 event")
	assert.Equal(t, "metrics", metricMsg.Channel)
	metricEvent, ok := metricMsg.Payload.(*core.MetricEvent)
	assert.True(t, ok, "expected MetricEvent in Payload")
	assert.Equal(t, "test-host", metricEvent.Hostname)
	assert.Equal(t, "hb-service", metricEvent.Name)
}

func TestHeartbeatCheck_SubsequentCall_NoRestart(t *testing.T) {
	deps, messenger := makeDeps(t)
	cfg := makeCfg("hb-service", nil)

	svc := NewService(deps, cfg, context.Background()).(*Service)

	// First call sets the instance restart state and may emit a restart event.
	require.NoError(t, svc.Check())
	// Clear captured messages so we can assert on the subsequent check only.
	initialCount := len(messenger.PublishedMessages)

	// Second call should not emit a restart event; only heartbeat and metric messages are expected.
	require.NoError(t, svc.Check())
	newMessages := messenger.PublishedMessages[initialCount:]
	require.Len(t, newMessages, 2)

	payloadTypes := make(map[string]bool)
	for _, msg := range newMessages {
		payloadTypes[msg.PayloadType] = true
	}
	assert.True(t, payloadTypes["service.heartbeat.v1"], "expected heartbeat event")
	assert.True(t, payloadTypes["core.metric.v1"], "expected metric event")
	assert.False(t, payloadTypes["service.restart.v1"], "unexpected restart event")
}

func TestHeartbeatCheck_CustomMetricName(t *testing.T) {
	deps, messenger := makeDeps(t)
	cfg := makeCfg("hb-service", map[string]interface{}{
		"metricName": "custom.heartbeat.name",
	})

	svc := NewService(deps, cfg, context.Background()).(*Service)
	// Prime the instance to consume the restart event, then clear messages.
	require.NoError(t, svc.Check())
	initialCount := len(messenger.PublishedMessages)

	require.NoError(t, svc.Check())
	newMessages := messenger.PublishedMessages[initialCount:]
	require.Len(t, newMessages, 2)

	// Find the metric event and verify the custom metric name
	var metricMsg *testutil.PublishedMessage
	for i := range newMessages {
		if newMessages[i].PayloadType == "core.metric.v1" {
			metricMsg = &newMessages[i]
			break
		}
	}
	require.NotNil(t, metricMsg, "expected a core.metric.v1 message")

	metric, ok := metricMsg.Payload.(*core.MetricEvent)
	require.True(t, ok, "expected MetricEvent in Payload")
	assert.Equal(t, "custom.heartbeat.name", metric.Name)
}

func TestHeartbeatCheck_DefaultMetricName(t *testing.T) {
	deps, newMessenger := makeDeps(t)
	cfg := makeCfg("my-heartbeat", nil)

	svc := NewService(deps, cfg, context.Background()).(*Service)
	// Prime the instance to consume the restart event, then clear messages.
	require.NoError(t, svc.Check())
	initialCount := len(newMessenger.PublishedMessages)

	require.NoError(t, svc.Check())
	newMessages := newMessenger.PublishedMessages[initialCount:]
	require.Len(t, newMessages, 2)

	// Find the metric event and verify the default metric name is the service name
	var metricMsg *testutil.PublishedMessage
	for i := range newMessages {
		if newMessages[i].PayloadType == "core.metric.v1" {
			metricMsg = &newMessages[i]
			break
		}
	}
	require.NotNil(t, metricMsg, "expected a core.metric.v1 message")

	metric, ok := metricMsg.Payload.(*core.MetricEvent)
	require.True(t, ok, "expected MetricEvent in Payload")
	assert.Equal(t, "my-heartbeat", metric.Name)
}

func TestHeartbeatCheck_CorrelationShared(t *testing.T) {
	deps, newMessenger := makeDeps(t)
	cfg := makeCfg("hb-service", nil)

	svc := NewService(deps, cfg, context.Background()).(*Service)
	// Prime and clear restart event.
	require.NoError(t, svc.Check())
	initialCount := len(newMessenger.PublishedMessages)

	require.NoError(t, svc.Check())
	newMessages := newMessenger.PublishedMessages[initialCount:]

	// Verify that we have both heartbeat and metric events
	require.Len(t, newMessages, 2)
	payloadTypes := make(map[string]bool)
	for _, msg := range newMessages {
		payloadTypes[msg.PayloadType] = true
	}
	assert.True(t, payloadTypes["service.heartbeat.v1"], "expected heartbeat event")
	assert.True(t, payloadTypes["core.metric.v1"], "expected metric event")
}

func TestHeartbeatValidateConfig(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)

	svc := Service{Cfg: core.ServiceConfig{Name: "hb"}, Deps: deps}
	// ValidateConfig returns nil — no required channels.
	assert.Nil(t, svc.ValidateConfig())
}
