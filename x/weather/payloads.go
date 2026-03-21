package weather

import (
	"keyop/core"
	"time"
)

// Compile-time interface assertion.
var _ core.PayloadProvider = (*Service)(nil)

// ForecastPrecip holds the probability of precipitation for a forecast period.
type ForecastPrecip struct {
	Value    *float64 `json:"value"`
	UnitCode string   `json:"unitCode"`
}

// ForecastPeriod represents a single NWS forecast period (typically 12 hours).
type ForecastPeriod struct {
	Number                     int            `json:"number"`
	Name                       string         `json:"name"`
	StartTime                  string         `json:"startTime"`
	EndTime                    string         `json:"endTime"`
	IsDaytime                  bool           `json:"isDaytime"`
	Temperature                float64        `json:"temperature"`
	TemperatureUnit            string         `json:"temperatureUnit"`
	TemperatureTrend           string         `json:"temperatureTrend"`
	ProbabilityOfPrecipitation ForecastPrecip `json:"probabilityOfPrecipitation"`
	WindSpeed                  string         `json:"windSpeed"`
	WindDirection              string         `json:"windDirection"`
	ShortForecast              string         `json:"shortForecast"`
	DetailedForecast           string         `json:"detailedForecast"`
}

// ForecastEvent is the typed payload for a weather_forecast event.
type ForecastEvent struct {
	Periods   []ForecastPeriod `json:"periods"`
	FetchedAt time.Time        `json:"fetchedAt"`
}

// PayloadType returns the canonical payload type for weather forecast events.
func (e ForecastEvent) PayloadType() string { return "service.weather.v1" }

// Name satisfies the core.PayloadProvider interface.
func (svc *Service) Name() string { return "weather" }

// RegisterPayloads registers weather payload types with the provided registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	types := map[string]func() any{
		"service.weather.v1": func() any { return &ForecastEvent{} },
		"weather_forecast":   func() any { return &ForecastEvent{} },
	}
	for name, factory := range types {
		if err := reg.Register(name, factory); err != nil {
			if !core.IsDuplicatePayloadRegistration(err) {
				return err
			}
		}
	}
	return nil
}
