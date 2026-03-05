package main

import (
	"keyop/core"
)

type GreetingPayload struct {
	Message string `json:"message"`
	From    string `json:"from"`
}

func (p GreetingPayload) PayloadType() string { return "plugin.helloWorld.greeting.v1" }

type HelloWorldPlugin struct {
	deps core.Dependencies
	cfg  core.ServiceConfig
}

func (p *HelloWorldPlugin) Name() string {
	return "helloWorldPlugin"
}

func (p *HelloWorldPlugin) RegisterPayloads(reg core.PayloadRegistry) error {
	return reg.Register("plugin.helloWorld.greeting.v1", func() any { return &GreetingPayload{} })
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
