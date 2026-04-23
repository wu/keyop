//nolint:revive
package core

import (
	"context"

	km "github.com/wu/keyop-messenger"
)

// MessengerApi is the interface for the new keyop-messenger library.
type MessengerApi interface {
	Publish(ctx context.Context, channel string, payloadType string, payload interface{}) error
	RegisterPayloadType(typeStr string, prototype interface{}) error
	Subscribe(ctx context.Context, channel string, subscriberID string, handler km.HandlerFunc) error
	InstanceName() string
	Close() error
}

type Dependencies struct {
	logger    Logger
	os        OsProviderApi
	messenger MessengerApi
	state     StateStoreApi
	ctx       context.Context
	cancel    context.CancelFunc
}

func (d *Dependencies) SetStateStore(state StateStoreApi) {
	d.state = state
}

func (d *Dependencies) MustGetStateStore() StateStoreApi {
	if d.state == nil {
		panic("ERROR: State store is not initialized")
	}
	return d.state
}

// GetStateStore returns the state store if set, otherwise nil.
func (d *Dependencies) GetStateStore() StateStoreApi {
	return d.state
}

func (d *Dependencies) SetLogger(logger Logger) {
	d.logger = logger
}

func (d *Dependencies) MustGetLogger() Logger {
	if d.logger == nil {
		panic("ERROR: Logger is not initialized")
	}
	return d.logger
}

func (d *Dependencies) SetContext(ctx context.Context) {
	d.ctx = ctx
}

func (d *Dependencies) MustGetContext() context.Context {
	if d.ctx == nil {
		panic("ERROR: Context is not initialized")
	}
	return d.ctx
}

func (d *Dependencies) SetCancel(cancel context.CancelFunc) {
	d.cancel = cancel
}

func (d *Dependencies) MustGetCancel() context.CancelFunc {
	if d.cancel == nil {
		panic("ERROR: Cancel function is not initialized")
	}
	return d.cancel
}

func (d *Dependencies) SetOsProvider(os OsProviderApi) {
	d.os = os
}

func (d *Dependencies) MustGetOsProvider() OsProviderApi {
	if d.os == nil {
		panic("ERROR: OS provider is not initialized")
	}
	return d.os
}

// SetMessenger stores the keyop-messenger instance.
func (d *Dependencies) SetMessenger(m MessengerApi) {
	d.messenger = m
}

// MustGetMessenger returns the keyop-messenger instance, or panics if not available.
func (d *Dependencies) MustGetMessenger() MessengerApi {
	if d.messenger == nil {
		panic("ERROR: messenger is not initialized")
	}
	return d.messenger
}
