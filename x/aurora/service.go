package aurora

import (
	"database/sql"
	"fmt"
	"keyop/core"
	"keyop/util"
	"sync"
	"time"
)

// Service polls the NOAA aurora API on a schedule and publishes aurora activity events and metrics to subscribers.
type Service struct {
	Deps      core.Dependencies
	Cfg       core.ServiceConfig
	Lat       float64
	Lon       float64
	cachedLat *float64
	cachedLon *float64
	apiURL    string
	// apiForecastURL is the endpoint used to fetch multi-day forecasts. It may be
	// overridden by configuration (cfg.Config["forecast_url"]).
	apiForecastURL string
	mu             sync.RWMutex

	// db is a pointer to the sqlite DB managed by the sqlite service. It is
	// populated at runtime if a sqlite service is configured and accepts
	// the aurora payload types.
	db **sql.DB
}

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps:           deps,
		Cfg:            cfg,
		apiURL:         DefaultAPIURL,
		apiForecastURL: DefaultForecastAPIURL,
	}

	if lat, ok := cfg.Config["lat"].(float64); ok {
		svc.Lat = lat
	}
	if lon, ok := cfg.Config["lon"].(float64); ok {
		svc.Lon = lon
	}
	if fu, ok := cfg.Config["forecast_url"].(string); ok && fu != "" {
		svc.apiForecastURL = fu
	}

	return svc
}

// Name returns the canonical service type name for aurora (implements core.PayloadProvider).
func (svc *Service) Name() string { return "aurora" }

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("subs", svc.Cfg.Subs, []string{"gps"}, logger)

	if _, ok := svc.Cfg.Config["lat"].(float64); !ok {
		errs = append(errs, fmt.Errorf("aurora: lat not set or not a float in config"))
	}
	if _, ok := svc.Cfg.Config["lon"].(float64); !ok {
		errs = append(errs, fmt.Errorf("aurora: lon not set or not a float in config"))
	}

	return errs
}

// Initialize performs one-time startup required by the service (resource loading or connectivity checks).
func (svc *Service) Initialize() error {
	messenger := svc.Deps.MustGetMessenger()
	gpsChan, ok := svc.Cfg.Subs["gps"]
	if !ok {
		return nil
	}

	if err := messenger.Subscribe(svc.Deps.MustGetContext(), svc.Cfg.Name, gpsChan.Name, svc.Cfg.Type, svc.Cfg.Name, gpsChan.MaxAge, svc.gpsHandler); err != nil {
		return err
	}

	return nil
}

func (svc *Service) gpsHandler(msg core.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return nil
	}

	lat, okLat := data["lat"].(float64)
	lon, okLon := data["lon"].(float64)

	if okLat && okLon {
		svc.mu.Lock()
		svc.cachedLat = &lat
		svc.cachedLon = &lon
		svc.mu.Unlock()
		svc.Deps.MustGetLogger().Debug("aurora: updated cached gps coordinates", "lat", lat, "lon", lon)
	}
	return nil
}

