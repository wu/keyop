// Package sun computes solar position and daylight events used for scheduling and automation rules.
package sun

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"math"
	"sync"
	"time"

	"github.com/sj14/astral/pkg/astral"
)

// Service calculates sun times (sunrise, sunset) and publishes events used by scheduling subsystems.
type Service struct {
	Deps         core.Dependencies
	Cfg          core.ServiceConfig
	Lat          float64
	Lon          float64
	Alt          float64
	cachedLat    *float64
	cachedLon    *float64
	cachedAlt    *float64
	cachedEvents Events
	cachedAt     time.Time
	cacheTTL     time.Duration
	timers       []*time.Timer
	mu           sync.RWMutex
}

const defaultSunCacheTTL = 15 * time.Minute

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps:     deps,
		Cfg:      cfg,
		cacheTTL: defaultSunCacheTTL,
		timers:   make([]*time.Timer, 0),
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

// Name satisfies the core.PayloadProvider interface.
func (svc *Service) Name() string { return "sun" }

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("subs", svc.Cfg.Subs, []string{"gps"}, logger)

	if _, ok := svc.Cfg.Config["lat"].(float64); !ok {
		errs = append(errs, fmt.Errorf("sun: lat not set or not a float in config"))
	}
	if _, ok := svc.Cfg.Config["lon"].(float64); !ok {
		errs = append(errs, fmt.Errorf("sun: lon not set or not a float in config"))
	}
	// alt is optional, defaults to 0 if not provided in config or gps message

	return errs
}

// RegisterPayloads registers the sun payload types with the provided registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	if err := reg.Register("sun", func() any { return &SunEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register sun alias: %w", err)
		}
	}
	if err := reg.Register("service.sun.v1", func() any { return &SunEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register service.sun.v1: %w", err)
		}
	}
	return nil
}

// Initialize performs one-time startup required by the service (resource loading or connectivity checks).
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
		// Invalidate cached events so expensive recomputation runs on next scheduled refresh
		svc.cachedEvents = Events{}
		svc.cachedAt = time.Time{}
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

// Events contains computed solar times (dawn, sunrise, sunset, dusk) for a given date and location.
//
//nolint:revive
type Events struct {
	Sunrise     time.Time `json:"sunrise"`
	Sunset      time.Time `json:"sunset"`
	CivilDawn   time.Time `json:"civil_dawn"`
	CivilDusk   time.Time `json:"civil_dusk"`
	DayLength   string    `json:"day_length"`
	NightLength string    `json:"night_length"`
}

// SunEvent is a typed payload sent on sun events.
//
//nolint:revive
type SunEvent struct {
	Now             time.Time `json:"now"`
	Sunrise         time.Time `json:"sunrise"`
	Sunset          time.Time `json:"sunset"`
	CivilDawn       time.Time `json:"civil_dawn"`
	CivilDusk       time.Time `json:"civil_dusk"`
	DayLength       string    `json:"day_length"`
	NightLength     string    `json:"night_length"`
	TomorrowSunrise time.Time `json:"tomorrow_sunrise"`
	TomorrowDawn    time.Time `json:"tomorrow_dawn"`
	// NextEquinoxSolstice is the name of the next solstice/equinox (e.g. "Summer Solstice").
	NextEquinoxSolstice string `json:"next_equinox_solstice"`
	// NextEquinoxSolsticeDays is days until the next event (0 = today).
	NextEquinoxSolsticeDays int `json:"next_equinox_solstice_days"`
}

// PayloadType returns the registered payload type name for SunEvent.
func (s SunEvent) PayloadType() string { return "service.sun.v1" }

