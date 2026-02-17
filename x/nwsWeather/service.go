package nwsWeather

import (
	"encoding/json"
	"fmt"
	"io"
	"keyop/core"
	"keyop/util"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	Deps        core.Dependencies
	Cfg         core.ServiceConfig
	Lat         float64
	Lon         float64
	cachedLat   *float64
	cachedLon   *float64
	apiBaseURL  string
	forecastURL string
	mu          sync.RWMutex
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps:       deps,
		Cfg:        cfg,
		apiBaseURL: "https://api.weather.gov",
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
	errs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"events"}, logger)
	errs = append(errs, util.ValidateConfig("subs", svc.Cfg.Subs, []string{"gps"}, logger)...)

	if _, ok := svc.Cfg.Config["lat"].(float64); !ok {
		errs = append(errs, fmt.Errorf("nwsWeather: lat not set or not a float in config"))
	}
	if _, ok := svc.Cfg.Config["lon"].(float64); !ok {
		errs = append(errs, fmt.Errorf("nwsWeather: lon not set or not a float in config"))
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
		if svc.cachedLat == nil || svc.cachedLon == nil || *svc.cachedLat != lat || *svc.cachedLon != lon {
			svc.forecastURL = ""
		}
		svc.cachedLat = &lat
		svc.cachedLon = &lon
		svc.mu.Unlock()
		svc.Deps.MustGetLogger().Debug("nwsWeather: updated cached gps coordinates", "lat", lat, "lon", lon)
	}
	return nil
}

func (svc *Service) fetchForecastURL() error {
	svc.mu.RLock()
	apiBaseURL := svc.apiBaseURL
	lat := svc.Lat
	lon := svc.Lon
	if svc.cachedLat != nil && svc.cachedLon != nil {
		lat = *svc.cachedLat
		lon = *svc.cachedLon
	}
	svc.mu.RUnlock()

	url := fmt.Sprintf("%s/points/%.4f,%.4f", apiBaseURL, lat, lon)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "(keyop, https://github.com/keyop/keyop)")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("nwsWeather: failed to fetch points: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var data struct {
		Properties struct {
			Forecast string `json:"forecast"`
		} `json:"properties"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return err
	}

	if data.Properties.Forecast == "" {
		return fmt.Errorf("nwsWeather: forecast URL not found in response")
	}

	svc.mu.Lock()
	svc.forecastURL = data.Properties.Forecast
	svc.mu.Unlock()

	return nil
}

func (svc *Service) Check() error {
	svc.mu.RLock()
	forecastURL := svc.forecastURL
	svc.mu.RUnlock()

	if forecastURL == "" {
		if err := svc.fetchForecastURL(); err != nil {
			return err
		}
		svc.mu.RLock()
		forecastURL = svc.forecastURL
		svc.mu.RUnlock()
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", forecastURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "(keyop, https://github.com/keyop/keyop)")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// If forecast URL failed, maybe it expired or is wrong, try to refetch it next time
		svc.mu.Lock()
		svc.forecastURL = ""
		svc.mu.Unlock()
		return fmt.Errorf("nwsWeather: failed to fetch forecast: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var forecastData struct {
		Properties struct {
			Periods []map[string]interface{} `json:"periods"`
		} `json:"properties"`
	}

	if err := json.Unmarshal(body, &forecastData); err != nil {
		return err
	}

	if len(forecastData.Properties.Periods) == 0 {
		return fmt.Errorf("nwsWeather: no forecast periods found")
	}

	currentPeriod := forecastData.Properties.Periods[0]

	messenger := svc.Deps.MustGetMessenger()
	msg := core.Message{
		Uuid:        uuid.New().String(),
		ChannelName: svc.Cfg.Pubs["events"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Text:        fmt.Sprintf("%v", currentPeriod["detailedForecast"]),
		Summary:     fmt.Sprintf("Weather: %v, %v°%v", currentPeriod["shortForecast"], currentPeriod["temperature"], currentPeriod["temperatureUnit"]),
		Data:        currentPeriod,
	}

	return messenger.Send(msg)
}
