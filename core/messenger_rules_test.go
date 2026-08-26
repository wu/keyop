package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/wu/keyop/core"
	"github.com/wu/keyop/core/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	km "github.com/wu/keyop-messenger"
)

const alertPayload = "core.alert.v1"

func silenceRule(t *testing.T) core.Rule {
	t.Helper()
	rules, errs := core.ParseRules("test", []core.RuleSpec{{
		Type:  alertPayload,
		When:  map[string]any{"level": "warning"},
		Force: map[string]any{"notify.audio": core.AudioSilent},
	}})
	require.Empty(t, errs)
	return rules[0]
}

func rulesMessenger(t *testing.T, pub, sub []core.Rule) (core.MessengerApi, *testutil.FakeMessenger, *testutil.FakeLogger) {
	t.Helper()
	inner := testutil.NewFakeMessenger()
	logger := &testutil.FakeLogger{}
	return core.NewRulesMessenger(inner, pub, sub, logger, "speak"), inner, logger
}

func TestRulesMessenger_Publish_AppliesPubRules(t *testing.T) {
	m, inner, _ := rulesMessenger(t, []core.Rule{silenceRule(t)}, nil)

	require.NoError(t, m.Publish(context.Background(), "alerts", alertPayload,
		core.AlertEvent{Summary: "s", Level: "warning"}))

	require.Len(t, inner.PublishedMessages, 1)
	sent, ok := inner.PublishedMessages[0].Payload.(core.AlertEvent)
	require.True(t, ok, "the published payload must keep its concrete type, got %T", inner.PublishedMessages[0].Payload)
	require.NotNil(t, sent.Notify)
	assert.Equal(t, core.AudioSilent, sent.Notify.Audio)
}

// A producer builds an alert, publishes the pointer, and may keep using it.
func TestRulesMessenger_Publish_DoesNotMutateCaller(t *testing.T) {
	m, inner, _ := rulesMessenger(t, []core.Rule{silenceRule(t)}, nil)

	original := &core.AlertEvent{Summary: "s", Level: "warning", Notify: &core.NotifyHints{Audio: core.AudioSpeak}}
	require.NoError(t, m.Publish(context.Background(), "alerts", alertPayload, original))

	assert.Equal(t, core.AudioSpeak, original.Notify.Audio, "the caller's hints must not be rewritten")
	sent := inner.PublishedMessages[0].Payload.(*core.AlertEvent)
	assert.Equal(t, core.AudioSilent, sent.Notify.Audio)
	assert.NotSame(t, original, sent)
}

func TestRulesMessenger_Publish_NoMatchPassesThrough(t *testing.T) {
	m, inner, _ := rulesMessenger(t, []core.Rule{silenceRule(t)}, nil)

	in := core.AlertEvent{Summary: "s", Level: "info"}
	require.NoError(t, m.Publish(context.Background(), "alerts", alertPayload, in))
	assert.Equal(t, in, inner.PublishedMessages[0].Payload)
}

// Rules on the pub side must not touch a payload of a different type.
func TestRulesMessenger_Publish_OtherPayloadTypesUntouched(t *testing.T) {
	m, inner, _ := rulesMessenger(t, []core.Rule{silenceRule(t)}, nil)

	in := core.ErrorEvent{Summary: "s", Level: "warning"}
	require.NoError(t, m.Publish(context.Background(), "errors", "core.error.v1", in))
	assert.Equal(t, in, inner.PublishedMessages[0].Payload)
}

func TestRulesMessenger_Subscribe_AppliesSubRules(t *testing.T) {
	m, inner, _ := rulesMessenger(t, nil, []core.Rule{silenceRule(t)})

	var got core.AlertEvent
	var called bool
	require.NoError(t, m.Subscribe(context.Background(), "alerts", "speak-alerts",
		func(_ context.Context, msg km.Message) error {
			called = true
			// Subscribers assert on the concrete value type; that must survive.
			got = msg.Payload.(core.AlertEvent)
			return nil
		}))

	handler := inner.Handlers["alerts"]
	require.NotNil(t, handler)
	require.NoError(t, handler(context.Background(), km.Message{
		Channel:     "alerts",
		PayloadType: alertPayload,
		Payload:     core.AlertEvent{Summary: "s", Level: "warning"},
	}))

	require.True(t, called)
	require.NotNil(t, got.Notify)
	assert.Equal(t, core.AudioSilent, got.Notify.Audio)
}

