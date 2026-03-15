package logicalday

import (
	"testing"
	"time"
)

func TestNewCalculator(t *testing.T) {
	loc, _ := time.LoadLocation("America/Los_Angeles")

	tests := []struct {
		name    string
		eod     string
		loc     *time.Location
		wantErr bool
	}{
		{"Valid HH:MM", "04:00", loc, false},
		{"Valid HH:MM:SS", "04:00:00", loc, false},
		{"Another time HH:MM", "22:30", loc, false},
		{"Invalid format", "4", nil, true},
		{"Invalid format too many parts", "04:00:00:00", loc, true},
		{"Hour too high", "25:00", loc, true},
		{"Minute too high", "04:60", loc, true},
		{"Second too high", "04:00:60", loc, true},
		{"Nil location", "04:00", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); (r != nil) != tt.wantErr {
					if tt.wantErr {
						t.Errorf("expected panic but got none")
					} else {
						t.Errorf("unexpected panic: %v", r)
					}
				}
			}()

			_ = NewCalculator(tt.eod, tt.loc)
		})
	}
}

func TestGetLogicalDay(t *testing.T) {
	loc, _ := time.LoadLocation("America/Los_Angeles")
	calc := NewCalculator("04:00:00", loc)

	tests := []struct {
		name            string
		scheduledTime   time.Time
		hasSpecificTime bool
		expectedDate    string // YYYY-MM-DD format
	}{
		{
			name:            "Before end-of-day (2am Saturday) with specific time",
			scheduledTime:   time.Date(2026, 3, 14, 2, 0, 0, 0, loc), // 2am Saturday March 14
			hasSpecificTime: true,
			expectedDate:    "2026-03-13", // Should be Friday March 13
		},
		{
			name:            "At end-of-day (4am Saturday) with specific time",
			scheduledTime:   time.Date(2026, 3, 14, 4, 0, 0, 0, loc), // 4am Saturday March 14
			hasSpecificTime: true,
			expectedDate:    "2026-03-14", // Should be Saturday March 14
		},
		{
			name:            "After end-of-day (5am Saturday) with specific time",
			scheduledTime:   time.Date(2026, 3, 14, 5, 0, 0, 0, loc), // 5am Saturday March 14
			hasSpecificTime: true,
			expectedDate:    "2026-03-14", // Should be Saturday March 14
		},
		{
			name:            "Late at night (11pm Friday) with specific time",
			scheduledTime:   time.Date(2026, 3, 13, 23, 0, 0, 0, loc), // 11pm Friday March 13
			hasSpecificTime: true,
			expectedDate:    "2026-03-13", // Should be Friday March 13
		},
		{
			name:            "No specific time uses date as-is",
			scheduledTime:   time.Date(2026, 3, 14, 2, 0, 0, 0, loc), // Any time Saturday March 14
			hasSpecificTime: false,
			expectedDate:    "2026-03-14", // Should be Saturday March 14
		},
		{
			name:            "No specific time with midnight time",
			scheduledTime:   time.Date(2026, 3, 14, 0, 0, 0, 0, loc), // Midnight Saturday March 14
			hasSpecificTime: false,
			expectedDate:    "2026-03-14", // Should be Saturday March 14
		},
		{
			name:            "Just before end-of-day (3:59:59am)",
			scheduledTime:   time.Date(2026, 3, 14, 3, 59, 59, 0, loc),
			hasSpecificTime: true,
			expectedDate:    "2026-03-13", // Should be Friday March 13
		},
		{
			name:            "Just after end-of-day (4:00:01am)",
			scheduledTime:   time.Date(2026, 3, 14, 4, 0, 1, 0, loc),
			hasSpecificTime: true,
			expectedDate:    "2026-03-14", // Should be Saturday March 14
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.GetLogicalDay(tt.scheduledTime, tt.hasSpecificTime)
			resultDate := result.Format("2006-01-02")

			if resultDate != tt.expectedDate {
				t.Errorf("got %s, want %s", resultDate, tt.expectedDate)
			}

			// Verify time is always 00:00:00
			if result.Hour() != 0 || result.Minute() != 0 || result.Second() != 0 {
				t.Errorf("expected time to be 00:00:00, got %02d:%02d:%02d", result.Hour(), result.Minute(), result.Second())
			}
		})
	}
}

func TestGetLogicalDayDifferentEndOfDay(t *testing.T) {
	loc, _ := time.LoadLocation("America/Los_Angeles")
	calc := NewCalculator("22:30:00", loc) // End at 10:30pm

	tests := []struct {
		name            string
		scheduledTime   time.Time
		hasSpecificTime bool
		expectedDate    string
	}{
		{
			name:            "Before 22:30 should be previous day",
			scheduledTime:   time.Date(2026, 3, 14, 22, 29, 0, 0, loc),
			hasSpecificTime: true,
			expectedDate:    "2026-03-13",
		},
		{
			name:            "At 22:30 should be current day",
			scheduledTime:   time.Date(2026, 3, 14, 22, 30, 0, 0, loc),
			hasSpecificTime: true,
			expectedDate:    "2026-03-14",
		},
		{
			name:            "After 22:30 should be current day",
			scheduledTime:   time.Date(2026, 3, 14, 22, 31, 0, 0, loc),
			hasSpecificTime: true,
			expectedDate:    "2026-03-14",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.GetLogicalDay(tt.scheduledTime, tt.hasSpecificTime)
			resultDate := result.Format("2006-01-02")

			if resultDate != tt.expectedDate {
				t.Errorf("got %s, want %s", resultDate, tt.expectedDate)
			}
		})
	}
}

