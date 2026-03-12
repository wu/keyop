package tides

import "keyop/core"

// TideAlertEvent represents an alert about a tide peak or extreme-window status.
// It carries the station ID, the peak details, the window (for extreme alerts),
// and the previous record when applicable.
//
// Payload type name chosen to follow the service.*.v1 convention.
type TideAlertEvent struct {
	core.AlertEvent `json:",omitempty"`
	StationID       string            `json:"stationId"`
	Window          string            `json:"window,omitempty"`
	Peak            TidePeak          `json:"peak"`
	Previous        *TideExtremeEntry `json:"previous,omitempty"`
}

// PayloadType returns the canonical payload type for tide alerts.
func (e TideAlertEvent) PayloadType() string { return "service.tides.tideAlert.v1" }

// RegisterPayloads registers the tide alert payload with the provided registry.
// It follows the same duplicate-registration handling pattern as other services.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	if err := reg.Register("tide_alert", func() any { return &TideAlertEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return err
		}
	}
	if err := reg.Register("service.tides.tideAlert.v1", func() any { return &TideAlertEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return err
		}
	}
	return nil
}
