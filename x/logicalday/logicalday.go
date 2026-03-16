package logicalday

import (
	"fmt"
	"strings"
	"time"
)

// Calculator computes logical days based on a configurable end-of-day time.
// A logical day ends at a specified time (e.g., 4am) rather than midnight.
type Calculator struct {
	endOfDayHour int
	endOfDayMin  int
	endOfDaySec  int
	loc          *time.Location
}

// NewCalculator creates a new logical day calculator.
//
// endOfDayStr must be in "HH:MM" or "HH:MM:SS" format (24-hour).
// loc is the timezone for interpreting end-of-day time.
// Panics if endOfDayStr is invalid or loc is nil.
func NewCalculator(endOfDayStr string, loc *time.Location) *Calculator {
	if loc == nil {
		panic("location cannot be nil")
	}

	parts := strings.Split(endOfDayStr, ":")
	if len(parts) < 2 || len(parts) > 3 {
		panic(fmt.Sprintf("invalid end-of-day format: %s (expected HH:MM or HH:MM:SS)", endOfDayStr))
	}

	hour, minute, second := 0, 0, 0
	var errH, errM, errS error

	if len(parts) >= 1 {
		_, errH = fmt.Sscanf(parts[0], "%d", &hour)
	}
	if len(parts) >= 2 {
		_, errM = fmt.Sscanf(parts[1], "%d", &minute)
	}
	if len(parts) == 3 {
		_, errS = fmt.Sscanf(parts[2], "%d", &second)
	}

	if errH != nil || errM != nil || errS != nil || hour > 23 || minute > 59 || second > 59 {
		panic(fmt.Sprintf("invalid end-of-day time: %s", endOfDayStr))
	}

	return &Calculator{
		endOfDayHour: hour,
		endOfDayMin:  minute,
		endOfDaySec:  second,
		loc:          loc,
	}
}

// GetLogicalDay returns the logical day for a given scheduled time.
//
// If hasSpecificTime is false, the logical day is simply the date portion
// of scheduledTime (with time set to 00:00:00 in the local timezone).
//
// If hasSpecificTime is true, the logical day is calculated by:
//   - If the time is before end-of-day, subtract one day (it belongs to yesterday)
//   - If the time is at or after end-of-day, use the same day
//
// The returned time is in the local timezone with time set to 00:00:00.
func (c *Calculator) GetLogicalDay(scheduledTime time.Time, hasSpecificTime bool) time.Time {
	// Convert to local timezone
	localTime := scheduledTime.In(c.loc)

	// Start with midnight of the local date
	baseDay := time.Date(localTime.Year(), localTime.Month(), localTime.Day(), 0, 0, 0, 0, c.loc)

	if !hasSpecificTime {
		// No specific time: use the date as-is
		return baseDay
	}

	// Has specific time: check if it's before end-of-day
	hour := localTime.Hour()
	minute := localTime.Minute()
	second := localTime.Second()

	// Compare: (hour, minute, second) < (endOfDayHour, endOfDayMin, endOfDaySec)
	if hour < c.endOfDayHour ||
		(hour == c.endOfDayHour && minute < c.endOfDayMin) ||
		(hour == c.endOfDayHour && minute == c.endOfDayMin && second < c.endOfDaySec) {
		// Time is before end-of-day, so it belongs to the previous day
		return baseDay.AddDate(0, 0, -1)
	}

	// Time is at or after end-of-day, so it belongs to this day
	return baseDay
}

// IsToday returns true if the given logical day is today.
// "Today" is defined as the current date in the local timezone.
func (c *Calculator) IsToday(logicalDay time.Time) bool {
	now := time.Now().In(c.loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, c.loc)

	logicalDayMidnight := logicalDay.In(c.loc)
	logicalDayMidnight = time.Date(logicalDayMidnight.Year(), logicalDayMidnight.Month(), logicalDayMidnight.Day(), 0, 0, 0, 0, c.loc)

	return today.Equal(logicalDayMidnight)
}

// TodayStart returns the start of today (00:00:00 in local timezone).
func (c *Calculator) TodayStart() time.Time {
	now := time.Now().In(c.loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, c.loc)
}

// TodayEnd returns the end of today (23:59:59 in local timezone).
func (c *Calculator) TodayEnd() time.Time {
	now := time.Now().In(c.loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, c.loc)
}

// LogicalTodayStart returns the start of the logical day that includes now.
// This is the time when the current logical day started (end-of-day time yesterday).
func (c *Calculator) LogicalTodayStart() time.Time {
	now := time.Now().In(c.loc)

	// Start with today at end-of-day time
	endOfDayToday := time.Date(now.Year(), now.Month(), now.Day(), c.endOfDayHour, c.endOfDayMin, c.endOfDaySec, 0, c.loc)

	// If we've already passed end-of-day, the logical day started at end-of-day today
	// Otherwise, it started at end-of-day yesterday
	if now.After(endOfDayToday) {
		return endOfDayToday
	}

	return endOfDayToday.AddDate(0, 0, -1)
}

// LogicalTodayEnd returns the end of the logical day that includes now.
// This is the end-of-day time today.
func (c *Calculator) LogicalTodayEnd() time.Time {
	now := time.Now().In(c.loc)
	return time.Date(now.Year(), now.Month(), now.Day(), c.endOfDayHour, c.endOfDayMin, c.endOfDaySec, 0, c.loc)
}

// Today returns the date representing the logical day that includes now.
// This accounts for the end-of-day cutoff: if it's before end-of-day, it returns yesterday;
// if it's at or after end-of-day, it returns today.
func (c *Calculator) Today() time.Time {
	return c.GetLogicalDay(time.Now(), true)
}

// Yesterday returns the date representing yesterday in the local timezone.
func (c *Calculator) Yesterday() time.Time {
	now := time.Now().In(c.loc)
	yesterday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, c.loc).AddDate(0, 0, -1)
	return yesterday
}

// Tomorrow returns the date representing tomorrow in the local timezone.
func (c *Calculator) Tomorrow() time.Time {
	now := time.Now().In(c.loc)
	tomorrow := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, c.loc).AddDate(0, 0, 1)
	return tomorrow
}

// DayBeforeYesterday returns the date two days before today.
func (c *Calculator) DayBeforeYesterday() time.Time {
	now := time.Now().In(c.loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, c.loc).AddDate(0, 0, -2)
}

// DayAfterTomorrow returns the date two days after today.
func (c *Calculator) DayAfterTomorrow() time.Time {
	now := time.Now().In(c.loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, c.loc).AddDate(0, 0, 2)
}

// Location returns the location used by this calculator.
func (c *Calculator) Location() *time.Location {
	return c.loc
}