// Check performs the service's periodic work: collect data, evaluate state, and publish messages/metrics.
func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()

	svc.mu.RLock()
	lat := svc.Lat
	lon := svc.Lon
	if svc.cachedLat != nil && svc.cachedLon != nil {
		lat = *svc.cachedLat
		lon = *svc.cachedLon
	}
	svc.mu.RUnlock()

	data, err := FetchOvationData(svc.apiURL)
	if err != nil {
		return err
	}

	bestProb := data.FindProbability(lat, lon)

	messenger := svc.Deps.MustGetMessenger()

	// Send event each time Check() gets run using a typed Event payload
	auroraData := Event{
		Likelihood:   bestProb,
		Lat:          lat,
		Lon:          lon,
		ForecastTime: data.ForecastTime,
	}
	eventMsg := core.Message{
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "aurora_check",
		Text:        fmt.Sprintf("Aurora likelihood: %d%%", bestProb),
		Summary:     fmt.Sprintf("Aurora: %d%%", bestProb),
		Data:        auroraData,
	}
	if err := messenger.Send(eventMsg); err != nil {
		return err
	}

	// Send an alert if the possibility is greater than zero using core.AlertEvent
	if bestProb > 0 {
		alert := core.AlertEvent{
			Summary: fmt.Sprintf("Aurora Alert: %d%%", bestProb),
			Text:    fmt.Sprintf("Aurora alert! Likelihood is %d%% at your location.", bestProb),
		}
		alertMsg := core.Message{
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "aurora_alert",
			Text:        alert.Text,
			Summary:     alert.Summary,
			Data:        alert,
		}
		if err := messenger.Send(alertMsg); err != nil {
			return err
		}
	}

	// Attempt to fetch the multi-day forecast and publish it as a typed Forecast payload.
	if svc.apiForecastURL != "" {
		logger.Warn("aurora: fetching 3-day forecast")
		if body, err := FetchOvationForecast(svc.apiForecastURL); err == nil {
			lat := svc.Lat
			lon := svc.Lon
			if svc.cachedLat != nil {
				lat = *svc.cachedLat
			}
			if svc.cachedLon != nil {
				lon = *svc.cachedLon
			}
			fc := Forecast{
				FetchedAt: time.Now(),
				SourceURL: svc.apiForecastURL,
				Lat:       lat,
				Lon:       lon,
			}
			// Try to parse plain-text 3-day forecast into structured data
			if pf, perr := Parse3DayForecastText(body); perr == nil && pf != nil && len(pf.Table) > 0 {
				fc.Data = pf

				// Extract and check for high G events (> G3)
				currentGEvents, _ := extractGEvents(pf)
				if len(currentGEvents) > 0 {
					previousGEvents, _ := svc.loadPreviousGEvents()
					newGEvents := filterNewEvents(currentGEvents, previousGEvents)

					// If there are new G events, send an alert
					if len(newGEvents) > 0 {
						highest := findHighestGEvent(newGEvents)
						if highest != nil {
							alertText := fmt.Sprintf(
								"Aurora alert! %s event predicted on %s from %s to %s UTC",
								highest.GScale,
								highest.StartTime.Format("Jan 2"),
								highest.StartTime.Format("15:04"),
								highest.EndTime.Format("15:04"),
							)
							alertMsg := core.Message{
								ChannelName: svc.Cfg.Name,
								ServiceName: svc.Cfg.Name,
								ServiceType: svc.Cfg.Type,
								Event:       "aurora_alert",
								Text:        alertText,
								Summary:     fmt.Sprintf("Aurora Alert: %s predicted", highest.GScale),
								Data: core.AlertEvent{
									Summary: fmt.Sprintf("Aurora Alert: %s predicted", highest.GScale),
									Text:    alertText,
								},
							}
							logger.Warn("aurora: send G-event alert", "text", alertText)
							if err := messenger.Send(alertMsg); err != nil {
								logger.Debug("aurora: failed to send G-event alert", "err", err)
							}
						}
					}

					// Save current G events for next comparison
					if err := svc.savePreviousGEvents(currentGEvents); err != nil {
						logger.Debug("aurora: failed to save G-events to state store", "err", err)
					}
				}
			}

			forecastMsg := core.Message{
				ChannelName: svc.Cfg.Name,
				ServiceName: svc.Cfg.Name,
				ServiceType: svc.Cfg.Type,
				Event:       "aurora_forecast",
				Text:        "Aurora 3-day forecast updated",
				Summary:     "Aurora forecast",
				Data:        fc,
			}
			logger.Warn("aurora: send 3-day forecast event", "data", fc)
			if err := messenger.Send(forecastMsg); err != nil {
				svc.Deps.MustGetLogger().Debug("aurora: failed to send forecast message", "err", err)
			}
		} else {
			logger.Warn("aurora: failed to fetch 3-day forecast", "err", err)
		}
	}

	return nil
}
