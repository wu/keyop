package core

import (
	"context"

	km "github.com/wu/keyop-messenger"
)

// SubscribeOptions translates the channel's delivery-tuning fields into
// keyop-messenger SubscribeOptions. It is the single place that maps config to
// options: adding a new per-subscription option means extending this method and
// ChannelInfo, with no change to services. Returns nil when nothing is set.
func (ci ChannelInfo) SubscribeOptions() []km.SubscribeOption {
	var opts []km.SubscribeOption
	if ci.MaxAge > 0 {
		opts = append(opts, km.WithMaxAge(ci.MaxAge))
	}
	if ci.MaxRetries != nil {
		opts = append(opts, km.WithMaxRetries(*ci.MaxRetries))
	}
	if ci.RetryBackoffBase > 0 || ci.RetryBackoffMax > 0 {
		opts = append(opts, km.WithRetryBackoff(ci.RetryBackoffBase, ci.RetryBackoffMax))
	}
	return opts
}

// configMessenger decorates a MessengerApi so that a service's per-channel
// SubscribeOptions (from its Subs config) are applied automatically on every
// Subscribe call, without the service having to read or pass them. Every method
// except Subscribe is promoted unchanged from the embedded MessengerApi, so the
// decorator is a transparent stand-in for the underlying messenger.
type configMessenger struct {
	MessengerApi
	// subOpts maps a configured channel Name to the options derived from its
	// ChannelInfo. Only channels with at least one option appear here.
	subOpts map[string][]km.SubscribeOption
}

// NewConfigMessenger wraps inner so that Subscribe calls for any channel whose
// Name appears in subs automatically receive that channel's SubscribeOptions.
// Channels with no options (or an empty Name) are not indexed, so subscriptions
// to unconfigured channels pass through unchanged. If inner already has no
// options to apply the wrapper is still returned; it is cheap and transparent.
func NewConfigMessenger(inner MessengerApi, subs map[string]ChannelInfo) MessengerApi {
	subOpts := make(map[string][]km.SubscribeOption)
	for _, ci := range subs {
		if ci.Name == "" {
			continue
		}
		if o := ci.SubscribeOptions(); len(o) > 0 {
			subOpts[ci.Name] = o
		}
	}
	return &configMessenger{MessengerApi: inner, subOpts: subOpts}
}

// Subscribe applies the configured options for channel (if any) ahead of the
// caller-supplied opts, then delegates to the wrapped messenger. Caller-supplied
// options come last so an explicit per-call option overrides the config default.
func (m *configMessenger) Subscribe(ctx context.Context, channel, subscriberID string, handler km.HandlerFunc, opts ...km.SubscribeOption) error {
	if cfgOpts, ok := m.subOpts[channel]; ok {
		merged := make([]km.SubscribeOption, 0, len(cfgOpts)+len(opts))
		merged = append(merged, cfgOpts...)
		merged = append(merged, opts...)
		opts = merged
	}
	return m.MessengerApi.Subscribe(ctx, channel, subscriberID, handler, opts...)
}
