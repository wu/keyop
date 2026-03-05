package core

import (
	"context"
	"time"
)

// PreprocessMessenger wraps a MessengerApi and applies sub_preprocess rules on Subscribe
// and pub_preprocess rules on Send.
//
// sub_preprocess: When a subscribed message is received, conditions are evaluated. If any
// condition matches, its Updates are merged into the message (last-write-wins across all
// matching conditions) and the modified message is forwarded to the real handler. If no
// condition matches, the message is dropped (not forwarded).
//
// pub_preprocess: When Send is called, conditions are evaluated against the outgoing
// message. For every condition that matches, its Updates are merged (cumulatively) and
// the resulting message is sent. If no condition matches, the original message is sent
// unchanged.
type PreprocessMessenger struct {
	inner         MessengerApi
	subConditions []ConditionConfig
	pubConditions []ConditionConfig
}

// NewPreprocessMessenger creates a PreprocessMessenger wrapping inner.
// Pass nil or empty slices for conditions that should be inactive.
func NewPreprocessMessenger(inner MessengerApi, subConditions []ConditionConfig, pubConditions []ConditionConfig) *PreprocessMessenger {
	return &PreprocessMessenger{
		inner:         inner,
		subConditions: subConditions,
		pubConditions: pubConditions,
	}
}

// Send applies pub_preprocess rules and then sends the (potentially modified) message.
// If pub_preprocess conditions are defined and none match, the original message is sent unchanged.
// If one or more conditions match, only the matching messages are sent (one per match, each with
// accumulated updates).
func (p *PreprocessMessenger) Send(msg Message) error {
	if len(p.pubConditions) == 0 {
		return p.inner.Send(msg)
	}

	results := ApplyConditions(msg, p.pubConditions)
	if len(results) == 0 {
		// No conditions matched — send the original message unmodified.
		return p.inner.Send(msg)
	}

	var lastErr error
	for _, m := range results {
		if err := p.inner.Send(m); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// Subscribe wraps the provided messageHandler so that sub_preprocess conditions are
// evaluated before the handler is invoked. If no conditions are configured, the handler
// is called directly. If conditions are configured and none match, the message is silently
// dropped.
func (p *PreprocessMessenger) Subscribe(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(Message) error) error {
	if len(p.subConditions) == 0 {
		return p.inner.Subscribe(ctx, sourceName, channelName, serviceType, serviceName, maxAge, messageHandler)
	}

	wrappedHandler := func(msg Message) error {
		processed, ok := ApplySubPreprocess(msg, p.subConditions)
		if !ok {
			return nil
		}
		return messageHandler(processed)
	}

	return p.inner.Subscribe(ctx, sourceName, channelName, serviceType, serviceName, maxAge, wrappedHandler)
}

// SubscribeExtended wraps the handler with sub_preprocess logic.
func (p *PreprocessMessenger) SubscribeExtended(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(Message, string, int64) error) error {
	if len(p.subConditions) == 0 {
		return p.inner.SubscribeExtended(ctx, source, channelName, serviceType, serviceName, maxAge, messageHandler)
	}

	wrappedHandler := func(msg Message, fileName string, offset int64) error {
		processed, ok := ApplySubPreprocess(msg, p.subConditions)
		if !ok {
			return nil
		}
		return messageHandler(processed, fileName, offset)
	}

	return p.inner.SubscribeExtended(ctx, source, channelName, serviceType, serviceName, maxAge, wrappedHandler)
}

func (p *PreprocessMessenger) SetReaderState(channelName string, readerName string, fileName string, offset int64) error {
	return p.inner.SetReaderState(channelName, readerName, fileName, offset)
}

func (p *PreprocessMessenger) SeekToEnd(channelName string, readerName string) error {
	return p.inner.SeekToEnd(channelName, readerName)
}

func (p *PreprocessMessenger) SetDataDir(dir string) {
	p.inner.SetDataDir(dir)
}

func (p *PreprocessMessenger) SetHostname(hostname string) {
	p.inner.SetHostname(hostname)
}

func (p *PreprocessMessenger) GetStats() MessengerStats {
	return p.inner.GetStats()
}

func (p *PreprocessMessenger) GetPayloadRegistry() PayloadRegistry {
	return p.inner.GetPayloadRegistry()
}

func (p *PreprocessMessenger) SetPayloadRegistry(reg PayloadRegistry) {
	p.inner.SetPayloadRegistry(reg)
}
