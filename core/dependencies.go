//nolint:revive
package core

import (
	"context"
)

type Dependencies struct {
	logger    Logger
	os        OsProviderApi
	messenger MessengerApi
	state     StateStore
	ctx       context.Context
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

// GetStateStore returns the state store if set, otherwise nil.
func (d *Dependencies) GetStateStore() StateStore {
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

func (d *Dependencies) SetMessenger(messenger MessengerApi) {
	d.messenger = messenger
}

func (d *Dependencies) MustGetMessenger() MessengerApi {
	if d.messenger == nil {
		panic("ERROR: Messenger is not initialized")
	}
	return d.messenger
}

// Clone returns a shallow copy of Dependencies. This is useful when you want to override
// a single field (e.g. the messenger) for a specific service without affecting the global deps.
func (d *Dependencies) Clone() Dependencies {
	return Dependencies{
		logger:    d.logger,
		os:        d.os,
		messenger: d.messenger,
		state:     d.state,
		ctx:       d.ctx,
		cancel:    d.cancel,
	}
}

// ParsePreprocessConditions extracts sub_preprocess and pub_preprocess condition lists
// from a ServiceConfig's Config map.
func ParsePreprocessConditions(cfg ServiceConfig) (subConditions []ConditionConfig, pubConditions []ConditionConfig) {
	if raw, ok := cfg.Config["sub_preprocess"].([]interface{}); ok {
		subConditions = ParseConditions(raw)
	}
	if raw, ok := cfg.Config["pub_preprocess"].([]interface{}); ok {
		pubConditions = ParseConditions(raw)
	}
	return
}