// The central guarantee: rules mutate, they never drop. A rule that cannot be
// applied is logged, and the handler still runs with the original payload.
func TestRulesMessenger_Subscribe_BadRuleStillDelivers(t *testing.T) {
	broken := core.Rule{PayloadType: alertPayload, Force: map[string]any{"nosuchfield": "x"}}
	m, inner, logger := rulesMessenger(t, nil, []core.Rule{broken})

	var got any
	require.NoError(t, m.Subscribe(context.Background(), "alerts", "speak-alerts",
		func(_ context.Context, msg km.Message) error {
			got = msg.Payload
			return nil
		}))

	in := core.AlertEvent{Summary: "s", Level: "warning"}
	require.NoError(t, inner.Handlers["alerts"](context.Background(), km.Message{
		Channel: "alerts", PayloadType: alertPayload, Payload: in,
	}))

	assert.Equal(t, in, got, "the message must be delivered unchanged, not dropped")
	assert.Contains(t, logger.LastErrMsg(), "could not apply rule")
}

// A handler error is the subscriber's own; the wrapper must pass it through so
// the messenger's retry and dead-letter logic still sees it.
func TestRulesMessenger_Subscribe_HandlerErrorPropagates(t *testing.T) {
	m, inner, _ := rulesMessenger(t, nil, []core.Rule{silenceRule(t)})

	sentinel := errors.New("handler failed")
	require.NoError(t, m.Subscribe(context.Background(), "alerts", "speak-alerts",
		func(_ context.Context, _ km.Message) error { return sentinel }))

	err := inner.Handlers["alerts"](context.Background(), km.Message{
		Channel: "alerts", PayloadType: alertPayload,
		Payload: core.AlertEvent{Level: "warning"},
	})
	assert.ErrorIs(t, err, sentinel)
}

func TestRulesMessenger_NoRulesIsTransparent(t *testing.T) {
	m, inner, _ := rulesMessenger(t, nil, nil)

	in := core.AlertEvent{Summary: "s", Level: "warning"}
	require.NoError(t, m.Publish(context.Background(), "alerts", alertPayload, in))
	assert.Equal(t, in, inner.PublishedMessages[0].Payload)

	var got any
	require.NoError(t, m.Subscribe(context.Background(), "alerts", "sub",
		func(_ context.Context, msg km.Message) error { got = msg.Payload; return nil }))
	require.NoError(t, inner.Handlers["alerts"](context.Background(), km.Message{
		Channel: "alerts", PayloadType: alertPayload, Payload: in,
	}))
	assert.Equal(t, in, got)
}

// Every other capability must still reach the wrapped messenger, or a service
// would silently lose it by being given the decorator.
func TestRulesMessenger_PromotesOtherMethods(t *testing.T) {
	inner := testutil.NewFakeMessenger()
	inner.InstanceNameValue = "host1"
	require.NoError(t, inner.RegisterPayloadType(alertPayload, &core.AlertEvent{}))

	m := core.NewRulesMessenger(inner, nil, nil, &testutil.FakeLogger{}, "speak")

	assert.Equal(t, "host1", m.InstanceName())
	_, ok := m.PayloadPrototype(alertPayload)
	assert.True(t, ok)
	assert.NoError(t, m.Close())
}

// The debug line is the only trace of why a payload changed, so it has to carry
// enough to find the rule that did it.
func TestRulesMessenger_LogsWhatChanged(t *testing.T) {
	m, _, logger := rulesMessenger(t, []core.Rule{silenceRule(t)}, nil)

	require.NoError(t, m.Publish(context.Background(), "alerts", alertPayload,
		core.AlertEvent{Summary: "s", Level: "warning"}))

	assert.Contains(t, logger.LastDebugMsg, "rule changed a field")
	fields := map[string]any{}
	for i := 0; i+1 < len(logger.LastDebugArgs); i += 2 {
		fields[logger.LastDebugArgs[i].(string)] = logger.LastDebugArgs[i+1]
	}
	assert.Equal(t, "speak", fields["service"])
	assert.Equal(t, "pub", fields["side"])
	assert.Equal(t, "alerts", fields["channel"])
	assert.Equal(t, "notify.audio", fields["path"])
	assert.Equal(t, core.AudioSilent, fields["to"])
	assert.Equal(t, 0, fields["rule"])
}
