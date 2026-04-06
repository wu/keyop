package core

import (
	"context"

	km "github.com/wu/keyop-messenger"
)

// MessengerBridge subscribes to channels on the new messenger and republishes
// each message on the old messenger. This allows old-messenger services to
// receive messages published by already-migrated services during the incremental
// migration from core.Messenger to keyop-messenger.
//
// Direction: new messenger → old messenger.
//
// The bridge only operates on the channels listed in its configuration. Once all
// services on a given channel have been migrated to the new messenger, remove that
// channel from the bridge config and it will no longer be bridged.
type MessengerBridge struct {
	old      MessengerApi
	new      *km.Messenger
	channels []string
	logger   Logger
}

// NewMessengerBridge creates a bridge that will mirror the listed channels from
// the new messenger to the old messenger.
func NewMessengerBridge(old MessengerApi, newMsgr *km.Messenger, channels []string, logger Logger) *MessengerBridge {
	return &MessengerBridge{
		old:      old,
		new:      newMsgr,
		channels: channels,
		logger:   logger,
	}
}

// Start launches one goroutine per channel. Each goroutine subscribes to the new
// messenger and forwards received messages to the old messenger. The goroutines
// run until ctx is cancelled.
func (b *MessengerBridge) Start(ctx context.Context) {
	for _, ch := range b.channels {
		channel := ch
		go b.bridgeChannel(ctx, channel)
	}
}

func (b *MessengerBridge) bridgeChannel(ctx context.Context, channel string) {
	subscriberID := "bridge.new-to-old." + channel
	err := b.new.Subscribe(ctx, channel, subscriberID, func(_ context.Context, msg km.Message) error {
		coreMsg := bridgeConvertToCore(msg)
		if err := b.old.Send(coreMsg); err != nil {
			b.logger.Error("bridge: failed to relay message to old messenger",
				"channel", channel,
				"id", msg.ID,
				"payloadType", msg.PayloadType,
				"error", err,
			)
			return err
		}
		return nil
	})
	if err != nil {
		b.logger.Error("bridge: failed to subscribe to new messenger channel",
			"channel", channel,
			"error", err,
		)
	}
}

// bridgeConvertToCore converts a keyop-messenger Message to a core.Message.
// It applies field mappings for canonical core payload types so that old-messenger
// subscribers (e.g. graphite, alerts) receive correctly-populated fields.
// For unknown payload types, a minimal Message is returned with DataType and Data set.
func bridgeConvertToCore(msg km.Message) Message {
	base := Message{
		Uuid:        msg.ID,
		Timestamp:   msg.Timestamp,
		Hostname:    msg.Origin,
		ChannelName: msg.Channel,
		DataType:    msg.PayloadType,
		Data:        msg.Payload,
	}

	switch msg.PayloadType {
	case "core.metric.v1", "metric":
		if event, ok := msg.Payload.(*MetricEvent); ok && event != nil {
			base.Event = "metric"
			base.MetricName = event.Name
			base.Metric = event.Value
		}

	case "core.alert.v1", "alert":
		if event, ok := msg.Payload.(*AlertEvent); ok && event != nil {
			base.Event = "alert"
			base.Summary = event.Summary
			base.Text = event.Text
			base.Status = event.Level
		}

	case "core.status.v1", "status":
		if event, ok := msg.Payload.(*StatusEvent); ok && event != nil {
			base.Event = "status"
			base.Text = event.Details
			base.Status = event.Status
		}

	case "core.error.v1", "error":
		if event, ok := msg.Payload.(*ErrorEvent); ok && event != nil {
			base.Event = "error"
			base.Summary = event.Summary
			base.Text = event.Text
			base.Status = event.Level
		}

	case "core.temp.v1", "temp":
		if event, ok := msg.Payload.(*TempEvent); ok && event != nil {
			base.Event = "temp"
			base.Text = event.SensorName
		}

	case "core.device.status.v1", "device.status":
		if event, ok := msg.Payload.(*DeviceStatusEvent); ok && event != nil {
			base.Event = "device.status"
			base.Status = event.Status
		}
	}

	return base
}
