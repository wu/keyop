package aurora

import (
	"fmt"
	"time"

	"keyop/core"
)

// Event represents the payload sent with the 'aurora_check' event.
// It carries the likelihood (percentage), the location used for the check,
// and the forecast time from the NOAA feed.
type Event struct {
	Likelihood   int     `json:"likelihood"`
	Lat          float64 `json:"lat"`
	Lon          float64 `json:"lon"`
	ForecastTime string  `json:"forecast_time,omitempty"`
}

// PayloadType returns the canonical payload type for aurora events.
func (e Event) PayloadType() string { return "service.aurora.v1" }

// Forecast represents a multi-day aurora forecast payload. It contains the
// fetched timestamp, the source URL used, and the parsed forecast structure
// so consumers can render it directly.
type Forecast struct {
	FetchedAt time.Time       `json:"fetched_at"`
	SourceURL string          `json:"source_url,omitempty"`
	Lat       float64         `json:"lat,omitempty"`
	Lon       float64         `json:"lon,omitempty"`
	Data      *ParsedForecast `json:"data,omitempty"`
}

// PayloadType returns the canonical payload type for aurora forecasts.
func (f Forecast) PayloadType() string { return "service.aurora.forecast.v1" }

// RegisterPayloads registers aurora payload types with the provided registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	// Event payload (instant check)
	if err := reg.Register("aurora", func() any { return &Event{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("aurora: failed to register aurora alias: %w", err)
		}
	}
	if err := reg.Register("service.aurora.v1", func() any { return &Event{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("aurora: failed to register service.aurora.v1: %w", err)
		}
	}

	// Forecast payload (3-day / multi-day forecast)
	if err := reg.Register("aurora_forecast", func() any { return &Forecast{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("aurora: failed to register aurora_forecast alias: %w", err)
		}
	}
	if err := reg.Register("service.aurora.forecast.v1", func() any { return &Forecast{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("aurora: failed to register service.aurora.forecast.v1: %w", err)
		}
	}

	// Alert payload (reuse core.AlertEvent)
	if err := reg.Register("aurora_alert", func() any { return &core.AlertEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("aurora: failed to register aurora_alert alias: %w", err)
		}
	}
	if err := reg.Register("service.aurora.alert.v1", func() any { return &core.AlertEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("aurora: failed to register service.aurora.alert.v1: %w", err)
		}
	}

	return nil
}
