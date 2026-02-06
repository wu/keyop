package metricsMonitor

import (
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

func (m *MockMessenger) Subscribe(sourceName string, channelName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
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
			"alertText":  "CPU load is too high",
		},
		map[string]interface{}{
			"metricName": "temp",
			"value":      20.0,
			"condition":  "below",
			"status":     "warning",
			"alertText":  "Temperature is too low",
		},
	}

	cfg := core.ServiceConfig{
		Name: "monitor",
		Type: "monitor-type",
		Subs: map[string]core.ChannelInfo{
			"metrics": {Name: "metrics-chan"},
		},
		Pubs: map[string]core.ChannelInfo{
			"alerts": {Name: "alerts-chan"},
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
		assert.Equal(t, "alerts-chan", messenger.SentMessages[0].ChannelName)
		assert.Equal(t, "critical", messenger.SentMessages[0].Status)
		assert.Contains(t, messenger.SentMessages[0].Text, "CPU load is too high")
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
		assert.Equal(t, "warning", messenger.SentMessages[0].Status)
		assert.Contains(t, messenger.SentMessages[0].Text, "Temperature is too low")
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
		assert.Len(t, messenger.SentMessages, 0)
	})

	t.Run("first matching threshold", func(t *testing.T) {
		// New config with overlapping thresholds
		overlappingCfg := core.ServiceConfig{
			Name: "monitor",
			Pubs: map[string]core.ChannelInfo{"alerts": {Name: "alerts-chan"}},
			Config: map[string]interface{}{
				"thresholds": []interface{}{
					map[string]interface{}{
						"metricName": "cpu_load",
						"value":      0.5,
						"condition":  "above",
						"alertText":  "First threshold",
					},
					map[string]interface{}{
						"metricName": "cpu_load",
						"value":      0.8,
						"condition":  "above",
						"alertText":  "Second threshold",
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
		assert.Contains(t, messenger.SentMessages[0].Text, "First threshold")
	})

	t.Run("match all metrics (empty MetricName)", func(t *testing.T) {
		cfgAll := core.ServiceConfig{
			Name: "monitor",
			Pubs: map[string]core.ChannelInfo{"alerts": {Name: "alerts-chan"}},
			Config: map[string]interface{}{
				"thresholds": []interface{}{
					map[string]interface{}{
						"metricName": "",
						"value":      100.0,
						"condition":  "above",
						"alertText":  "Any metric above 100",
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
		assert.Contains(t, messenger.SentMessages[0].Text, "Any metric above 100")
	})

	t.Run("only send notification when status changes", func(t *testing.T) {
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

		// Second trigger with same status - should NOT send notification
		err = svc.messageHandler(msg)
		assert.NoError(t, err)
		assert.Len(t, messenger.SentMessages, 1)

		// Change status back to normal (below threshold) - should not send notification from this threshold
		// but if we had a "normal" status we might want to send it.
		// The issue says "Only send a notification when the status changes".
		// Currently, it only sends if `triggered` is true.

		msg.Metric = 0.5
		err = svc.messageHandler(msg)
		assert.NoError(t, err)
		assert.Len(t, messenger.SentMessages, 1) // Still 1

		// Trigger again - should send notification because it changed from not-triggered to triggered
		msg.Metric = 0.95
		err = svc.messageHandler(msg)
		assert.NoError(t, err)
		assert.Len(t, messenger.SentMessages, 2)
	})
}

func TestService_ValidateConfig(t *testing.T) {
	deps := testDeps(t, nil)

	t.Run("valid config", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Subs: map[string]core.ChannelInfo{"metrics": {Name: "m"}},
			Pubs: map[string]core.ChannelInfo{"alerts": {Name: "a"}},
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
			Pubs: map[string]core.ChannelInfo{"alerts": {Name: "a"}},
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
