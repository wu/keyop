package sun

import (
	"testing"
	"time"
)

// saneLat/Lon: San Francisco — reliable civil dawn/dusk year-round.
const (
	sfLat = 37.7749
	sfLon = -122.4194
)

func TestCivilDawnDusk_ReturnsNonZero(t *testing.T) {
	t.Parallel()
	// Mid-summer San Francisco: sun always rises and sets.
	ts := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	dawn, dusk := CivilDawnDusk(sfLat, sfLon, ts)
	if dawn.IsZero() {
		t.Error("expected non-zero dawn")
	}
	if dusk.IsZero() {
		t.Error("expected non-zero dusk")
	}
	if !dawn.Before(dusk) {
		t.Errorf("expected dawn (%v) before dusk (%v)", dawn, dusk)
	}
}

func TestCivilDawnDusk_UTC(t *testing.T) {
	t.Parallel()
	ts := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	dawn, dusk := CivilDawnDusk(sfLat, sfLon, ts)
	if dawn.Location() != time.UTC {
		t.Errorf("dawn location: want UTC, got %v", dawn.Location())
	}
	if dusk.Location() != time.UTC {
		t.Errorf("dusk location: want UTC, got %v", dusk.Location())
	}
}

func TestCivilDawnDusk_TruncatedToSeconds(t *testing.T) {
	t.Parallel()
	ts := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	dawn, dusk := CivilDawnDusk(sfLat, sfLon, ts)
	if dawn.Nanosecond() != 0 {
		t.Errorf("dawn has sub-second component: %v", dawn)
	}
	if dusk.Nanosecond() != 0 {
		t.Errorf("dusk has sub-second component: %v", dusk)
	}
}

func TestCivilDawnDusk_InputTimeIgnored(t *testing.T) {
	t.Parallel()
	// Time-of-day within the same UTC calendar day should not affect the result.
	ts1 := time.Date(2024, 6, 21, 6, 0, 0, 0, time.UTC)
	ts2 := time.Date(2024, 6, 21, 18, 0, 0, 0, time.UTC)
	dawn1, dusk1 := CivilDawnDusk(sfLat, sfLon, ts1)
	dawn2, dusk2 := CivilDawnDusk(sfLat, sfLon, ts2)
	if dawn1 != dawn2 {
		t.Errorf("dawn differs for same calendar day: %v vs %v", dawn1, dawn2)
	}
	if dusk1 != dusk2 {
		t.Errorf("dusk differs for same calendar day: %v vs %v", dusk1, dusk2)
	}
}

func TestCivilDawnDusk_PolarNight(t *testing.T) {
	t.Parallel()
	// North Pole in winter: no civil dawn/dusk — should return zero times, not panic.
	ts := time.Date(2024, 12, 21, 12, 0, 0, 0, time.UTC)
	dawn, dusk := CivilDawnDusk(90.0, 0.0, ts)
	// Either both zero (polar night) or both non-zero (polar day) — no panic either way.
	_ = dawn
	_ = dusk
}

func TestSolarDaysForRange_SingleDay(t *testing.T) {
	t.Parallel()
	ts := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	days := SolarDaysForRange(sfLat, sfLon, ts, ts)
	if len(days) != 1 {
		t.Fatalf("expected 1 day, got %d", len(days))
	}
	if days[0].Date != "2024-06-21" {
		t.Errorf("expected date 2024-06-21, got %q", days[0].Date)
	}
	if days[0].Dawn.IsZero() {
		t.Error("expected non-zero dawn")
	}
	if days[0].Dusk.IsZero() {
		t.Error("expected non-zero dusk")
	}
}

func TestSolarDaysForRange_Multipledays(t *testing.T) {
	t.Parallel()
	start := time.Date(2024, 6, 21, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 23, 23, 59, 59, 0, time.UTC)
	days := SolarDaysForRange(sfLat, sfLon, start, end)
	if len(days) != 3 {
		t.Fatalf("expected 3 days, got %d: %v", len(days), days)
	}
	wantDates := []string{"2024-06-21", "2024-06-22", "2024-06-23"}
	for i, d := range days {
		if d.Date != wantDates[i] {
			t.Errorf("days[%d].Date = %q, want %q", i, d.Date, wantDates[i])
		}
	}
}

func TestSolarDaysForRange_ChronologicalOrder(t *testing.T) {
	t.Parallel()
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	days := SolarDaysForRange(sfLat, sfLon, start, end)
	for i := 1; i < len(days); i++ {
		if days[i].Date <= days[i-1].Date {
			t.Errorf("days not in order: %q after %q", days[i].Date, days[i-1].Date)
		}
	}
}

func TestSolarDaysForRange_StartAfterEnd(t *testing.T) {
	t.Parallel()
	start := time.Date(2024, 6, 23, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 21, 0, 0, 0, 0, time.UTC)
	days := SolarDaysForRange(sfLat, sfLon, start, end)
	if len(days) != 0 {
		t.Errorf("expected 0 days for inverted range, got %d", len(days))
	}
}

func TestSolarDaysForRange_SpansMonthBoundary(t *testing.T) {
	t.Parallel()
	start := time.Date(2024, 1, 30, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC)
	days := SolarDaysForRange(sfLat, sfLon, start, end)
	if len(days) != 4 {
		t.Fatalf("expected 4 days across month boundary, got %d", len(days))
	}
	if days[0].Date != "2024-01-30" || days[3].Date != "2024-02-02" {
		t.Errorf("unexpected dates: %q .. %q", days[0].Date, days[3].Date)
	}
}

func TestSolarDay_FieldsPopulated(t *testing.T) {
	t.Parallel()
	ts := time.Date(2024, 6, 21, 0, 0, 0, 0, time.UTC)
	days := SolarDaysForRange(sfLat, sfLon, ts, ts)
	if len(days) == 0 {
		t.Fatal("expected at least one day")
	}
	d := days[0]
	if d.Date == "" {
		t.Error("SolarDay.Date is empty")
	}
	if d.Dawn.IsZero() {
		t.Error("SolarDay.Dawn is zero")
	}
	if d.Dusk.IsZero() {
		t.Error("SolarDay.Dusk is zero")
	}
}
