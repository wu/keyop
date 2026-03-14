// Package moon calculates lunar phases and related astronomical data used for scheduling and notifications.
package moon

import (
	"fmt"
	"keyop/core"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sj14/astral/pkg/astral"
	"github.com/soniakeys/meeus/v3/moonphase"
)

// Service computes lunar-phase related metrics and publishes events when phases change or match configured criteria.
type Service struct {
	Deps            core.Dependencies
	Cfg             core.ServiceConfig
	lastMoonPhase   float64
	mu              sync.Mutex
	cachedMoonEvent MoonEvent
	cachedAt        time.Time
	cacheTTL        time.Duration
}

const defaultMoonCacheTTL = 15 * time.Minute

// MoonEvent is a typed payload with moon phase details sent on moon events.
//
//nolint:revive
type MoonEvent struct {
	Now            time.Time `json:"now"`
	Phase          float64   `json:"phase"`
	Name           string    `json:"name"`
	Illumination   int       `json:"illumination"`
	NextNew        string    `json:"next_new,omitempty"`
	NextFull       string    `json:"next_full,omitempty"`
	NextMajorName  string    `json:"next_major_name,omitempty"`
	NextMajorTime  string    `json:"next_major_time,omitempty"`
	NextMajorInSec int       `json:"next_major_in_sec,omitempty"`
}

// PayloadType returns the registered payload type name for MoonEvent.
func (m MoonEvent) PayloadType() string { return "service.moon.v1" }

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps:          deps,
		Cfg:           cfg,
		lastMoonPhase: -1, // Initialize to an impossible value
		cacheTTL:      defaultMoonCacheTTL,
	}
}

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	return nil
}

// RegisterPayloads registers the moon payload types with the provided registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	if err := reg.Register("moon", func() any { return &MoonEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register moon alias: %w", err)
		}
	}
	if err := reg.Register("service.moon.v1", func() any { return &MoonEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register service.moon.v1: %w", err)
		}
	}
	return nil
}

// Initialize performs one-time startup required by the service (resource loading or connectivity checks).
func (svc *Service) Initialize() error {
	state := svc.Deps.MustGetStateStore()
	err := state.Load(svc.Cfg.Name, &svc.lastMoonPhase)
	if err != nil {
		logger := svc.Deps.MustGetLogger()
		logger.Error("Failed to load moon phase state", "error", err)
	}
	return nil
}

