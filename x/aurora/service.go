package aurora

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"sync"
)

type Service struct {
	Deps      core.Dependencies
	Cfg       core.ServiceConfig
	Lat       float64
	Lon       float64
	cachedLat *float64
	cachedLon *float64
	apiURL    string
	mu        sync.RWMutex
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps:   deps,
		Cfg:    cfg,
		apiURL: DefaultApiURL,
	}

	if lat, ok := cfg.Config["lat"].(float64); ok {
		svc.Lat = lat
	}
	if lon, ok := cfg.Config["lon"].(float64); ok {
		svc.Lon = lon
	}

	return svc
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"events", "alerts"}, logger)
	errs = append(errs, util.ValidateConfig("subs", svc.Cfg.Subs, []string{"gps"}, logger)...)

	if _, ok := svc.Cfg.Config["lat"].(float64); !ok {
		errs = append(errs, fmt.Errorf("aurora: lat not set or not a float in config"))
	}
	if _, ok := svc.Cfg.Config["lon"].(float64); !ok {
		errs = append(errs, fmt.Errorf("aurora: lon not set or not a float in config"))
	}

	return errs
}

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

func (svc *Service) Check() error {
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

	// Send event each time Check() gets run
	eventMsg := core.Message{
		ChannelName: svc.Cfg.Pubs["events"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Text:        fmt.Sprintf("Aurora likelihood: %d%%", bestProb),
		Summary:     fmt.Sprintf("Aurora: %d%%", bestProb),
		Data: map[string]interface{}{
			"likelihood":    bestProb,
			"lat":           lat,
			"lon":           lon,
			"forecast_time": data.ForecastTime,
		},
	}
	if err := messenger.Send(eventMsg); err != nil {
		return err
	}

	// Send an alert if the possibility is greater than zero
	if bestProb > 0 {
		alertMsg := core.Message{
			ChannelName: svc.Cfg.Pubs["alerts"].Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Text:        fmt.Sprintf("Aurora alert! Likelihood is %d%% at your location.", bestProb),
			Summary:     fmt.Sprintf("Aurora Alert: %d%%", bestProb),
			Data: map[string]interface{}{
				"likelihood": bestProb,
			},
		}
		if err := messenger.Send(alertMsg); err != nil {
			return err
		}
	}

	return nil
}
