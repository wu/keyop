package aurora

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ParsedForecast represents a structured version of the 3-day text forecast.
type ParsedForecast struct {
	Issued  string               `json:"issued,omitempty"`
	Days    []string             `json:"days,omitempty"`
	Periods []string             `json:"periods,omitempty"`
	Table   map[string][]KpEntry `json:"table,omitempty"`
}

// KpEntry represents one KP cell value and optional G-scale note.
type KpEntry struct {
	KP  *float64 `json:"kp,omitempty"`
	Raw string   `json:"raw"`
	G   string   `json:"g_scale,omitempty"`
}

// Parse3DayForecastText parses the plain-text 3-day forecast and returns a structured representation.
func Parse3DayForecastText(body []byte) (*ParsedForecast, error) {
	text := string(body)
	lines := strings.Split(text, "\n")
	pf := &ParsedForecast{Table: map[string][]KpEntry{}}

	// find Issued line
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, ":Issued:") {
			pf.Issued = strings.TrimSpace(strings.TrimPrefix(l, ":Issued:"))
			break
		}
	}

	// find the header line "NOAA Kp index breakdown" and then header row
	headerIdx := -1
	for i, l := range lines {
		if strings.Contains(l, "NOAA Kp index breakdown") {
			// header likely in the next 1-3 lines
			for j := i + 1; j < i+5 && j < len(lines); j++ {
				if strings.TrimSpace(lines[j]) == "" {
					continue
				}
				headerIdx = j
				break
			}
			break
		}
	}
	if headerIdx == -1 {
		return pf, nil // nothing to parse, return empty
	}

	// header line: days separated by 2+ spaces
	headerLine := strings.TrimSpace(lines[headerIdx])
	sepRe := regexp.MustCompile(`\s{2,}`)
	parts := sepRe.Split(headerLine, -1)
	// remove any leading empty part
	if len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	pf.Days = parts

	// parse subsequent rows until empty line or non-matching
	for r := headerIdx + 1; r < len(lines); r++ {
		ln := strings.TrimSpace(lines[r])
		if ln == "" {
			break
		}
		// split by 2+ spaces
		cols := sepRe.Split(lines[r], -1)
		if len(cols) < 2 {
			continue
		}
		// first col is period
		period := strings.TrimSpace(cols[0])
		pf.Periods = append(pf.Periods, period)
		values := []KpEntry{}
		for i := 1; i < len(cols); i++ {
			cell := strings.TrimSpace(cols[i])
			kpEntry := KpEntry{Raw: cell}
			// extract kp and optional (Gx)
			if cell == "" || cell == "-" {
				values = append(values, kpEntry)
				continue
			}
			// find (Gx) if present
			if idx := strings.Index(cell, "("); idx != -1 {
				end := strings.Index(cell, ")")
				if end > idx {
					kpEntry.G = strings.TrimSpace(cell[idx+1 : end])
					cell = strings.TrimSpace(cell[:idx])
				}
			}
			// try parse float
			if kp, err := strconv.ParseFloat(strings.TrimSpace(cell), 64); err == nil {
				kpEntry.KP = &kp
			}
			values = append(values, kpEntry)
		}
		// If there are fewer values than days, pad with empty
		for len(values) < len(pf.Days) {
			values = append(values, KpEntry{Raw: ""})
		}
		pf.Table[period] = values
	}

	// normalize Issued to RFC3339 if possible
	if pf.Issued != "" {
		if t, err := time.Parse("2006 Jan 02 1504 UTC", pf.Issued); err == nil {
			pf.Issued = t.Format(time.RFC3339)
		}
	}

	return pf, nil
}

// MarshalParsedForecast marshals the parsed forecast to JSON bytes.
func MarshalParsedForecast(pf *ParsedForecast) ([]byte, error) {
	return json.Marshal(pf)
}
