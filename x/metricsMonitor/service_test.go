package metricsMonitor

import (
	"context"
	"keyop/core"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockMessenger struct {
	SentMessages []core.Message
}

func (m *MockMessenger) Send(msg core.Message) error {
	m.SentMessages = append(m.SentMessages, msg)
	return nil
}

func (m *MockMessenger) Subscribe(ctx context.Context, sourceName string, channelName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
	return nil
}

func (m *MockMessenger) SubscribeExtended(ctx context.Context, source string, channelName string, maxAge time.Duration, messageHandler func(core.Message, string, int64) error) error {
	return nil
}

func (m *MockMessenger) SetReaderState(channelName string, readerName string, fileName string, offset int64) error {
	return nil
}

func (m *MockMessenger) SeekToEnd(channelName string, readerName string) error {
	return nil
}

func testDeps(t *testing.T, messenger core.MessengerApi) core.Dependencies {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}

	deps.SetLogger(logger)
	if messenger != nil {
		deps.SetMessenger(messenger)
	}

	return deps
}

func TestService_MessageHandler(t *testing.T) {
	thresholds := []interface{}{
		map[string]interface{}{
			"metricName": "cpu_load",
			"value":      0.8,
			"condition":  "above",
			"status":     "critical",
		},
		map[string]interface{}{
			"metricName": "temp",
			"value":      20.0,
			"condition":  "below",
			"status":     "warning",
		},
	}

	cfg := core.ServiceConfig{
		Name: "monitor",
		Type: "monitor-type",
		Subs: map[string]core.ChannelInfo{
			"metrics": {Name: "metrics-chan"},
		},
		Pubs: map[string]core.ChannelInfo{
			"status": {Name: "status-chan"},
		},
		Config: map[string]interface{}{
			"thresholds": thresholds,
		},
	}

	t.Run("trigger above threshold", func(t *testing.T) {
		messenger := &MockMessenger{}
		deps := testDeps(t, messenger)
		svc := NewService(deps, cfg).(*Service)

		msg := core.Message{
			MetricName: "cpu_load",
			Metric:     0.85,
		}

		err := svc.messageHandler(msg)
		assert.NoError(t, err)

		require.Len(t, messenger.SentMessages, 1)
		assert.Equal(t, "status-chan", messenger.SentMessages[0].ChannelName)
		assert.Equal(t, "critical", messenger.SentMessages[0].Status)
		assert.Contains(t, messenger.SentMessages[0].Text, "cpu_load")

		// Transition back to OK
		msg.Metric = 0.5
		err = svc.messageHandler(msg)
		assert.NoError(t, err)
		require.Len(t, messenger.SentMessages, 2)
		assert.Equal(t, "status-chan", messenger.SentMessages[1].ChannelName)
		assert.Equal(t, "ok", messenger.SentMessages[1].Status)
	})

	t.Run("trigger below threshold", func(t *testing.T) {
		messenger := &MockMessenger{}
		deps := testDeps(t, messenger)
		svc := NewService(deps, cfg).(*Service)

		msg := core.Message{
			MetricName: "temp",
			Metric:     15.0,
		}

		err := svc.messageHandler(msg)
		assert.NoError(t, err)

		require.Len(t, messenger.SentMessages, 1)
		assert.Equal(t, "status-chan", messenger.SentMessages[0].ChannelName)
		assert.Equal(t, "warning", messenger.SentMessages[0].Status)
		assert.Contains(t, messenger.SentMessages[0].Text, "temp")
	})

	t.Run("no trigger", func(t *testing.T) {
		messenger := &MockMessenger{}
		deps := testDeps(t, messenger)
		svc := NewService(deps, cfg).(*Service)

		msg := core.Message{
			MetricName: "cpu_load",
			Metric:     0.5,
		}

		err := svc.messageHandler(msg)
		assert.NoError(t, err)
		assert.Len(t, messenger.SentMessages, 1)
		assert.Equal(t, "status-chan", messenger.SentMessages[0].ChannelName)
		assert.Equal(t, "ok", messenger.SentMessages[0].Status)
	})

	t.Run("first matching threshold", func(t *testing.T) {
		// New config with overlapping thresholds
		overlappingCfg := core.ServiceConfig{
			Name: "monitor",
			Pubs: map[string]core.ChannelInfo{"status": {Name: "status-chan"}},
			Config: map[string]interface{}{
				"thresholds": []interface{}{
					map[string]interface{}{
						"metricName": "cpu_load",
						"value":      0.5,
						"condition":  "above",
						"status":     "warning",
					},
					map[string]interface{}{
						"metricName": "cpu_load",
						"value":      0.8,
						"condition":  "above",
						"status":     "critical",
					},
				},
			},
		}

		messenger := &MockMessenger{}
		deps := testDeps(t, messenger)
		svc := NewService(deps, overlappingCfg).(*Service)

		msg := core.Message{
			MetricName: "cpu_load",
			Metric:     0.9,
		}

		err := svc.messageHandler(msg)
		assert.NoError(t, err)

		require.Len(t, messenger.SentMessages, 1)
		// critical should take precedence
		assert.Equal(t, "critical", messenger.SentMessages[0].Status)
	})

	t.Run("recovery threshold", func(t *testing.T) {
		recoveryCfg := core.ServiceConfig{
			Name: "monitor",
			Pubs: map[string]core.ChannelInfo{"status": {Name: "status-chan"}},
			Config: map[string]interface{}{
				"thresholds": []interface{}{
					map[string]interface{}{
						"metricName":        "temp",
						"value":             40.0,
						"recoveryThreshold": 39.0,
						"condition":         "above",
						"status":            "critical",
					},
				},
			},
		}

		messenger := &MockMessenger{}
		deps := testDeps(t, messenger)
		svc := NewService(deps, recoveryCfg).(*Service)

		// 1. Above threshold (40.0) -> ALERT
		msg := core.Message{MetricName: "temp", Metric: 41.0}
		err := svc.messageHandler(msg)
		assert.NoError(t, err)
		require.Len(t, messenger.SentMessages, 1)
		assert.Equal(t, "critical", messenger.SentMessages[0].Status)

		// 2. Below threshold but above recovery threshold (39.5) -> STAY ALERT
		msg.Metric = 39.5
		err = svc.messageHandler(msg)
		assert.NoError(t, err)
		require.Len(t, messenger.SentMessages, 2)
		assert.Equal(t, "critical", messenger.SentMessages[1].Status)

		// 3. Below recovery threshold (38.5) -> RECOVERED
		msg.Metric = 38.5
		err = svc.messageHandler(msg)
		assert.NoError(t, err)
		require.Len(t, messenger.SentMessages, 3)
		assert.Equal(t, "ok", messenger.SentMessages[2].Status)

		// 4. Back to 39.5 (above recovery, below threshold) -> STAY OK
		msg.Metric = 39.5
		err = svc.messageHandler(msg)
		assert.NoError(t, err)
		require.Len(t, messenger.SentMessages, 4)
		assert.Equal(t, "ok", messenger.SentMessages[3].Status)
	})

	t.Run("match all metrics (empty MetricName)", func(t *testing.T) {
		cfgAll := core.ServiceConfig{
			Name: "monitor",
			Pubs: map[string]core.ChannelInfo{"status": {Name: "status-chan"}},
			Config: map[string]interface{}{
				"thresholds": []interface{}{
					map[string]interface{}{
						"metricName": "",
						"value":      100.0,
						"condition":  "above",
						"status":     "critical",
					},
				},
			},
		}

		messenger := &MockMessenger{}
		deps := testDeps(t, messenger)
		svc := NewService(deps, cfgAll).(*Service)

		msg := core.Message{
			MetricName: "something_random",
			Metric:     150.0,
		}

		err := svc.messageHandler(msg)
		assert.NoError(t, err)

		require.Len(t, messenger.SentMessages, 1)
		assert.Equal(t, "critical", messenger.SentMessages[0].Status)
	})

	t.Run("send notification every time", func(t *testing.T) {
		messenger := &MockMessenger{}
		deps := testDeps(t, messenger)
		svc := NewService(deps, cfg).(*Service)

		msg := core.Message{
			MetricName: "cpu_load",
			Metric:     0.9,
		}

		// First trigger - should send notification
		err := svc.messageHandler(msg)
		assert.NoError(t, err)
		assert.Len(t, messenger.SentMessages, 1)

		// Second trigger with same status - should STILL send notification
		err = svc.messageHandler(msg)
		assert.NoError(t, err)
		assert.Len(t, messenger.SentMessages, 2)

		// Change status back to ok - should send notification
		msg.Metric = 0.5
		err = svc.messageHandler(msg)
		assert.NoError(t, err)
		assert.Len(t, messenger.SentMessages, 3)
		assert.Equal(t, "ok", messenger.SentMessages[2].Status)
	})
}

