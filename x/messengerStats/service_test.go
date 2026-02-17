package messengerStats

import (
	"context"
	"keyop/core"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type mockMessenger struct {
	messages []core.Message
	stats    core.MessengerStats
}

func (m *mockMessenger) Send(msg core.Message) error {
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockMessenger) Subscribe(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
	return nil
}

func (m *mockMessenger) SubscribeExtended(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message, string, int64) error) error {
	return nil
}

func (m *mockMessenger) SetReaderState(channelName string, readerName string, fileName string, offset int64) error {
	return nil
}

func (m *mockMessenger) SeekToEnd(channelName string, readerName string) error {
	return nil
}

func (m *mockMessenger) SetDataDir(dir string) {}

func (m *mockMessenger) GetStats() core.MessengerStats {
	return m.stats
}

func TestCheck_MetricName(t *testing.T) {
	tests := []struct {
		name           string
		config         map[string]interface{}
		expectedMetric string
	}{
		{
			name:           "default metric name",
			config:         map[string]interface{}{},
			expectedMetric: "total_messages",
		},
		{
			name: "override metric name",
			config: map[string]interface{}{
				"metricName": "custom_metric_name",
			},
			expectedMetric: "custom_metric_name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := core.Dependencies{}
			deps.SetLogger(&core.FakeLogger{})
			messenger := &mockMessenger{
				stats: core.MessengerStats{
					TotalMessageCount: 10,
				},
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
			for _, m := range messenger.messages {
				if m.ChannelName == "metrics_chan" {
					metricMsg = &m
					break
				}
			}

			assert.NotNil(t, metricMsg, "metric message should be sent")
			assert.Equal(t, tt.expectedMetric, metricMsg.MetricName)
			assert.Equal(t, float64(10), metricMsg.Metric)
		})
	}
}