func TestIsTodayAndTodayHelpers(t *testing.T) {
	loc, _ := time.LoadLocation("America/Los_Angeles")
	calc := NewCalculator("04:00:00", loc)

	// We can't reliably test IsToday without mocking time, but we can verify the basic logic
	today := time.Now().In(loc)
	todayMidnight := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)

	if !calc.IsToday(todayMidnight) {
		t.Errorf("IsToday failed for today's midnight")
	}

	yesterday := todayMidnight.AddDate(0, 0, -1)
	if calc.IsToday(yesterday) {
		t.Errorf("IsToday incorrectly returned true for yesterday")
	}

	// Test TodayStart and TodayEnd
	start := calc.TodayStart()
	end := calc.TodayEnd()

	if start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 {
		t.Errorf("TodayStart should be midnight, got %02d:%02d:%02d", start.Hour(), start.Minute(), start.Second())
	}

	if end.Hour() != 23 || end.Minute() != 59 || end.Second() != 59 {
		t.Errorf("TodayEnd should be 23:59:59, got %02d:%02d:%02d", end.Hour(), end.Minute(), end.Second())
	}

	if !start.Before(end) {
		t.Errorf("TodayStart should be before TodayEnd")
	}
}

func TestLogicalTodayHelpers(t *testing.T) {
	loc, _ := time.LoadLocation("America/Los_Angeles")
	calc := NewCalculator("04:00:00", loc)

	logicalStart := calc.LogicalTodayStart()
	logicalEnd := calc.LogicalTodayEnd()

	if logicalStart.Hour() != 4 || logicalStart.Minute() != 0 || logicalStart.Second() != 0 {
		t.Errorf("LogicalTodayStart should be at end-of-day time, got %02d:%02d:%02d", logicalStart.Hour(), logicalStart.Minute(), logicalStart.Second())
	}

	if logicalEnd.Hour() != 4 || logicalEnd.Minute() != 0 || logicalEnd.Second() != 0 {
		t.Errorf("LogicalTodayEnd should be at end-of-day time, got %02d:%02d:%02d", logicalEnd.Hour(), logicalEnd.Minute(), logicalEnd.Second())
	}

	// Logical start should be before or equal to logical end
	if logicalStart.After(logicalEnd) {
		t.Errorf("LogicalTodayStart should not be after LogicalTodayEnd")
	}
}

func TestTimezoneDifference(t *testing.T) {
	laLoc, _ := time.LoadLocation("America/Los_Angeles")
	utcLoc := time.UTC

	calcLA := NewCalculator("04:00:00", laLoc)
	calcUTC := NewCalculator("04:00:00", utcLoc)

	// Create a time that's 2am in LA on March 14
	// This is 10am UTC on March 14 (LA is UTC-7 in March)
	timeInLA := time.Date(2026, 3, 14, 2, 0, 0, 0, laLoc)

	logicalDayLA := calcLA.GetLogicalDay(timeInLA, true)
	logicalDayUTC := calcUTC.GetLogicalDay(timeInLA, true)

	// In LA, 2am is before 4am end-of-day, so should be previous day (March 13)
	if logicalDayLA.Format("2006-01-02") != "2026-03-13" {
		t.Errorf("LA: got %s, want 2026-03-13", logicalDayLA.Format("2006-01-02"))
	}

	// In UTC, the same moment is 10am on March 14, which is after 4am end-of-day
	// So it should be March 14 in UTC's logical day
	if logicalDayUTC.Format("2006-01-02") != "2026-03-14" {
		t.Errorf("UTC: got %s, want 2026-03-14", logicalDayUTC.Format("2006-01-02"))
	}
}

func TestEdgeCases(t *testing.T) {
	loc, _ := time.LoadLocation("America/Los_Angeles")
	calc := NewCalculator("00:00:00", loc) // End of day at midnight (normal calendar day)

	// With midnight end-of-day:
	// - 00:00:00 exactly is AT end-of-day, so it belongs to that day
	// - Any time before midnight (1am-11pm) is NOT before midnight, so belongs to that day
	// - Only times between midnight and end-of-day (which is midnight) would wrap
	time1am := time.Date(2026, 3, 14, 1, 0, 0, 0, loc)
	time11pm := time.Date(2026, 3, 14, 23, 0, 0, 0, loc)
	timeMidnight := time.Date(2026, 3, 14, 0, 0, 0, 0, loc)

	day1am := calc.GetLogicalDay(time1am, true)
	day11pm := calc.GetLogicalDay(time11pm, true)
	dayMidnight := calc.GetLogicalDay(timeMidnight, true)

	if day1am.Format("2006-01-02") != "2026-03-14" {
		t.Errorf("1am should be on 2026-03-14, got %s", day1am.Format("2006-01-02"))
	}

	if day11pm.Format("2006-01-02") != "2026-03-14" {
		t.Errorf("11pm should be on 2026-03-14, got %s", day11pm.Format("2006-01-02"))
	}

	if dayMidnight.Format("2006-01-02") != "2026-03-14" {
		t.Errorf("Midnight should be on 2026-03-14, got %s", dayMidnight.Format("2006-01-02"))
	}
}