// Check performs the service's periodic work: collect data, evaluate state, and publish messages/metrics.
func (svc *Service) Check() error {
	lat, lon, alt := svc.getObserverData()
	now := time.Now()
	logger := svc.Deps.MustGetLogger()
	logger.Info("Calculating sun events", "lat", lat, "lon", lon, "alt", alt, "time", now)

	// Try to use cached events to avoid expensive recomputation; refresh after cacheTTL
	var events Events
	useCache := false
	svc.mu.RLock()
	if !svc.cachedAt.IsZero() && time.Since(svc.cachedAt) < svc.cacheTTL {
		events = svc.cachedEvents
		useCache = true
	}
	svc.mu.RUnlock()

	if !useCache {
		events = svc.calculateSunEvents(lat, lon, alt, now)
		svc.mu.Lock()
		svc.cachedEvents = events
		svc.cachedAt = time.Now()
		svc.mu.Unlock()
	}

	// Determine the next event
	nextEventName := ""
	var nextEventTime time.Time

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

	// Always compute tomorrow's events for the payload
	tomorrowForPayload := svc.calculateSunEvents(lat, lon, alt, now.AddDate(0, 0, 1))

	// Compute next solstice/equinox
	eqSolName, _, eqSolDays := nextEquinoxSolstice(now)

	// Send event message
	messenger := svc.Deps.MustGetMessenger()

	se := SunEvent{
		Now:                     now,
		Sunrise:                 events.Sunrise,
		Sunset:                  events.Sunset,
		CivilDawn:               events.CivilDawn,
		CivilDusk:               events.CivilDusk,
		DayLength:               events.DayLength,
		NightLength:             events.NightLength,
		TomorrowSunrise:         tomorrowForPayload.Sunrise,
		TomorrowDawn:            tomorrowForPayload.CivilDawn,
		NextEquinoxSolstice:     eqSolName,
		NextEquinoxSolsticeDays: eqSolDays,
	}

	eventMsg := core.Message{
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "sun_check",
		Text:        fmt.Sprintf("Next sun event: %s at %s", nextEventName, nextEventTime.Format("15:04")),
		Summary:     fmt.Sprintf("Next: %s %s", nextEventName, nextEventTime.Format("15:04")),
		Data:        se,
	}
	return messenger.Send(eventMsg)
}

// formatDuration formats a duration string (e.g. "13h2m30s") as "13h 2m" (hours and minutes only).
func formatDuration(s string) string {
	d, err := time.ParseDuration(s)
	if err != nil {
		return s
	}
	d = d.Truncate(time.Minute)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", h, m)
}

