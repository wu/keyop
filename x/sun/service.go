package sun

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sj14/astral/pkg/astral"
)

type Service struct {
	Deps      core.Dependencies
	Cfg       core.ServiceConfig
	Lat       float64
	Lon       float64
	Alt       float64
	cachedLat *float64
	cachedLon *float64
	cachedAlt *float64
	timers    []*time.Timer
	mu        sync.RWMutex
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps:   deps,
		Cfg:    cfg,
		timers: make([]*time.Timer, 0),
	}

	if lat, ok := cfg.Config["lat"].(float64); ok {
		svc.Lat = lat
	}
	if lon, ok := cfg.Config["lon"].(float64); ok {
		svc.Lon = lon
	}
	if alt, ok := cfg.Config["alt"].(float64); ok {
		svc.Alt = alt
	}

	return svc
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"events", "alerts"}, logger)
	errs = append(errs, util.ValidateConfig("subs", svc.Cfg.Subs, []string{"gps"}, logger)...)

	if _, ok := svc.Cfg.Config["lat"].(float64); !ok {
		errs = append(errs, fmt.Errorf("sun: lat not set or not a float in config"))
	}
	if _, ok := svc.Cfg.Config["lon"].(float64); !ok {
		errs = append(errs, fmt.Errorf("sun: lon not set or not a float in config"))
	}
	// alt is optional, defaults to 0 if not provided in config or gps message

	return errs
}

func (svc *Service) Initialize() error {
	messenger := svc.Deps.MustGetMessenger()
	gpsChan, ok := svc.Cfg.Subs["gps"]
	if !ok {
		return fmt.Errorf("sun: gps subscription not configured")
	}

	if err := messenger.Subscribe(svc.Deps.MustGetContext(), svc.Cfg.Name, gpsChan.Name, svc.Cfg.Type, svc.Cfg.Name, gpsChan.MaxAge, svc.gpsHandler); err != nil {
		return err
	}

	svc.scheduleAlerts()
	return nil
}

func (svc *Service) gpsHandler(msg core.Message) error {
	data, ok := msg.Data.(map[string]interface{})
	if !ok {
		return nil
	}

	lat, okLat := data["lat"].(float64)
	lon, okLon := data["lon"].(float64)
	alt, okAlt := data["alt"].(float64)

	if okLat && okLon {
		svc.mu.Lock()
		svc.cachedLat = &lat
		svc.cachedLon = &lon
		if okAlt {
			svc.cachedAlt = &alt
		}
		svc.mu.Unlock()
		if okAlt {
			svc.Deps.MustGetLogger().Debug("sun: updated cached gps coordinates", "lat", lat, "lon", lon, "alt", alt)
		} else {
			svc.Deps.MustGetLogger().Debug("sun: updated cached gps coordinates", "lat", lat, "lon", lon)
		}
		svc.scheduleAlerts()
	}
	return nil
}

type SunEvents struct {
	Sunrise     time.Time `json:"sunrise"`
	Sunset      time.Time `json:"sunset"`
	CivilDawn   time.Time `json:"civil_dawn"`
	CivilDusk   time.Time `json:"civil_dusk"`
	DayLength   string    `json:"day_length"`
	NightLength string    `json:"night_length"`
}

