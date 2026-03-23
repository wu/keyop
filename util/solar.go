package util

import (
	"time"

	"github.com/sj14/astral/pkg/astral"
)

// SolarDay holds civil dawn and dusk for a single UTC calendar date.
type SolarDay struct {
	// Date is the UTC calendar date in "YYYY-MM-DD" format.
	Date string `json:"date"`
	// Dawn is the civil dawn time (UTC).
	Dawn time.Time `json:"dawn"`
	// Dusk is the civil dusk time (UTC).
	Dusk time.Time `json:"dusk"`
}

// CivilDawnDusk computes civil dawn and dusk for the UTC calendar day containing t,
// at the given latitude and longitude (decimal degrees).
// Returned times are in UTC. Returns zero times if the astral library cannot compute
// a result (polar day/night).
func CivilDawnDusk(lat, lon float64, t time.Time) (dawn, dusk time.Time) {
	observer := astral.Observer{
		Latitude:  lat,
		Longitude: lon,
	}
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	d, err := astral.Dawn(observer, day, astral.DepressionCivil)
	if err == nil {
		// Truncate to whole seconds so JSON serialization produces a string that all
		// browsers can parse with new Date() — Go's RFC3339Nano format would otherwise
		// emit sub-second digits that some browsers reject.
		dawn = d.UTC().Truncate(time.Second)
	}
	k, err := astral.Dusk(observer, day, astral.DepressionCivil)
	if err == nil {
		dusk = k.UTC().Truncate(time.Second)
	}
	return dawn, dusk
}

// SolarDaysForRange returns a SolarDay for each UTC calendar day that overlaps
// [start, end], computed at the given lat/lon. The slice is ordered chronologically.
func SolarDaysForRange(lat, lon float64, start, end time.Time) []SolarDay {
	// Iterate UTC calendar days from the day containing start through the day containing end.
	first := time.Date(start.UTC().Year(), start.UTC().Month(), start.UTC().Day(), 0, 0, 0, 0, time.UTC)
	last := time.Date(end.UTC().Year(), end.UTC().Month(), end.UTC().Day(), 0, 0, 0, 0, time.UTC)
	var days []SolarDay
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		dawn, dusk := CivilDawnDusk(lat, lon, d)
		days = append(days, SolarDay{
			Date: d.Format("2006-01-02"),
			Dawn: dawn,
			Dusk: dusk,
		})
	}
	return days
}