// nextEquinoxSolstice returns the name and date of the next solstice or equinox after now,
// and the number of days until it (0 = today).
// Dates use the standard astronomical approximations (accurate to within 1-2 days).
func nextEquinoxSolstice(now time.Time) (name string, date time.Time, days int) {
	year := now.Year()
	loc := now.Location()

	// Approximate dates for a given year (all times are ~noon UTC on those days).
	type event struct {
		name  string
		month time.Month
		day   int
	}
	eventsForYear := func(y int) []struct {
		name string
		t    time.Time
	} {
		// Approximations accurate to ±2 days across 1901–2099.
		// March equinox:    ~Mar 20
		// June solstice:    ~Jun 21
		// September equinox: ~Sep 22
		// December solstice: ~Dec 21
		evs := []event{
			{"Spring Equinox", time.March, 20},
			{"Summer Solstice", time.June, 21},
			{"Autumn Equinox", time.September, 22},
			{"Winter Solstice", time.December, 21},
		}
		var out []struct {
			name string
			t    time.Time
		}
		for _, e := range evs {
			out = append(out, struct {
				name string
				t    time.Time
			}{e.name, time.Date(y, e.month, e.day, 12, 0, 0, 0, loc)})
		}
		return out
	}

	// Check current year and next year to always find the next event.
	candidates := append(eventsForYear(year), eventsForYear(year+1)...)

	// Truncate now to the start of today so events on today show 0 days.
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	for _, c := range candidates {
		eventDay := time.Date(c.t.Year(), c.t.Month(), c.t.Day(), 0, 0, 0, 0, loc)
		if !eventDay.Before(today) {
			diff := int(math.Round(eventDay.Sub(today).Hours() / 24))
			return c.name, c.t, diff
		}
	}
	// Fallback (should never happen)
	return "Winter Solstice", time.Date(year+1, time.December, 21, 12, 0, 0, 0, loc), 365
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

	// Schedule alerts for today
	events := svc.calculateSunEvents(lat, lon, alt, now)

	schedule := func(eventTime time.Time, name string, dayLength string, nightLength string) {
		if eventTime.After(now) {
			duration := eventTime.Sub(now)
			logger.Debug("sun: scheduling alert", "event", name, "at", eventTime, "in", duration)
			// capture variables for closure
			et := eventTime
			n := name
			dl := dayLength
			nl := nightLength
			timer := time.AfterFunc(duration, func() {
				// Build a typed AlertEvent payload for the alert
				ae := core.AlertEvent{
					Summary: n,
					Text:    fmt.Sprintf("Sun event: %s (day length: %s, night length: %s)", n, formatDuration(dl), formatDuration(nl)),
					Level:   "info",
				}
				if err := messenger.Send(core.Message{
					ChannelName: svc.Cfg.Name,
					ServiceName: svc.Cfg.Name,
					ServiceType: svc.Cfg.Type,
					Event:       "sun_alert",
					Text:        fmt.Sprintf("Sun event: %s (day length: %s, night length: %s)", n, formatDuration(dl), formatDuration(nl)),
					Summary:     n,
					Data:        ae,
				}); err != nil {
					logger.Warn("sun: failed to send scheduled event", "err", err, "event", n, "time", et)
				}
				// Reschedule after the alert fires to keep it going
				svc.scheduleAlerts()
			})
			svc.timers = append(svc.timers, timer)
		}
	}

	schedule(events.Sunrise, "sunrise", events.DayLength, events.NightLength)
	schedule(events.Sunset, "sunset", events.DayLength, events.NightLength)
	schedule(events.CivilDawn, "dawn", events.DayLength, events.NightLength)
	schedule(events.CivilDusk, "dusk", events.DayLength, events.NightLength)

	// If no timers were scheduled (all today's events have passed), schedule tomorrow's dawn.
	// This handles the case where scheduleAlerts() is called after dusk has already passed.
	if len(svc.timers) == 0 {
		tomorrow := now.AddDate(0, 0, 1)
		tomorrowEvents := svc.calculateSunEvents(lat, lon, alt, tomorrow)
		logger.Debug("sun: all today's events have passed, scheduling tomorrow's dawn", "tomorrow", tomorrowEvents.CivilDawn)
		schedule(tomorrowEvents.CivilDawn, "dawn", tomorrowEvents.DayLength, tomorrowEvents.NightLength)
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
func (svc *Service) calculateSunEvents(lat, lon, alt float64, t time.Time) Events {
	observer := astral.Observer{
		Latitude:  lat,
		Longitude: lon,
		Elevation: alt,
	}

	sunrise, _ := astral.Sunrise(observer, t)
	sunset, _ := astral.Sunset(observer, t)
	dawn, _ := astral.Dawn(observer, t, astral.DepressionCivil)
	dusk, _ := astral.Dusk(observer, t, astral.DepressionCivil)

	// DayLength is the duration of civil daylight (civil dawn to civil dusk).
	// This is the period when the sun is less than 6° below the horizon,
	// which is considered the time with adequate natural light for outdoor activities.
	dayLength := dusk.Sub(dawn)

	// Night length (dusk to dawn next day) is the period of civil night.
	// This is measured from civil dusk today to civil dawn tomorrow.
	nextDawn, _ := astral.Dawn(observer, t.AddDate(0, 0, 1), astral.DepressionCivil)
	nightLength := nextDawn.Sub(dusk)

	return Events{
		Sunrise:     sunrise,
		Sunset:      sunset,
		CivilDawn:   dawn,
		CivilDusk:   dusk,
		DayLength:   dayLength.String(),
		NightLength: nightLength.String(),
	}
}
