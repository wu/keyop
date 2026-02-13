package main

import (
	"keyop/core"
)

type HelloWorldPlugin struct {
	deps core.Dependencies
	cfg  core.ServiceConfig
}

func (p *HelloWorldPlugin) Initialize() error {
	p.deps.MustGetLogger().Info("HelloWorldPlugin initialized")
	return nil
}

func (p *HelloWorldPlugin) Check() error {
	p.deps.MustGetLogger().Info("HelloWorldPlugin check: Hello World!")
	return nil
}

func (p *HelloWorldPlugin) ValidateConfig() []error {
	return nil
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &HelloWorldPlugin{
		deps: deps,
		cfg:  cfg,
	}
}