func TestService_StateTransitions(t *testing.T) {
	thresholds := []interface{}{
		map[string]interface{}{
			"metricName":        "test_metric",
			"value":             70.0,
			"recoveryThreshold": 69.0,
			"condition":         "above",
			"status":            "warning",
		},
		map[string]interface{}{
			"metricName":        "test_metric",
			"value":             90.0,
			"recoveryThreshold": 89.0,
			"condition":         "above",
			"status":            "critical",
		},
	}

	cfg := core.ServiceConfig{
		Name: "monitor",
		Type: "monitor-type",
		Subs: map[string]core.ChannelInfo{
			"metrics": {Name: "metrics-chan"},
		},
		Pubs: map[string]core.ChannelInfo{
			"status": {Name: "status-chan"},
		},
		Config: map[string]interface{}{
			"thresholds": thresholds,
		},
	}

	messenger := &MockMessenger{}
	deps := testDeps(t, messenger)
	svc := NewService(deps, cfg).(*Service)

	// 1. Start in OK state
	msg := core.Message{
		MetricName: "test_metric",
		Metric:     50.0,
	}
	err := svc.messageHandler(msg)
	assert.NoError(t, err)
	assert.Len(t, messenger.SentMessages, 1)
	assert.Equal(t, "ok", messenger.SentMessages[0].Status)

	// 2. Move to Warning state
	msg.Metric = 75.0
	err = svc.messageHandler(msg)
	assert.NoError(t, err)
	require.Len(t, messenger.SentMessages, 2)
	assert.Equal(t, "warning", messenger.SentMessages[1].Status)

	// 3. Recover from Warning
	msg.Metric = 50.0
	err = svc.messageHandler(msg)
	assert.NoError(t, err)
	require.Len(t, messenger.SentMessages, 3)
	assert.Equal(t, "ok", messenger.SentMessages[2].Status)

	// 4. Move to Critical state
	msg.Metric = 95.0
	err = svc.messageHandler(msg)
	assert.NoError(t, err)
	require.Len(t, messenger.SentMessages, 4)
	assert.Equal(t, "critical", messenger.SentMessages[3].Status)

	// 5. Recover from Critical
	msg.Metric = 50.0
	err = svc.messageHandler(msg)
	assert.NoError(t, err)
	require.Len(t, messenger.SentMessages, 5)
	assert.Equal(t, "ok", messenger.SentMessages[4].Status)

	// 6. Move to Warning state
	msg.Metric = 75.0
	err = svc.messageHandler(msg)
	assert.NoError(t, err)
	require.Len(t, messenger.SentMessages, 6)
	assert.Equal(t, "warning", messenger.SentMessages[5].Status)

	// 7. Move back to Critical state
	msg.Metric = 95.0
	err = svc.messageHandler(msg)
	assert.NoError(t, err)
	require.Len(t, messenger.SentMessages, 7)
	assert.Equal(t, "critical", messenger.SentMessages[6].Status)

	// 8. Move back to Warning state
	msg.Metric = 75.0
	err = svc.messageHandler(msg)
	assert.NoError(t, err)
	require.Len(t, messenger.SentMessages, 8)
	assert.Equal(t, "warning", messenger.SentMessages[7].Status)

	// 9. Recover from Warning
	msg.Metric = 50.0
	err = svc.messageHandler(msg)
	assert.NoError(t, err)
	require.Len(t, messenger.SentMessages, 9)
	assert.Equal(t, "ok", messenger.SentMessages[8].Status)
}

func TestService_ValidateConfig(t *testing.T) {
	deps := testDeps(t, nil)

	t.Run("valid config", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Subs: map[string]core.ChannelInfo{"metrics": {Name: "m"}},
			Pubs: map[string]core.ChannelInfo{"status": {Name: "s"}},
			Config: map[string]interface{}{
				"thresholds": []interface{}{},
			},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.Empty(t, errs)
	})

	t.Run("missing sub", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Subs: map[string]core.ChannelInfo{},
			Pubs: map[string]core.ChannelInfo{"status": {Name: "s"}},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.NotEmpty(t, errs)
	})

	t.Run("missing pub", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Subs: map[string]core.ChannelInfo{"metrics": {Name: "m"}},
			Pubs: map[string]core.ChannelInfo{},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.NotEmpty(t, errs)
	})
}
