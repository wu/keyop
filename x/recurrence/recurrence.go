package recurrence

import (
	"time"
)

// Pattern defines a recurrence pattern for tasks.
type Pattern struct {
	Type       string         `json:"type"`       // "daily", "weekly", "monthly", "yearly"
	Interval   int            `json:"interval"`   // Repeat every N units (1 = every day/week/month)
	DaysOfWeek []time.Weekday `json:"daysOfWeek"` // For weekly: which days (0=Sun, 1=Mon, etc.)
	DayOfMonth int            `json:"dayOfMonth"` // For monthly: which day (1-31)
	Month      time.Month     `json:"month"`      // For yearly: which month
	DayOfYear  int            `json:"dayOfYear"`  // For yearly: which day (1-366)
}

// Next calculates the next occurrence of this pattern after the given time.
func (p *Pattern) Next(after time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}

	switch p.Type {
	case "daily":
		return p.nextDaily(after)
	case "weekly":
		return p.nextWeekly(after)
	case "monthly":
		return p.nextMonthly(after)
	case "yearly":
		return p.nextYearly(after)
	default:
		return time.Time{}
	}
}

func (p *Pattern) nextDaily(after time.Time) time.Time {
	interval := p.Interval
	if interval < 1 {
		interval = 1
	}
	// Add interval days to the given time
	return after.AddDate(0, 0, interval)
}

func (p *Pattern) nextWeekly(after time.Time) time.Time {
	interval := p.Interval
	if interval < 1 {
		interval = 1
	}

	if len(p.DaysOfWeek) == 0 {
		// No days specified, default to same weekday
		return after.AddDate(0, 0, 7*interval)
	}

	// Sort the days for easier logic
	targetDays := p.DaysOfWeek
	currentDay := after.Weekday()
	currentTime := after

	// Check if any of today's target days haven't passed yet
	for _, targetDay := range targetDays {
		if targetDay > currentDay {
			daysUntil := int(targetDay - currentDay)
			return currentTime.AddDate(0, 0, daysUntil)
		}
	}

	// Otherwise, get the next occurrence by adding interval weeks and finding first target day
	nextWeekStart := currentTime.AddDate(0, 0, 7*interval-(int(currentDay)))
	for _, targetDay := range targetDays {
		if targetDay >= 0 {
			return nextWeekStart.AddDate(0, 0, int(targetDay))
		}
	}

	return currentTime.AddDate(0, 0, 7*interval)
}

func (p *Pattern) nextMonthly(after time.Time) time.Time {
	interval := p.Interval
	if interval < 1 {
		interval = 1
	}

	dayOfMonth := p.DayOfMonth
	if dayOfMonth < 1 || dayOfMonth > 31 {
		dayOfMonth = after.Day()
	}

	// Try to find the next occurrence by adding interval months from current month
	year := after.Year()
	month := after.Month()

	// Add the interval to get to the target month
	for i := 0; i < interval; i++ {
		month++
		if month > 12 {
			month = 1
			year++
		}
	}

	// Set to the target day, clamping if necessary
	daysInMonth := daysIn(month, year)
	targetDay := dayOfMonth
	if targetDay > daysInMonth {
		targetDay = daysInMonth
	}

	result := time.Date(year, month, targetDay,
		after.Hour(), after.Minute(), after.Second(), after.Nanosecond(), after.Location())

	// If result is before or equal to after, we need to add another interval
	if !result.After(after) {
		return p.nextMonthly(result)
	}

	return result
}

func (p *Pattern) nextYearly(after time.Time) time.Time {
	interval := p.Interval
	if interval < 1 {
		interval = 1
	}

	month := p.Month
	if month < 1 || month > 12 {
		month = after.Month()
	}

	dayOfMonth := p.DayOfMonth
	if dayOfMonth < 1 || dayOfMonth > 31 {
		dayOfMonth = after.Day()
	}

	daysInMonth := daysIn(month, after.Year())
	if dayOfMonth > daysInMonth {
		dayOfMonth = daysInMonth
	}

	result := time.Date(after.Year(), month, dayOfMonth,
		after.Hour(), after.Minute(), after.Second(), after.Nanosecond(), after.Location())

	if !result.After(after) {
		result = time.Date(after.Year()+interval, month, dayOfMonth,
			after.Hour(), after.Minute(), after.Second(), after.Nanosecond(), after.Location())
	}

	return result
}

// daysIn returns the number of days in a given month.
func daysIn(m time.Month, year int) int {
	return time.Date(year, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