func (svc *Service) Check() error {
	lat, lon, alt := svc.getObserverData()
	now := time.Now()
	logger := svc.Deps.MustGetLogger()
	logger.Info("Calculating sun events", "lat", lat, "lon", lon, "alt", alt, "time", now)

	events := svc.calculateSunEvents(lat, lon, alt, now)

	// Determine the next event
	nextEventName := ""
	nextEventTime := time.Time{}

	if events.CivilDawn.After(now) {
		nextEventName = "Dawn"
		nextEventTime = events.CivilDawn
	} else if events.Sunrise.After(now) {
		nextEventName = "Sunrise"
		nextEventTime = events.Sunrise
	} else if events.Sunset.After(now) {
		nextEventName = "Sunset"
		nextEventTime = events.Sunset
	} else if events.CivilDusk.After(now) {
		nextEventName = "Dusk"
		nextEventTime = events.CivilDusk
	} else {
		// All events today have passed, get tomorrow's dawn
		tomorrow := now.AddDate(0, 0, 1)
		tomorrowEvents := svc.calculateSunEvents(lat, lon, alt, tomorrow)
		nextEventName = "Dawn"
		nextEventTime = tomorrowEvents.CivilDawn
	}

	// Send event message
	msgUuid := uuid.New().String()
	messenger := svc.Deps.MustGetMessenger()

	eventMsg := core.Message{
		Uuid:        msgUuid,
		ChannelName: svc.Cfg.Pubs["events"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Text:        fmt.Sprintf("Next sun event: %s at %s", nextEventName, nextEventTime.Format("15:04")),
		Summary:     fmt.Sprintf("Next: %s %s", nextEventName, nextEventTime.Format("15:04")),
		Data:        events,
	}
	return messenger.Send(eventMsg)
}

func (svc *Service) scheduleAlerts() {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	// Cancel existing timers
	for _, t := range svc.timers {
		t.Stop()
	}
	svc.timers = nil

	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	lat, lon, alt := svc.getObserverDataLocked()
	now := time.Now()

	// We want to schedule alerts for dawn and dusk for today and tomorrow to be safe
	days := []time.Time{now, now.AddDate(0, 0, 1)}

	for _, t := range days {
		events := svc.calculateSunEvents(lat, lon, alt, t)

		schedule := func(eventTime time.Time, name string) {
			if eventTime.After(now) {
				duration := eventTime.Sub(now)
				logger.Debug("sun: scheduling alert", "event", name, "at", eventTime, "in", duration)
				timer := time.AfterFunc(duration, func() {
					messenger.Send(core.Message{
						Uuid:        uuid.New().String(),
						ChannelName: svc.Cfg.Pubs["alerts"].Name,
						ServiceName: svc.Cfg.Name,
						ServiceType: svc.Cfg.Type,
						Text:        fmt.Sprintf("Sun event: %s", name),
						Summary:     name,
					})
					// Reschedule after the alert fires to keep it going
					svc.scheduleAlerts()
				})
				svc.timers = append(svc.timers, timer)
			}
		}

		schedule(events.Sunrise, "sunrise")
		schedule(events.Sunset, "sunset")
		schedule(events.CivilDawn, "dawn")
		schedule(events.CivilDusk, "dusk")
	}
}

func (svc *Service) getObserverData() (float64, float64, float64) {
	svc.mu.RLock()
	defer svc.mu.RUnlock()
	return svc.getObserverDataLocked()
}

func (svc *Service) getObserverDataLocked() (float64, float64, float64) {
	lat, lon := svc.Lat, svc.Lon
	alt := svc.Alt

	if svc.cachedLat != nil && svc.cachedLon != nil {
		lat = *svc.cachedLat
		lon = *svc.cachedLon
	}
	if svc.cachedAlt != nil {
		alt = *svc.cachedAlt
	}
	return lat, lon, alt
}

// calculateSunEvents uses astral library to calculate sun events.
func (svc *Service) calculateSunEvents(lat, lon, alt float64, t time.Time) SunEvents {
	observer := astral.Observer{
		Latitude:  lat,
		Longitude: lon,
		Elevation: alt,
	}

	sunrise, _ := astral.Sunrise(observer, t)
	sunset, _ := astral.Sunset(observer, t)
	dawn, _ := astral.Dawn(observer, t, astral.DepressionCivil)
	dusk, _ := astral.Dusk(observer, t, astral.DepressionCivil)

	dayLength := dusk.Sub(dawn)

	// Night length (dusk to dawn next day)
	nextDawn, _ := astral.Dawn(observer, t.AddDate(0, 0, 1), astral.DepressionCivil)
	nightLength := nextDawn.Sub(dusk)

	return SunEvents{
		Sunrise:     sunrise,
		Sunset:      sunset,
		CivilDawn:   dawn,
		CivilDusk:   dusk,
		DayLength:   dayLength.String(),
		NightLength: nightLength.String(),
	}
}
