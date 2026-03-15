package recurrence

import (
	"testing"
	"time"
)

func TestDailyRecurrence(t *testing.T) {
	pattern := &Pattern{
		Type:     "daily",
		Interval: 2,
	}

	start := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	next := pattern.Next(start)

	expected := time.Date(2026, 3, 17, 10, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("Daily recurrence: expected %v, got %v", expected, next)
	}
}

func TestWeeklyRecurrence(t *testing.T) {
	// Every Monday and Friday
	pattern := &Pattern{
		Type:       "weekly",
		Interval:   1,
		DaysOfWeek: []time.Weekday{time.Monday, time.Friday},
	}

	// Start on Wednesday, Mar 12, 2026
	start := time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC) // This is a Wednesday
	next := pattern.Next(start)

	// Should jump to Friday, Mar 13
	expectedDay := 13
	if next.Day() != expectedDay {
		t.Errorf("Weekly recurrence: expected day %d, got %d", expectedDay, next.Day())
	}
}

func TestMonthlyRecurrence(t *testing.T) {
	pattern := &Pattern{
		Type:       "monthly",
		Interval:   1,
		DayOfMonth: 15,
	}

	start := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)
	next := pattern.Next(start)

	expected := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("Monthly recurrence: expected %v, got %v", expected, next)
	}
}

func TestYearlyRecurrence(t *testing.T) {
	pattern := &Pattern{
		Type:       "yearly",
		Interval:   1,
		Month:      time.December,
		DayOfMonth: 25,
	}

	start := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	next := pattern.Next(start)

	expected := time.Date(2026, 12, 25, 10, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("Yearly recurrence: expected %v, got %v", expected, next)
	}
}

func TestMonthlyRecurrenceEndOfMonth(t *testing.T) {
	// Test when day of month doesn't exist in target month (e.g., Jan 31 -> Feb)
	pattern := &Pattern{
		Type:       "monthly",
		Interval:   1,
		DayOfMonth: 31,
	}

	start := time.Date(2026, 1, 31, 10, 0, 0, 0, time.UTC)
	next := pattern.Next(start)

	// Should be Feb 28 (non-leap year)
	if next.Month() != time.February || next.Day() != 28 {
		t.Errorf("Monthly end-of-month: expected Feb 28, got %v", next)
	}
}
