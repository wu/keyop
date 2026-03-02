package tidesNoaa

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

const (
	noaaAPIBase    = "https://api.tidesandcurrents.noaa.gov/api/prod/datagetter"
	noaaDateFormat = "20060102"
	noaaTimeFormat = "2006-01-02 15:04"
	fileDateFormat = "2006-01-02"
	fetchDays      = 10
)

// localMidnight returns the start of the calendar day for t in t's location.
// Use this instead of t.Truncate(24*time.Hour) for "today/past/future day"
// comparisons; Truncate operates in UTC and gives wrong results in non-UTC zones.
func localMidnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// TideRecord represents a single water-level reading as returned by the
// NOAA CO-OPS data getter (6-minute interval) and stored in the daily YAML file.
type TideRecord struct {
	Time  string  `yaml:"time"  json:"t"`
	Value float64 `yaml:"value" json:"v,string"`
}

// TideDayFile is the structure written to / read from one daily YAML file.
type TideDayFile struct {
	StationID string       `yaml:"stationId"`
	Date      string       `yaml:"date"` // YYYY-MM-DD
	FetchedAt time.Time    `yaml:"fetchedAt"`
	Records   []TideRecord `yaml:"records"`
}

// dayFileStale returns true when a day file should be re-fetched.
// Staleness policy:
//   - Past days (before today):   never stale — historical data doesn't change.
//   - Today (offset 0):           stale if fetched more than 1 hour ago.
//   - Tomorrow (offset +1):       stale if fetched more than 1 hour ago.
//   - Day +2 and beyond:          fresh once written — NOAA predictions don't
//     change meaningfully at that range.
//
// dayOffset is the signed number of calendar days between now and day
// (positive = future, negative = past). Callers must compute this correctly
// using localMidnight so DST/timezone boundaries are handled.
func dayFileStale(f *TideDayFile, dayOffset int, now time.Time) bool {
	if f == nil || len(f.Records) == 0 {
		return true
	}
	if dayOffset < 0 {
		return false // past day — never re-fetch
	}
	if dayOffset <= 1 {
		return now.Sub(f.FetchedAt) > time.Hour // today or tomorrow
	}
	return false // day +2 and beyond: fresh once written
}

// fetchDayRecords calls the NOAA CO-OPS API for a single day and returns the
// 6-minute water-level records for that day.
// apiBase may be overridden in tests; pass noaaAPIBase for production.
func fetchDayRecords(apiBase, stationID string, day time.Time) ([]TideRecord, error) {
	date := day.Format(noaaDateFormat)
	url := fmt.Sprintf(
		"%s?product=predictions&application=keyop&begin_date=%s&end_date=%s&datum=MLLW&time_zone=lst_ldt&interval=6&units=english&format=json&station=%s",
		apiBase, date, date, stationID,
	)

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "keyop (https://github.com/keyop/keyop)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("NOAA API returned status %d for %s", resp.StatusCode, date)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var apiResp struct {
		Predictions []TideRecord `json:"predictions"`
		Error       *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse NOAA response for %s: %w", date, err)
	}
	if apiResp.Error != nil {
		return nil, fmt.Errorf("NOAA API error for %s: %s", date, apiResp.Error.Message)
	}
	if len(apiResp.Predictions) == 0 {
		return nil, fmt.Errorf("NOAA API returned no records for %s", date)
	}

	return apiResp.Predictions, nil
}

// TideExtremeEntry is a single reading on a high or low leaderboard.
type TideExtremeEntry struct {
	Value      float64   `json:"value"`
	Time       string    `json:"time"`
	RecordedAt time.Time `json:"recordedAt"`
}

// TideWindowExtremes holds the leaderboard for one time window.
// Highs is sorted descending (highest first); Lows ascending (lowest first).
type TideWindowExtremes struct {
	Highs []TideExtremeEntry `json:"highs"`
	Lows  []TideExtremeEntry `json:"lows"`
}

// High returns the current highest entry, or a zero value if the list is empty.
func (w TideWindowExtremes) High() TideExtremeEntry {
	if len(w.Highs) == 0 {
		return TideExtremeEntry{}
	}
	return w.Highs[0]
}

// Low returns the current lowest entry, or a zero value if the list is empty.
func (w TideWindowExtremes) Low() TideExtremeEntry {
	if len(w.Lows) == 0 {
		return TideExtremeEntry{}
	}
	return w.Lows[0]
}

// lunarCycle is the window length used for extreme-tide tracking.  28 days is
// chosen instead of the astronomical synodic period (~29.5 days) so that a
// record from the equivalent phase last month always falls outside the window
// by the time the same phase recurs this month.  With one daily high/low
// reading per day this prevents last month's extreme from perpetually
// suppressing an alert for this month's equivalent tide.
const lunarCycle = 28 * 24 * time.Hour

