package runtime

import (
	"context"
	"testing"

	"github.com/wu/keyop/core"
	"github.com/wu/keyop/core/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureService records the messenger the runtime handed it, which is what the
// wiring test needs to inspect.
type captureService struct {
	msgr core.MessengerApi
}

func (s *captureService) Initialize() error       { return nil }
func (s *captureService) Check() error            { return nil }
func (s *captureService) ValidateConfig() []error { return nil }

// A service's configured rules have to reach the messenger it is given, or rules
// would validate at startup and then never fire.
func TestRun_ServiceMessengerAppliesConfiguredRules(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := core.Dependencies{}
	deps.SetLogger(&testutil.FakeLogger{})
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	fakeMsgr := testutil.NewFakeMessenger()
	require.NoError(t, fakeMsgr.RegisterPayloadType("core.alert.v1", &core.AlertEvent{}))
	deps.SetMessenger(fakeMsgr)

	svc := &captureService{}
	serviceType := "rules_wiring_" + t.Name()
	core.RegisterService(serviceType, func(d core.Dependencies, _ core.ServiceConfig, _ context.Context) interface{} {
		svc.msgr = d.MustGetMessenger()
		return svc
	})

	rules, errs := core.ParseRules("test", []core.RuleSpec{{
		Type:  "core.alert.v1",
		When:  map[string]any{"level": "warning"},
		Force: map[string]any{"notify.audio": core.AudioSilent, "category": "tide"},
	}})
	require.Empty(t, errs)

	require.NoError(t, run(deps, []core.ServiceConfig{{
		Name:     "tides",
		Type:     serviceType,
		PubRules: rules,
	}}))

	require.NotNil(t, svc.msgr, "the service should have been constructed")
	require.NoError(t, svc.msgr.Publish(ctx, "alerts", "core.alert.v1",
		core.AlertEvent{Summary: "extreme low tide", Level: "warning"}))

	require.Len(t, fakeMsgr.PublishedMessages, 1)
	sent, ok := fakeMsgr.PublishedMessages[0].Payload.(core.AlertEvent)
	require.True(t, ok, "payload should still be a core.AlertEvent, got %T", fakeMsgr.PublishedMessages[0].Payload)
	require.NotNil(t, sent.Notify)
	assert.Equal(t, core.AudioSilent, sent.Notify.Audio)
	assert.Equal(t, "tide", sent.Category)
}

// A service with no rules must be given a messenger that changes nothing, and the
// SubscribeOptions decorator underneath must still be in place.
func TestRun_ServiceMessengerWithoutRules(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := core.Dependencies{}
	deps.SetLogger(&testutil.FakeLogger{})
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	deps.SetOsProvider(&testutil.FakeOsProvider{Host: "test-host"})
	deps.SetStateStore(&testutil.NoOpStateStore{})

	fakeMsgr := testutil.NewFakeMessenger()
	deps.SetMessenger(fakeMsgr)

	svc := &captureService{}
	serviceType := "rules_wiring_none_" + t.Name()
	core.RegisterService(serviceType, func(d core.Dependencies, _ core.ServiceConfig, _ context.Context) interface{} {
		svc.msgr = d.MustGetMessenger()
		return svc
	})

	require.NoError(t, run(deps, []core.ServiceConfig{{
		Name: "speak",
		Type: serviceType,
		Subs: map[string]core.ChannelInfo{"alerts": {Name: "alerts"}},
	}}))

	in := core.AlertEvent{Summary: "s", Level: "warning"}
	require.NoError(t, svc.msgr.Publish(ctx, "alerts", "core.alert.v1", in))
	require.Len(t, fakeMsgr.PublishedMessages, 1)
	assert.Equal(t, in, fakeMsgr.PublishedMessages[0].Payload)
}
