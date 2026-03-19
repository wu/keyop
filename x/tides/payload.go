package tides

import "keyop/core"

// Compile-time interface assertions.
var _ core.PayloadProvider = (*Service)(nil)

// TideEvent represents the payload sent with the 'tide' event. It carries the
// station identifier, the current reading, an optional next reading and peak,
// and optional low-tide periods used by the web UI.
type TideEvent struct {
	StationID string          `json:"stationId"`
	Current   TideRecord      `json:"current"`
	State     string          `json:"state"`
	Next      *TideRecord     `json:"next,omitempty"`
	NextPeak  *TidePeak       `json:"nextPeak,omitempty"`
	Periods   []LowTidePeriod `json:"periods,omitempty"`
	Threshold float64         `json:"threshold,omitempty"`
}

// PayloadType returns the canonical payload type for tide events.
func (e TideEvent) PayloadType() string { return "service.tide.v1" }

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

// Name satisfies the core.PayloadProvider interface.
func (svc *Service) Name() string { return "tides" }

// RegisterPayloads registers the tide payloads with the provided registry.
// It follows the same duplicate-registration handling pattern as other services.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	// Register the main tide event payload (canonical and legacy alias).
	if err := reg.Register("tide", func() any { return &TideEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return err
		}
	}
	if err := reg.Register("service.tide.v1", func() any { return &TideEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return err
		}
	}

	// Register the tide alert payload (existing behavior).
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