// TideExtremes holds leaderboards for the 1, 3, and 12 lunar-cycle windows.
// It is persisted via the state store and updated incrementally on each Check.
type TideExtremes struct {
	Window1Lunar  TideWindowExtremes `json:"window1Lunar"`
	Window3Lunar  TideWindowExtremes `json:"window3Lunar"`
	Window12Lunar TideWindowExtremes `json:"window12Lunar"`
}

// maxExtremeEntries caps each high/low leaderboard. One entry per day means
// 365 days × 1 entry = 365 max; 400 gives comfortable headroom.
const maxExtremeEntries = 400

// updateExtremes returns a new TideExtremes that incorporates the daily high
// and low from records into every applicable window, evicts entries that have
// aged past each window cutoff, and trims each leaderboard.
// records should be all readings for a single day (e.g. one TideDayFile).
// dayDate is the calendar date of those records, used for window membership.
func updateExtremes(ex TideExtremes, records []TideRecord, dayDate time.Time, now time.Time) TideExtremes {
	high, low := dailyHighLow(records, dayDate)
	if high == nil {
		return ex // no parseable records
	}

	type windowSpec struct {
		cutoff time.Time
		window *TideWindowExtremes
	}
	windows := []windowSpec{
		{now.Add(-1 * lunarCycle), &ex.Window1Lunar},
		{now.Add(-3 * lunarCycle), &ex.Window3Lunar},
		{now.Add(-12 * lunarCycle), &ex.Window12Lunar},
	}

	for _, ws := range windows {
		if !dayDate.Before(ws.cutoff) {
			ws.window.Highs = append(ws.window.Highs, *high)
			ws.window.Lows = append(ws.window.Lows, *low)
		}

		ws.window.Highs = filterEntries(ws.window.Highs, ws.cutoff)
		ws.window.Lows = filterEntries(ws.window.Lows, ws.cutoff)

		sort.Slice(ws.window.Highs, func(i, j int) bool {
			return ws.window.Highs[i].Value > ws.window.Highs[j].Value // descending
		})
		sort.Slice(ws.window.Lows, func(i, j int) bool {
			return ws.window.Lows[i].Value < ws.window.Lows[j].Value // ascending
		})
		if len(ws.window.Highs) > maxExtremeEntries {
			ws.window.Highs = ws.window.Highs[:maxExtremeEntries]
		}
		if len(ws.window.Lows) > maxExtremeEntries {
			ws.window.Lows = ws.window.Lows[:maxExtremeEntries]
		}
	}

	return ex
}

// dailyHighLow scans records and returns the single highest and lowest entry.
// Both returned pointers reference the same RecordedAt (dayDate at noon) so
// the leaderboard can age them out correctly by day.
// Returns nil, nil if no records can be parsed.
func dailyHighLow(records []TideRecord, dayDate time.Time) (high *TideExtremeEntry, low *TideExtremeEntry) {
	// Use noon of the day as the canonical RecordedAt so expiry is day-aligned.
	recordedAt := time.Date(dayDate.Year(), dayDate.Month(), dayDate.Day(), 12, 0, 0, 0, dayDate.Location())
	for _, r := range records {
		e := TideExtremeEntry{Value: r.Value, Time: r.Time, RecordedAt: recordedAt}
		if high == nil || r.Value > high.Value {
			cp := e
			high = &cp
		}
		if low == nil || r.Value < low.Value {
			cp := e
			low = &cp
		}
	}
	return
}

// filterEntries returns a new slice containing only entries whose RecordedAt
// is not before cutoff. A fresh allocation is always returned so that Highs
// and Lows never share a backing array and cannot corrupt each other.
func filterEntries(entries []TideExtremeEntry, cutoff time.Time) []TideExtremeEntry {
	out := make([]TideExtremeEntry, 0, len(entries))
	for _, e := range entries {
		if !e.RecordedAt.Before(cutoff) {
			out = append(out, e)
		}
	}
	return out
}

// TidePeak describes the next high or low tide.
type TidePeak struct {
	Time  string  `yaml:"time"`
	Value float64 `yaml:"value"`
	Type  string  `yaml:"type"` // "high" or "low"
}

