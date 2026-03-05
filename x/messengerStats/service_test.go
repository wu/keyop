package messengerStats

import (
	"keyop/core"
	"keyop/core/testutil"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCheck_MetricName(t *testing.T) {
	tests := []struct {
		name           string
		config         map[string]interface{}
		expectedMetric string
	}{
		{
			name:           "default metric name",
			config:         map[string]interface{}{},
			expectedMetric: "messages",
		},
		{
			name: "override metric name",
			config: map[string]interface{}{
				"metric_name": "custom_metric_name",
			},
			expectedMetric: "custom_metric_name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := core.Dependencies{}
			deps.SetLogger(&core.FakeLogger{})
			messenger := testutil.NewFakeMessenger()
			messenger.Stats = core.MessengerStats{
				TotalMessageCount: 10,
			}
			deps.SetMessenger(messenger)

			cfg := core.ServiceConfig{
				Name: "stats_service",
				Type: "messengerStats",
				Pubs: map[string]core.ChannelInfo{
					"events":  {Name: "events_chan"},
					"metrics": {Name: "metrics_chan"},
				},
				Config: tt.config,
			}

			svc := NewService(deps, cfg)
			err := svc.Check()
			assert.NoError(t, err)

			var metricMsg *core.Message
			for _, m := range messenger.SentMessages {
				if m.ChannelName == "metrics_chan" {
					metricMsg = &m
					break
				}
			}

			assert.Nil(t, metricMsg, "metric message should not be sent on first check")
		})
	}
}

func TestCheck_MessagesPerMinute(t *testing.T) {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	messenger := testutil.NewFakeMessenger()
	messenger.Stats = core.MessengerStats{
		TotalMessageCount: 10,
	}
	deps.SetMessenger(messenger)

	cfg := core.ServiceConfig{
		Name: "stats_service",
		Type: "messengerStats",
		Pubs: map[string]core.ChannelInfo{
			"events":  {Name: "events_chan"},
			"metrics": {Name: "metrics_chan"},
		},
	}

	svc := NewService(deps, cfg)

	// First check, lastCheckTime is zero, so no MPM metric
	err := svc.Check()
	assert.NoError(t, err)

	mpmSent := false
	for _, m := range messenger.SentMessages {
		if m.MetricName == "stats_service" {
			mpmSent = true
		}
	}
	assert.False(t, mpmSent, "MPM should not be sent on first check")

	// Advance stats and wait a bit
	messenger.Stats.TotalMessageCount = 20
	messenger.SentMessages = nil // clear messages
	time.Sleep(100 * time.Millisecond)

	// Second check
	err = svc.Check()
	assert.NoError(t, err)

	var mpmMsg *core.Message
	for _, m := range messenger.SentMessages {
		if m.MetricName == "stats_service" {
			mpmMsg = &m
			break
		}
	}

	assert.NotNil(t, mpmMsg, "metric message should be sent on second check")
	assert.Greater(t, mpmMsg.Metric, 0.0, "MPM metric should be greater than 0")
	// 10 messages in ~0.1s -> ~100 msgs/s -> ~6000 msgs/min
	assert.InDelta(t, 6000.0, mpmMsg.Metric, 3000.0)
}