// Check performs the service's periodic work: collect data, evaluate state, and publish messages/metrics.
func (svc *Service) Check() error {
	now := time.Now()
	phase := astral.MoonPhase(now)

	svc.mu.Lock()
	lastPhase := svc.lastMoonPhase
	svc.lastMoonPhase = phase
	svc.mu.Unlock()

	// Save state
	state := svc.Deps.MustGetStateStore()
	if err := state.Save(svc.Cfg.Name, phase); err != nil {
		logger := svc.Deps.MustGetLogger()
		logger.Error("Failed to save moon phase state", "error", err)
	}

	logger := svc.Deps.MustGetLogger()
	logger.Debug("Calculating moon phase", "time", now, "phase", phase)

	messenger := svc.Deps.MustGetMessenger()
	correlationID := uuid.New().String()

	phaseName := getMoonPhaseName(phase)
	lastPhaseName := ""
	if lastPhase >= 0 {
		lastPhaseName = getMoonPhaseName(lastPhase)
	}

	// Attempt to use cached moon event if fresh
	var me MoonEvent
	useCache := false
	var nextMajorName string
	var nextIn time.Duration

	svc.mu.Lock()
	if !svc.cachedAt.IsZero() && time.Since(svc.cachedAt) < svc.cacheTTL {
		me = svc.cachedMoonEvent
		useCache = true
	}
	svc.mu.Unlock()

	if useCache {
		// derive helper values for alert text
		nextMajorName = me.NextMajorName
		nextIn = time.Duration(me.NextMajorInSec) * time.Second
	} else {
		// Compute upcoming major phases (absolute times)
		dy := decimalYear(now)
		nextNew := jdeToTime(moonphase.New(dy))
		if !nextNew.After(now) {
			nextNew = jdeToTime(moonphase.New(decimalYear(now.Add(30 * 24 * time.Hour))))
		}
		nextFull := jdeToTime(moonphase.Full(dy))
		if !nextFull.After(now) {
			nextFull = jdeToTime(moonphase.Full(decimalYear(now.Add(30 * 24 * time.Hour))))
		}

		var nextMajorTime time.Time
		if nextNew.Before(nextFull) {
			nextMajorTime = nextNew
			nextMajorName = "New Moon"
		} else {
			nextMajorTime = nextFull
			nextMajorName = "Full Moon"
		}

		nextIn = time.Until(nextMajorTime).Round(time.Minute)

		// Compute illumination percentage
		f := math.Mod(phase, 28) / 28.0
		illumFrac := (1 - math.Cos(2*math.Pi*f)) / 2
		illumPct := int(math.Round(illumFrac * 100))

		me = MoonEvent{
			Now:            now,
			Phase:          phase,
			Name:           phaseName,
			Illumination:   illumPct,
			NextNew:        nextNew.UTC().Format(time.RFC3339),
			NextFull:       nextFull.UTC().Format(time.RFC3339),
			NextMajorName:  nextMajorName,
			NextMajorTime:  nextMajorTime.UTC().Format(time.RFC3339),
			NextMajorInSec: int(nextIn.Seconds()),
		}

		svc.mu.Lock()
		svc.cachedMoonEvent = me
		svc.cachedAt = time.Now()
		svc.mu.Unlock()
	}

	// Send event message with typed payload
	eventMsg := core.Message{
		Correlation: correlationID,
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "moon_phase",
		Text:        fmt.Sprintf("Current moon phase: %s (%.2f)", phaseName, phase),
		Summary:     fmt.Sprintf("Moon: %s", phaseName),
		Data:        me,
	}
	if err := messenger.Send(eventMsg); err != nil {
		return err
	}

	// Send alert if the phase name has changed
	if phaseName != lastPhaseName {
		ae := core.AlertEvent{
			Summary: fmt.Sprintf("Moon phase: %s", phaseName),
			Text:    fmt.Sprintf("The moon is now in the %s phase. Next %s in %s.", phaseName, nextMajorName, formatDuration(nextIn)),
			Level:   "info",
		}
		alertMsg := core.Message{
			Correlation: correlationID,
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "moon_alert",
			Text:        fmt.Sprintf("The moon is now in the %s phase. Next %s in %s.", phaseName, nextMajorName, formatDuration(nextIn)),
			Summary:     fmt.Sprintf("Moon phase: %s", phaseName),
			Data:        ae,
		}
		return messenger.Send(alertMsg)
	}

	return nil
}

// getMoonPhaseName returns the name of the moon phase based on the phase value (0-28)
func getMoonPhaseName(phase float64) string {
	// astral.MoonPhase returns a value between 0 and 27.99...
	// 0: New Moon
	// 7: First Quarter
	// 14: Full Moon
	// 21: Last Quarter

	if phase < 1 {
		return "New Moon"
	}
	if phase < 6 {
		return "Waxing Crescent"
	}
	if phase < 8 {
		return "First Quarter"
	}
	if phase < 13 {
		return "Waxing Gibbous"
	}
	if phase < 15 {
		return "Full Moon"
	}
	if phase < 20 {
		return "Waning Gibbous"
	}
	if phase < 22 {
		return "Last Quarter"
	}
	if phase < 27 {
		return "Waning Crescent"
	}
	return "New Moon"
}

// decimalYear converts a time.Time to a decimal year for use with the Meeus algorithms.
func decimalYear(t time.Time) float64 {
	y := t.Year()
	start := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(y+1, 1, 1, 0, 0, 0, 0, time.UTC)
	frac := float64(t.UTC().Sub(start)) / float64(end.Sub(start))
	return float64(y) + frac
}

// jdeToTime converts a Julian Ephemeris Day number to a time.Time (UTC).
// The Unix epoch (1970-01-01 00:00:00 UTC) corresponds to JDE 2440587.5.
func jdeToTime(jde float64) time.Time {
	const jdeUnixEpoch = 2440587.5
	secondsFromEpoch := (jde - jdeUnixEpoch) * 86400
	sec := int64(secondsFromEpoch)
	ns := int64((secondsFromEpoch - float64(sec)) * 1e9)
	return time.Unix(sec, ns).UTC()
}

// formatDuration returns a human-readable duration in days and hours.
func formatDuration(d time.Duration) string {
	totalHours := int(d.Hours())
	days := totalHours / 24
	hours := totalHours % 24
	if days == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	if hours == 0 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dd %dh", days, hours)
}