// findCurrentTide returns the most-recent record at or before now and the
// next record after now, scanning across all provided records in order.
// When all records are in the future the first entry is used as current.
func findCurrentTide(records []TideRecord, now time.Time) (*TideRecord, *TideRecord, error) {
	if len(records) == 0 {
		return nil, nil, fmt.Errorf("no tide records available")
	}

	var current *TideRecord
	var next *TideRecord

	for i := range records {
		t, err := time.ParseInLocation(noaaTimeFormat, records[i].Time, time.Local)
		if err != nil {
			continue
		}
		if !t.After(now) {
			p := records[i]
			current = &p
		} else if next == nil {
			p := records[i]
			next = &p
			if current != nil {
				break
			}
		}
	}

	if current == nil {
		// All records are in the future; use the first one.
		p := records[0]
		current = &p
		if len(records) > 1 {
			p2 := records[1]
			next = &p2
		}
	}

	return current, next, nil
}

// tideState returns "rising" or "falling" by comparing the current record to
// the one immediately preceding it in the slice. Returns "" if undetermined.
func tideState(records []TideRecord, current *TideRecord) string {
	if current == nil {
		return ""
	}
	// Find the index of current in the slice.
	idx := -1
	for i := range records {
		if records[i].Time == current.Time {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return ""
	}
	if records[idx].Value > records[idx-1].Value {
		return "rising"
	}
	return "falling"
}

// peakDir classifies the direction from value a to b:
//
//	+1 = rising, -1 = falling, 0 = flat (equal)
func peakDir(a, b float64) int {
	if b > a {
		return 1
	}
	if b < a {
		return -1
	}
	return 0
}

// recentPeaks scans backwards from the current record up to lookback positions
// and returns all local extrema found in that window.  This ensures a peak is
// not silently missed when Check() runs slightly late and the peak record has
// already slipped behind the current position.
//
// Plateau handling: consecutive equal values are treated as a single flat
// segment. A plateau qualifies as a peak when the slope before the plateau and
// the slope after it point in opposite directions. The first record of the
// plateau is reported as the peak time/value. Only one TidePeak is emitted per
// plateau so duplicate alerts are not generated.
//
// The first and last records in the slice are never candidates (no neighbours).
func recentPeaks(records []TideRecord, current *TideRecord, lookback int) []TidePeak {
	if current == nil || len(records) < 3 {
		return nil
	}
	idx := -1
	for i := range records {
		if records[i].Time == current.Time {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}

	start := idx - lookback
	if start < 1 {
		start = 1
	}
	end := idx
	if end > len(records)-2 {
		end = len(records) - 2
	}

	var peaks []TidePeak
	i := start
	for i <= end {
		leftDir := peakDir(records[i-1].Value, records[i].Value)
		if leftDir == 0 {
			i++
			continue // flat approach — can't determine direction yet
		}

		// Scan over any plateau starting at i.
		plateauStart := i
		j := i
		for j < len(records)-1 && records[j+1].Value == records[j].Value {
			j++
		}
		if j >= len(records)-1 {
			break // plateau extends to end — no right neighbour
		}

		rightDir := peakDir(records[j].Value, records[j+1].Value)
		if rightDir != 0 && leftDir != rightDir {
			peakType := "high"
			if leftDir < 0 {
				peakType = "low"
			}
			peaks = append(peaks, TidePeak{
				Time:  records[plateauStart].Time,
				Value: records[plateauStart].Value,
				Type:  peakType,
			})
		}
		// Advance past the plateau to avoid double-reporting.
		i = j + 1
	}
	return peaks
}

// nextPeak scans forward from the current record and returns the first local
// extremum (direction reversal) after the current position.
//
// Plateau handling: equal consecutive values are treated as a single flat
// segment. A plateau is reported as a peak when the slope entering the plateau
// reverses after the plateau ends. The first record of the plateau is used as
// the peak time/value.
//
// Returns nil if no peak is found in the available records.
func nextPeak(records []TideRecord, current *TideRecord) *TidePeak {
	if current == nil || len(records) < 3 {
		return nil
	}
	idx := -1
	for i := range records {
		if records[i].Time == current.Time {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}

	// lastDir tracks the most-recent non-zero direction so we can detect a
	// reversal after a flat plateau.
	lastDir := 0
	i := idx + 1
	for i < len(records) {
		d := peakDir(records[i-1].Value, records[i].Value)
		if d == 0 {
			// Flat segment — skip but keep lastDir unchanged.
			i++
			continue
		}
		if lastDir != 0 && d != lastDir {
			// Direction reversal detected at index i.
			// The peak is the first record of any plateau that preceded this
			// reversal: walk back from i-1 over the flat run.
			plateauEnd := i - 1
			plateauStart := plateauEnd
			for plateauStart > idx && records[plateauStart-1].Value == records[plateauEnd].Value {
				plateauStart--
			}
			peakType := "high"
			if lastDir < 0 {
				peakType = "low"
			}
			return &TidePeak{
				Time:  records[plateauStart].Time,
				Value: records[plateauStart].Value,
				Type:  peakType,
			}
		}
		lastDir = d
		i++
	}
	return nil
}
