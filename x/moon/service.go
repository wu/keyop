// Package moon calculates lunar phases and related astronomical data used for scheduling and notifications.
package moon

import (
	"fmt"
	"keyop/core"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sj14/astral/pkg/astral"
	"github.com/soniakeys/meeus/v3/moonphase"
)

// Service computes lunar-phase related metrics and publishes events when phases change or match configured criteria.
type Service struct {
	Deps          core.Dependencies
	Cfg           core.ServiceConfig
	lastMoonPhase float64
	mu            sync.Mutex
}

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps:          deps,
		Cfg:           cfg,
		lastMoonPhase: -1, // Initialize to an impossible value
	}
}

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
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

	// Send event message with details
	eventMsg := core.Message{
		Correlation: correlationID,
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "moon_phase",
		Text:        fmt.Sprintf("Current moon phase: %s (%.2f)", phaseName, phase),
		Summary:     fmt.Sprintf("Moon: %s", phaseName),
		Data: map[string]interface{}{
			"phase": phase,
			"name":  phaseName,
		},
	}
	if err := messenger.Send(eventMsg); err != nil {
		return err
	}

	// Send alert if the phase name has changed
	if phaseName != lastPhaseName {
		nextEvent, timeUntil := timeUntilNextMajorPhase(now)
		alertMsg := core.Message{
			Correlation: correlationID,
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "moon_phase_change",
			Text:        fmt.Sprintf("The moon is now in the %s phase. Next %s in %s.", phaseName, nextEvent, formatDuration(timeUntil)),
			Summary:     fmt.Sprintf("Moon phase: %s", phaseName),
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

// timeUntilNextMajorPhase returns the name and exact time.Time of the next New Moon or Full Moon,
// whichever comes first, using the Meeus algorithm for precise timing.
func timeUntilNextMajorPhase(from time.Time) (string, time.Duration) {
	dy := decimalYear(from)

	nextNew := jdeToTime(moonphase.New(dy))
	if !nextNew.After(from) {
		nextNew = jdeToTime(moonphase.New(decimalYear(from.Add(30 * 24 * time.Hour))))
	}

	nextFull := jdeToTime(moonphase.Full(dy))
	if !nextFull.After(from) {
		nextFull = jdeToTime(moonphase.Full(decimalYear(from.Add(30 * 24 * time.Hour))))
	}

	if nextNew.Before(nextFull) {
		return "New Moon", time.Until(nextNew).Round(time.Minute)
	}
	return "Full Moon", time.Until(nextFull).Round(time.Minute)
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
