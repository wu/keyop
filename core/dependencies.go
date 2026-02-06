package core

import (
	"context"
)

type Dependencies struct {
	logger    Logger
	os        OsProviderApi
	messenger MessengerApi
	state     StateStore
	context   context.Context
	cancel    context.CancelFunc
}

func (d *Dependencies) SetStateStore(state StateStore) {
	d.state = state
}

func (d *Dependencies) MustGetStateStore() StateStore {
	if d.state == nil {
		panic("ERROR: State store is not initialized")
	}
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
	d.context = ctx
}

func (d *Dependencies) MustGetContext() context.Context {
	if d.context == nil {
		panic("ERROR: Context is not initialized")
	}
	return d.context
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

func (d *Dependencies) SetMessenger(messenger MessengerApi) {
	d.messenger = messenger
}

func (d *Dependencies) MustGetMessenger() MessengerApi {
	if d.messenger == nil {
		panic("ERROR: Messenger is not initialized")
	}
	return d.messenger
}
