package condition

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

func (m *MockMessenger) Subscribe(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
	return nil
}

func (m *MockMessenger) SubscribeExtended(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message, string, int64) error) error {
	return nil
}

func (m *MockMessenger) SetReaderState(channelName string, readerName string, fileName string, offset int64) error {
	return nil
}

func (m *MockMessenger) SeekToEnd(channelName string, readerName string) error {
	return nil
}

func (m *MockMessenger) SetDataDir(dir string) {}

func (m *MockMessenger) SetHostname(hostname string) {}

func (m *MockMessenger) GetStats() core.MessengerStats {
	return core.MessengerStats{}
}

func testDeps(t *testing.T, messenger core.MessengerApi) core.Dependencies {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}

	deps.SetLogger(logger)
	deps.SetOsProvider(core.FakeOsProvider{Host: "test-host"})
	if messenger != nil {
		deps.SetMessenger(messenger)
	}
	deps.SetContext(context.Background())

	return deps
}

func TestService_MessageHandler(t *testing.T) {
	cfg := core.ServiceConfig{
		Name: "condition-svc",
		Type: "condition-type",
		Subs: map[string]core.ChannelInfo{
			"source": {Name: "source-chan"},
		},
		Pubs: map[string]core.ChannelInfo{
			"target": {Name: "target-chan"},
		},
		Config: map[string]interface{}{
			"conditions": []interface{}{
				map[string]interface{}{
					"field":    "metric",
					"operator": "gt",
					"value":    80,
					"updates": map[string]interface{}{
						"status":  "critical",
						"summary": "High metric value detected",
					},
				},
				map[string]interface{}{
					"field":    "text",
					"operator": "contains",
					"value":    "error",
					"updates": map[string]interface{}{
						"status": "error",
					},
				},
				map[string]interface{}{
					"field":    "status",
					"operator": "eq",
					"value":    "ok",
					"updates": map[string]interface{}{
						"summary": "Everything is fine",
					},
				},
			},
		},
	}

	t.Run("metric gt 80", func(t *testing.T) {
		messenger := &MockMessenger{}
		deps := testDeps(t, messenger)
		svc := NewService(deps, cfg).(*Service)

		msg := core.Message{
			Metric: 85,
		}

		err := svc.messageHandler(msg)
		assert.NoError(t, err)

		require.Len(t, messenger.SentMessages, 1)
		assert.Equal(t, "target-chan", messenger.SentMessages[0].ChannelName)
		assert.Equal(t, "critical", messenger.SentMessages[0].Status)
		assert.Equal(t, "High metric value detected", messenger.SentMessages[0].Summary)
	})

	t.Run("text contains error", func(t *testing.T) {
		messenger := &MockMessenger{}
		deps := testDeps(t, messenger)
		svc := NewService(deps, cfg).(*Service)

		msg := core.Message{
			Text: "something went wrong, error occurred",
		}

		err := svc.messageHandler(msg)
		assert.NoError(t, err)

		require.Len(t, messenger.SentMessages, 1)
		assert.Equal(t, "target-chan", messenger.SentMessages[0].ChannelName)
		assert.Equal(t, "error", messenger.SentMessages[0].Status)
	})

	t.Run("multiple matches", func(t *testing.T) {
		messenger := &MockMessenger{}
		deps := testDeps(t, messenger)
		svc := NewService(deps, cfg).(*Service)

		msg := core.Message{
			Metric: 90,
			Text:   "error in system",
		}

		err := svc.messageHandler(msg)
		assert.NoError(t, err)

		// All matches now result in publishing to 'target'.
		// 1. Metric > 80 matches -> publishes
		// 2. Text contains 'error' matches -> publishes
		require.Len(t, messenger.SentMessages, 2)

		// First match (metric > 80)
		assert.Equal(t, "critical", messenger.SentMessages[0].Status)
		assert.Equal(t, "High metric value detected", messenger.SentMessages[0].Summary)

		// Second match (text contains error)
		assert.Equal(t, "error", messenger.SentMessages[1].Status)
		assert.Equal(t, "High metric value detected", messenger.SentMessages[1].Summary)
	})

	t.Run("no match", func(t *testing.T) {
		messenger := &MockMessenger{}
		deps := testDeps(t, messenger)
		svc := NewService(deps, cfg).(*Service)

		msg := core.Message{
			Metric: 50,
			Text:   "all good",
		}

		err := svc.messageHandler(msg)
		assert.NoError(t, err)
		assert.Empty(t, messenger.SentMessages)
	})
}

func TestService_ValidateConfig(t *testing.T) {
	deps := testDeps(t, nil)

	t.Run("valid config", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Subs: map[string]core.ChannelInfo{"source": {Name: "s"}},
			Pubs: map[string]core.ChannelInfo{"target": {Name: "t"}},
			Config: map[string]interface{}{
				"conditions": []interface{}{
					map[string]interface{}{
						"field":    "metric",
						"operator": "lt",
						"value":    10,
					},
				},
			},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.Empty(t, errs)
	})

	t.Run("invalid operator", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Subs: map[string]core.ChannelInfo{"source": {Name: "s"}},
			Config: map[string]interface{}{
				"conditions": []interface{}{
					map[string]interface{}{
						"field":    "metric",
						"operator": "invalid",
						"value":    10,
					},
				},
			},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.NotEmpty(t, errs)
		assert.Contains(t, errs[0].Error(), "operator")
	})

	t.Run("missing target channel", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Subs: map[string]core.ChannelInfo{"source": {Name: "s"}},
			Config: map[string]interface{}{
				"conditions": []interface{}{
					map[string]interface{}{
						"field":    "metric",
						"operator": "eq",
						"value":    10,
					},
				},
			},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.NotEmpty(t, errs)
		assert.Contains(t, errs[0].Error(), "required pubs channel 'target' is missing")
	})
}
