package core

import (
	"context"
)

type Dependencies struct {
	logger   Logger
	hostname string
	context  context.Context
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

func (d *Dependencies) SetHostname(hostname string) {
	d.hostname = hostname
}

func (d *Dependencies) MustGetHostname() string {
	if d.hostname == "" {
		panic("ERROR: Hostname is not initialized")
	}
	return d.hostname
}
