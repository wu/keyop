package switchgpio

import (
	"fmt"
	"keyop/core"
)

// Event describes a switch payload containing the device name and current pin state.
type Event struct {
	DeviceName string `json:"deviceName"`
	State      string `json:"state"` // "ON" or "OFF"
}

// PayloadType returns the canonical payload type identifier for Event.
func (e Event) PayloadType() string {
	return "switch.event.v1"
}

// Name returns the service name for the PayloadProvider interface.
func (svc *Service) Name() string {
	return svc.Cfg.Name
}

// RegisterPayloads registers the switch payload types with the provided registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	if err := reg.Register("switch.event.v1", func() any { return &Event{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register switch.event.v1: %w", err)
		}
	}
	return nil
}
