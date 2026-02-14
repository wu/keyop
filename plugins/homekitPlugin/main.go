package main

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"os"
	"path/filepath"

	"github.com/brutella/hc"
	"github.com/brutella/hc/accessory"
)

type HomekitPlugin struct {
	deps      core.Dependencies
	cfg       core.ServiceConfig
	accessory *accessory.Thermometer
	transport hc.Transport
}

func (p *HomekitPlugin) Initialize() error {
	logger := p.deps.MustGetLogger()
	logger.Info("HomekitPlugin initializing")

	info := accessory.Info{
		Name:         p.cfg.Name,
		Manufacturer: "Keyop",
	}

	acc := accessory.NewTemperatureSensor(info, 0, -50, 100, 0.1)
	p.accessory = acc

	storagePath := filepath.Join(os.TempDir(), fmt.Sprintf("hc_%s", p.cfg.Name))
	if customStorage, ok := p.cfg.Config["storagePath"].(string); ok {
		storagePath = customStorage
	}

	config := hc.Config{
		Pin:         "12344321",
		StoragePath: storagePath,
	}
	if pin, ok := p.cfg.Config["pin"].(string); ok {
		config.Pin = pin
	}

	t, err := hc.NewIPTransport(config, acc.Accessory)
	if err != nil {
		return fmt.Errorf("hc.NewIPTransport failed: %w", err)
	}
	p.transport = t

	go func() {
		logger.Info("Starting HomeKit transport")
		t.Start()
	}()

	// Subscribe to temp channel
	messenger := p.deps.MustGetMessenger()
	sub, ok := p.cfg.Subs["temp"]
	if !ok {
		return fmt.Errorf("homekit: temp subscription not configured")
	}

	err = messenger.Subscribe(p.deps.MustGetContext(), p.cfg.Name, sub.Name, p.cfg.Type, p.cfg.Name, sub.MaxAge, p.tempHandler)
	if err != nil {
		return fmt.Errorf("failed to subscribe to temp channel: %w", err)
	}

	logger.Info("HomekitPlugin initialized and subscribed to", "channel", sub.Name)
	return nil
}

func (p *HomekitPlugin) tempHandler(msg core.Message) error {
	logger := p.deps.MustGetLogger()
	// According to x/temp/service.go, msg.Metric contains TempF by default?
	// Actually x/temp/service.go sets Metric to TempF.
	// HomeKit expects Celsius.

	tempC := (msg.Metric - 32) * 5 / 9
	// If Data is present and is an Event map, we might get TempC directly
	if data, ok := msg.Data.(map[string]interface{}); ok {
		if tc, ok := data["TempC"].(float64); ok {
			tempC = tc
		} else if tc, ok := data["TempC"].(float32); ok {
			tempC = float64(tc)
		}
	}

	logger.Debug("HomeKit updating temperature", "celsius", tempC)
	p.accessory.TempSensor.CurrentTemperature.SetValue(tempC)

	return nil
}

func (p *HomekitPlugin) Check() error {
	return nil
}

func (p *HomekitPlugin) ValidateConfig() []error {
	logger := p.deps.MustGetLogger()
	errs := util.ValidateConfig("subs", p.cfg.Subs, []string{"temp"}, logger)
	return errs
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &HomekitPlugin{
		deps: deps,
		cfg:  cfg,
	}
}
