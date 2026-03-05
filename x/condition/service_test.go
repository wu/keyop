package condition

import (
	"context"
	"keyop/core"
	"log/slog"
	"os"
	"testing"

	testutil "keyop/core/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDeps(messenger core.MessengerApi) core.Dependencies {
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
		Config: map[string]interface{}{
			"conditions": []interface{}{
				map[string]interface{}{
					"when": `metric > 80`,
					"updates": map[string]interface{}{
						"status":  "critical",
						"summary": "High metric value detected",
					},
				},
				map[string]interface{}{
					"when": `text contains error`,
					"updates": map[string]interface{}{
						"status": "error",
					},
				},
				map[string]interface{}{
					"when": `status eq "ok"`,
					"updates": map[string]interface{}{
						"summary": "Everything is fine",
					},
				},
			},
		},
	}

	t.Run("metric gt 80", func(t *testing.T) {
		messenger := &testutil.FakeMessenger{}
		deps := testDeps(messenger)
		svc := NewService(deps, cfg).(*Service)

		msg := core.Message{
			Metric: 85,
		}

		err := svc.messageHandler(msg)
		assert.NoError(t, err)

		require.Len(t, messenger.SentMessages, 1)
		assert.Equal(t, "condition-svc", messenger.SentMessages[0].ChannelName)
		assert.Equal(t, "critical", messenger.SentMessages[0].Status)
		assert.Equal(t, "High metric value detected", messenger.SentMessages[0].Summary)
	})

	t.Run("text contains error", func(t *testing.T) {
		messenger := &testutil.FakeMessenger{}
		deps := testDeps(messenger)
		svc := NewService(deps, cfg).(*Service)

		msg := core.Message{
			Text: "something went wrong, error occurred",
		}

		err := svc.messageHandler(msg)
		assert.NoError(t, err)

		require.Len(t, messenger.SentMessages, 1)
		assert.Equal(t, "condition-svc", messenger.SentMessages[0].ChannelName)
		assert.Equal(t, "error", messenger.SentMessages[0].Status)
	})

	t.Run("multiple matches", func(t *testing.T) {
		messenger := &testutil.FakeMessenger{}
		deps := testDeps(messenger)
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
		messenger := &testutil.FakeMessenger{}
		deps := testDeps(messenger)
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
	deps := testDeps(nil)

	t.Run("valid config", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Subs: map[string]core.ChannelInfo{"source": {Name: "s"}},
			Config: map[string]interface{}{
				"conditions": []interface{}{
					`metric < 10`,
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
					`metric invalid 10`,
				},
			},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.NotEmpty(t, errs)
		assert.Contains(t, errs[0].Error(), "operator")
	})
}
