package core

import (
	"context"

	km "github.com/wu/keyop-messenger"
)

// rulesMessenger decorates a MessengerApi so that a service's configured rules
// are applied to a message's payload as it is published and as it is received,
// without the service knowing rules exist. Every method except Publish and
// Subscribe is promoted unchanged from the embedded MessengerApi, so the
// decorator is a transparent stand-in for the underlying messenger.
//
// Rules mutate only. A rule that fails to apply is logged and skipped; it never
// turns into a delivery error, and a subscriber's handler is always called. This
// is deliberate: a message must not disappear because a config file is wrong,
// and startup validation has already rejected the rules that could fail here.
type rulesMessenger struct {
	MessengerApi
	pubRules []Rule
	subRules []Rule
	logger   Logger
	// service names the config the rules came from, so a log line points at the
	// file to edit rather than just at the message.
	service string
}

// NewRulesMessenger wraps inner so that pub rules are applied on Publish and sub
// rules on delivery to a subscriber. Either set may be empty, in which case that
// side passes through untouched; the wrapper is still returned, as it is cheap
// and transparent.
func NewRulesMessenger(inner MessengerApi, pubRules, subRules []Rule, logger Logger, service string) MessengerApi {
	return &rulesMessenger{
		MessengerApi: inner,
		pubRules:     pubRules,
		subRules:     subRules,
		logger:       logger,
		service:      service,
	}
}

// Publish applies the service's pub rules to payload before handing it to the
// wrapped messenger. The caller's value is never modified.
func (m *rulesMessenger) Publish(ctx context.Context, channel string, payloadType string, payload interface{}) error {
	payload = m.apply(m.pubRules, "pub", channel, payloadType, payload)
	return m.MessengerApi.Publish(ctx, channel, payloadType, payload)
}

// Subscribe wraps handler so the service's sub rules are applied to each message
// before the service sees it. The message's payload keeps its concrete type, so
// handlers that type assert on it continue to work.
func (m *rulesMessenger) Subscribe(ctx context.Context, channel string, subscriberID string, handler km.HandlerFunc, opts ...km.SubscribeOption) error {
	if len(m.subRules) == 0 {
		return m.MessengerApi.Subscribe(ctx, channel, subscriberID, handler, opts...)
	}

	wrapped := func(ctx context.Context, msg km.Message) error {
		// msg is a value, so this rewrites only our copy.
		msg.Payload = m.apply(m.subRules, "sub", msg.Channel, msg.PayloadType, msg.Payload)
		return handler(ctx, msg)
	}
	return m.MessengerApi.Subscribe(ctx, channel, subscriberID, wrapped, opts...)
}

// apply runs the rules and reports what happened. It returns the original
// payload unchanged when no rule matches or every write fails.
//
// The debug line is the only record of why a message looks the way it does:
// rules can live in any service's config on any host, so without it "why was
// this alert spoken?" has no answer.
func (m *rulesMessenger) apply(rules []Rule, side, channel, payloadType string, payload interface{}) interface{} {
	if len(rules) == 0 || payload == nil {
		return payload
	}

	out, changes, errs := ApplyRules(payloadType, payload, rules)

	for _, err := range errs {
		m.logger.Error("rules: could not apply rule",
			"service", m.service, "side", side, "channel", channel,
			"payloadType", payloadType, "error", err)
	}
	for _, change := range changes {
		m.logger.Debug("rules: rule changed a field",
			"service", m.service, "side", side, "channel", channel,
			"payloadType", payloadType, "rule", change.RuleIndex,
			"path", change.Path, "from", change.From, "to", change.To,
			"forced", change.Forced, "added", change.Added)
	}

	return out
}
