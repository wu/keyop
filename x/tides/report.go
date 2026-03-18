// Package tides provides NOAA tide-data fetching, local storage, reports,
// and extreme-tide detection for the keyop project.
package tides

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
func fetchStationInfo(logger core.Logger, metadataBase, stationID string) (lat, lon float64, name, tz string, err error) {
	url := fmt.Sprintf("%s/%s.json", metadataBase, stationID)
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, 0, "", "", err
	}
	req.Header.Set("User-Agent", "keyop (https://github.com/keyop/keyop)")

	// client.Do is called with a constructed NOAA URL; this is not user-supplied
	// network input and is safe. Suppress the gosec G704 warning here.
	resp, err := client.Do(req) // #nosec G704
	if err != nil {
		return 0, 0, "", "", err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			if logger != nil {
				logger.Warn("tides: failed to close metadata response body", "error", cerr)
			}
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, "", "", fmt.Errorf("NOAA metadata API returned status %d for station %s", resp.StatusCode, stationID)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, "", "", err
	}

	// Parse into a generic map so we can attempt to extract a timezone if present.
	var apiResp map[string]any
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return 0, 0, "", "", fmt.Errorf("failed to parse NOAA metadata response for station %s: %w", stationID, err)
	}

	stationsRaw, ok := apiResp["stations"].([]any)
	if !ok || len(stationsRaw) == 0 {
		return 0, 0, "", "", fmt.Errorf("NOAA metadata API returned no station for ID %s", stationID)
	}

	first, ok := stationsRaw[0].(map[string]any)
	if !ok {
		return 0, 0, "", "", fmt.Errorf("unexpected station metadata format for %s", stationID)
	}

	// Extract lat/lng/name if available
	if v, ok := first["lat"].(float64); ok {
		lat = v
	}
	if v, ok := first["lng"].(float64); ok {
		lon = v
	}
	if v, ok := first["name"].(string); ok {
		name = v
	}

	// Prefer the explicit top-level timezone fields when present. NOAA often
	// provides a 'timezone' (or 'timeZone') field; use that first. If absent,
	// look for the same keys inside nested objects. Avoid scanning arbitrary
	// string fields for '/' — that mistakenly picked up URLs in metadata.
	if v, ok := first["timezone"].(string); ok && v != "" {
		tz = v
	} else if v, ok := first["timeZone"].(string); ok && v != "" {
		tz = v
	} else {
		for _, v := range first {
			if m, ok := v.(map[string]any); ok {
				if s, ok := m["timezone"].(string); ok && s != "" {
					tz = s
					break
				}
				if s, ok := m["timeZone"].(string); ok && s != "" {
					tz = s
					break
				}
			}
		}
	}

	if lat == 0 && lon == 0 {
		return 0, 0, name, tz, fmt.Errorf("NOAA metadata API returned zero coordinates for station %s", stationID)
	}
	return lat, lon, name, tz, nil
}

// LowTidePeriod describes a contiguous window during which the tide is at or
// below a given threshold AND overlaps with daylight hours.
type LowTidePeriod struct {
	Date      string        `json:"date"`      // YYYY-MM-DD
	DayOfWeek string        `json:"dayOfWeek"` // e.g. "Monday"
	Start     time.Time     `json:"start"`
	End       time.Time     `json:"end"`
	Sunset    time.Time     `json:"sunset"`
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
	// Use UTC date to avoid depending on the provided time.Location when
	// performing astronomical calculations. Convert the resulting times back
	// into the station's location for downstream comparisons and display.
	tUTC := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	riseUTC, err := astral.Dawn(observer, tUTC, astral.DepressionCivil)
	if err != nil {
		riseUTC = localMidnight(tUTC)
	}
	setUTC, err := astral.Dusk(observer, tUTC, astral.DepressionCivil)
	if err != nil {
		setUTC = localMidnight(tUTC).Add(24 * time.Hour)
	}
	// Convert to the station/local time provided in t.
	rise = riseUTC.In(t.Location())
	set = setUTC.In(t.Location())
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
		// Only consider records that fall on this calendar day.
		if t.Format(fileDateFormat) != dateStr {
			// If we were in a period that started earlier on this day, don't let
			// a record from another day affect it — just skip. Do not close here;
			// daylight end logic will handle closing when we encounter an out-of-day
			// record that also lies outside daylight.
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
			Sunset:    sunset,
			Duration:  lastTime.Sub(periodStart),
			MinValue:  minVal,
			MinTime:   minTime,
		})
	}

	// Ensure Sunset is populated for all returned periods in the station's
	// location (some append paths historically omitted it).
	for i := range periods {
		if periods[i].Sunset.IsZero() {
			periods[i].Sunset = sunset.In(dayDate.Location())
		}
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
	out += "| Date | Day | Sunset | Start | End | Duration | Min | Min Time |\n"
	out += "|------|-----|--------|-------|-----|----------|-----|----------|\n"
	for _, p := range periods {
		out += fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %.2f ft | %s |\n",
			p.Date,
			p.DayOfWeek,
			p.Sunset.Format("3:04pm"),
			p.Start.Format("3:04pm"),
			p.End.Format("3:04pm"),
			FormatDuration(p.Duration),
			p.MinValue,
			p.MinTime.Format("3:04pm"),
		)
	}
	return out
}
