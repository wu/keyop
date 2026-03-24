package thermostat

import (
	"fmt"
	"keyop/core"
)

// Event describes a thermostat payload containing current temperature and control state.
type Event struct {
	HeaterTargetState string  `json:"heaterTargetState"`
	CoolerTargetState string  `json:"coolerTargetState"`
	Temp              float64 `json:"temp"`
	MinTemp           float64 `json:"minTemp"`
	MaxTemp           float64 `json:"maxTemp"`
	Mode              string  `json:"mode"`
	Hysteresis        float64 `json:"hysteresis,omitempty"`
}

// PayloadType returns the canonical payload type identifier for Event.
func (e Event) PayloadType() string {
	return "thermostat.event.v1"
}

// Name returns the service name for the PayloadProvider interface.
func (svc *Service) Name() string {
	return svc.Cfg.Name
}

// RegisterPayloads registers the thermostat payload types with the provided registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	if err := reg.Register("thermostat.event.v1", func() any { return &Event{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register thermostat.event.v1: %w", err)
		}
	}
	if err := reg.Register("thermostat", func() any { return &Event{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register thermostat alias: %w", err)
		}
	}
	return nil
}
