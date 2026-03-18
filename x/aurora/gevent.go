package aurora

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// GEvent represents a predicted G-scale event with its timing.
type GEvent struct {
	GScale    string    `json:"g_scale"`
	GValue    int       `json:"g_value"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

// StoredGEvents is the structure stored in the state store.
type StoredGEvents struct {
	Events    []GEvent  `json:"events"`
	FetchedAt time.Time `json:"fetched_at"`
}

// extractGEventNumber extracts the numeric value from a G-scale string (e.g., "G4" -> 4).
func extractGEventNumber(gScale string) int {
	// Try to extract the number from strings like "G4", "G3", etc.
	match := regexp.MustCompile(`G(\d+)`).FindStringSubmatch(gScale)
	if len(match) > 1 {
		var num int
		if _, err := fmt.Sscanf(match[1], "%d", &num); err == nil {
			return num
		}
	}
	return 0
}

// extractGEvents extracts all G-scale events > G3 from the forecast, with their times.
func extractGEvents(forecast *ParsedForecast) ([]GEvent, error) {
	var events []GEvent

	if forecast == nil || len(forecast.Days) == 0 || len(forecast.Periods) == 0 {
		return events, nil
	}

	// For each day and period, check for G events
	for dayIdx, dayStr := range forecast.Days {
		for _, period := range forecast.Periods {
			entries := forecast.Table[period]
			if dayIdx >= len(entries) {
				continue
			}

			entry := entries[dayIdx]
			if entry.G == "" {
				continue
			}

			// Check if G value is > G3
			gValue := extractGEventNumber(entry.G)
			if gValue <= 3 {
				continue
			}

			// Parse the period to get start and end times (e.g., "00-03UT")
			startTime, endTime, err := parsePeriodTimes(dayStr, period)
			if err != nil {
				continue
			}

			events = append(events, GEvent{
				GScale:    entry.G,
				GValue:    gValue,
				StartTime: startTime,
				EndTime:   endTime,
			})
		}
	}

	// Sort by start time
	sort.Slice(events, func(i, j int) bool {
		return events[i].StartTime.Before(events[j].StartTime)
	})

	return events, nil
}

// parsePeriodTimes parses a period string like "00-03UT" with a day string to get start/end times in UTC.
func parsePeriodTimes(dayStr, period string) (time.Time, time.Time, error) {
	// Parse day (e.g., "Mar 18" or "18 Mar 2025")
	var day time.Time
	year := time.Now().Year()

	// Try various date formats
	formats := []string{
		"Jan 2",
		"Jan 02",
		"2 Jan",
		"02 Jan",
	}

	dayStr = strings.TrimSpace(dayStr)
	for _, fmt := range formats {
		if d, err := time.Parse(fmt, dayStr); err == nil {
			day = d.AddDate(year, 0, 0)
			break
		}
	}

	if day.IsZero() {
		return time.Time{}, time.Time{}, fmt.Errorf("failed to parse day: %s", dayStr)
	}

	// Parse period (e.g., "00-03UT")
	periodParts := strings.Split(strings.TrimSuffix(period, "UT"), "-")
	if len(periodParts) != 2 {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid period format: %s", period)
	}

	var startHour, endHour int
	if _, err := fmt.Sscanf(periodParts[0], "%d", &startHour); err != nil {
		return time.Time{}, time.Time{}, err
	}
	if _, err := fmt.Sscanf(periodParts[1], "%d", &endHour); err != nil {
		return time.Time{}, time.Time{}, err
	}

	startTime := time.Date(day.Year(), day.Month(), day.Day(), startHour, 0, 0, 0, time.UTC)
	endTime := time.Date(day.Year(), day.Month(), day.Day(), endHour, 0, 0, 0, time.UTC)

	// Handle day rollover (e.g., 21-00UT means 21:00 today to 00:00 tomorrow)
	if endHour < startHour {
		endTime = endTime.AddDate(0, 0, 1)
	}

	return startTime, endTime, nil
}

// filterNewEvents returns only events that are new or changed compared to previous stored events.
func filterNewEvents(currentEvents []GEvent, storedEvents []GEvent) []GEvent {
	if len(storedEvents) == 0 {
		return currentEvents
	}

	// Build a map of stored events for quick lookup
	storedMap := make(map[string]bool)
	for _, evt := range storedEvents {
		key := fmt.Sprintf("%s|%d|%d", evt.GScale, evt.StartTime.Unix(), evt.EndTime.Unix())
		storedMap[key] = true
	}

	var newEvents []GEvent
	for _, evt := range currentEvents {
		key := fmt.Sprintf("%s|%d|%d", evt.GScale, evt.StartTime.Unix(), evt.EndTime.Unix())
		if !storedMap[key] {
			newEvents = append(newEvents, evt)
		}
	}

	return newEvents
}

// findHighestGEvent returns the G event with the highest G value.
func findHighestGEvent(events []GEvent) *GEvent {
	if len(events) == 0 {
		return nil
	}
	highest := &events[0]
	for i := 1; i < len(events); i++ {
		if events[i].GValue > highest.GValue {
			highest = &events[i]
		}
	}
	return highest
}

// loadPreviousGEvents loads previously stored G events from the state store.
func (svc *Service) loadPreviousGEvents() ([]GEvent, error) {
	store := svc.Deps.GetStateStore()
	if store == nil {
		return nil, nil
	}

	var stored StoredGEvents
	if err := store.Load("aurora_g_events", &stored); err != nil {
		// Key doesn't exist yet, that's fine
		return nil, nil
	}

	return stored.Events, nil
}

// savePreviousGEvents saves the current G events to the state store.
func (svc *Service) savePreviousGEvents(events []GEvent) error {
	store := svc.Deps.GetStateStore()
	if store == nil {
		return nil
	}

	stored := StoredGEvents{
		Events:    events,
		FetchedAt: time.Now(),
	}

	return store.Save("aurora_g_events", stored)
}
