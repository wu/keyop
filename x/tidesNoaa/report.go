// Package tidesNoaa provides NOAA tide-data fetching, local storage, reports,
// and extreme-tide detection for the keyop project.
//
//nolint:revive
package tidesNoaa

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"keyop/core"

	"github.com/sj14/astral/pkg/astral"
)

const noaaMetadataBase = "https://api.tidesandcurrents.noaa.gov/mdapi/prod/webapi/stations"

// fetchStationInfo queries the NOAA metadata API and returns the latitude,
// longitude, and name for stationID.  metadataBase may be overridden in
// tests; pass noaaMetadataBase for production.
func fetchStationInfo(logger core.Logger, metadataBase, stationID string) (lat, lon float64, name string, err error) {
	url := fmt.Sprintf("%s/%s.json", metadataBase, stationID)
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, 0, "", err
	}
	req.Header.Set("User-Agent", "keyop (https://github.com/keyop/keyop)")

	// client.Do is called with a constructed NOAA URL; this is not user-supplied
	// network input and is safe. Suppress the gosec G704 warning here.
	resp, err := client.Do(req) // #nosec G704
	if err != nil {
		return 0, 0, "", err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			if logger != nil {
				logger.Warn("tidesNoaa: failed to close metadata response body", "error", cerr)
			}
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, "", fmt.Errorf("NOAA metadata API returned status %d for station %s", resp.StatusCode, stationID)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, "", err
	}

	var apiResp struct {
		Stations []struct {
			Lat  float64 `json:"lat"`
			Lng  float64 `json:"lng"`
			Name string  `json:"name"`
		} `json:"stations"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return 0, 0, "", fmt.Errorf("failed to parse NOAA metadata response for station %s: %w", stationID, err)
	}
	if len(apiResp.Stations) == 0 {
		return 0, 0, "", fmt.Errorf("NOAA metadata API returned no station for ID %s", stationID)
	}

	s := apiResp.Stations[0]
	if s.Lat == 0 && s.Lng == 0 {
		return 0, 0, "", fmt.Errorf("NOAA metadata API returned zero coordinates for station %s", stationID)
	}
	return s.Lat, s.Lng, s.Name, nil
}

// LowTidePeriod describes a contiguous window during which the tide is at or
// below a given threshold AND overlaps with daylight hours.
type LowTidePeriod struct {
	Date      string        `json:"date"`      // YYYY-MM-DD
	DayOfWeek string        `json:"dayOfWeek"` // e.g. "Monday"
	Start     time.Time     `json:"start"`
	End       time.Time     `json:"end"`
	Duration  time.Duration `json:"duration"`
	MinValue  float64       `json:"minValue"` // lowest reading within the period
	MinTime   time.Time     `json:"minTime"`  // time of the lowest reading
}

// FormatDuration renders a duration as "Xh Ym" (e.g. "2h 18m").
func FormatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// sunriseSunset returns the civil dawn and civil dusk for day t at the given
// observer location.  On error (e.g. polar day/night) it falls back to
// midnight–midnight so no periods are suppressed due to calculation failure.
func sunriseSunset(lat, lon, alt float64, t time.Time) (rise, set time.Time) {
	observer := astral.Observer{
		Latitude:  lat,
		Longitude: lon,
		Elevation: alt,
	}
	rise, err := astral.Dawn(observer, t, astral.DepressionCivil)
	if err != nil {
		rise = localMidnight(t)
	}
	set, err = astral.Dusk(observer, t, astral.DepressionCivil)
	if err != nil {
		set = localMidnight(t).Add(24 * time.Hour)
	}
	return rise, set
}

// daylightLowPeriods scans records for the given calendar day and returns all
// contiguous segments where the water level is at or below threshold AND the
// time falls within [sunrise, sunset].
//
// records must be sorted chronologically and may span multiple calendar days
// (e.g. yesterday + today + tomorrow); only records whose parsed time falls on
// the calendar date of dayDate are considered.
//
// Each 6-minute record is treated as a point sample.  A period opens when a
// record at or below threshold is encountered inside daylight, and closes when
// a record exceeds threshold or daylight ends, whichever comes first.  The
// start time is the first qualifying record; the end time is the last
// qualifying record in the segment (not the first record that exceeded the
// threshold).
func daylightLowPeriods(records []TideRecord, dayDate time.Time, sunrise, sunset time.Time, threshold float64) []LowTidePeriod {
	dateStr := dayDate.Format(fileDateFormat)
	dow := dayDate.Format("Monday")

	var periods []LowTidePeriod
	inPeriod := false
	var periodStart time.Time
	var lastTime time.Time
	var minVal float64
	var minTime time.Time

	for _, r := range records {
		t, err := time.ParseInLocation(noaaTimeFormat, r.Time, dayDate.Location())
		if err != nil {
			continue
		}
		// Only consider records that fall within daylight on this calendar day.
		if t.Before(sunrise) || t.After(sunset) {
			if inPeriod {
				// Daylight ended while we were in a low period — close it.
				periods = append(periods, LowTidePeriod{
					Date:      dateStr,
					DayOfWeek: dow,
					Start:     periodStart,
					End:       lastTime,
					Duration:  lastTime.Sub(periodStart),
					MinValue:  minVal,
					MinTime:   minTime,
				})
				inPeriod = false
			}
			continue
		}

		if r.Value <= threshold {
			if !inPeriod {
				inPeriod = true
				periodStart = t
				minVal = r.Value
				minTime = t
			} else if r.Value < minVal {
				minVal = r.Value
				minTime = t
			}
			lastTime = t
		} else {
			if inPeriod {
				periods = append(periods, LowTidePeriod{
					Date:      dateStr,
					DayOfWeek: dow,
					Start:     periodStart,
					End:       lastTime,
					Duration:  lastTime.Sub(periodStart),
					MinValue:  minVal,
					MinTime:   minTime,
				})
				inPeriod = false
			}
		}
	}

	// Close any open period at end of records.
	if inPeriod {
		periods = append(periods, LowTidePeriod{
			Date:      dateStr,
			DayOfWeek: dow,
			Start:     periodStart,
			End:       lastTime,
			Duration:  lastTime.Sub(periodStart),
			MinValue:  minVal,
			MinTime:   minTime,
		})
	}

	return periods
}

// formatTideReport renders a slice of LowTidePeriods as a Markdown document
// with a single table covering all days.
// stationLabel is shown in the header; pass the station name when available,
// falling back to the station ID.
func formatTideReport(periods []LowTidePeriod, threshold float64, stationLabel string) string {
	if len(periods) == 0 {
		return fmt.Sprintf("## Tide Report — %s\n\nNo daylight low tides (≤ %.1f ft) in the next 7 days.\n",
			stationLabel, threshold)
	}

	out := fmt.Sprintf("## Tide Report — %s\n\nDaylight low tides ≤ %.1f ft:\n\n", stationLabel, threshold)
	out += "| Date | Day | Start | End | Duration | Min | Min Time |\n"
	out += "|------|-----|-------|-----|----------|-----|----------|\n"
	for _, p := range periods {
		out += fmt.Sprintf("| %s | %s | %s | %s | %s | %.2f ft | %s |\n",
			p.Date,
			p.DayOfWeek,
			p.Start.Format("15:04"),
			p.End.Format("15:04"),
			FormatDuration(p.Duration),
			p.MinValue,
			p.MinTime.Format("15:04"),
		)
	}
	return out
}
