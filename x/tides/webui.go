package tides

import (
	"fmt"
	"keyop/x/webui"
	"net/http"
	"time"
)

// WebUIAssets returns the static assets for the tides service.
func (svc *Service) WebUIAssets() http.FileSystem {
	return http.Dir("x/tides/resources")
}

// WebUITab returns the tab configuration for the tides service.
func (svc *Service) WebUITab() webui.TabInfo {
	return webui.TabInfo{
		ID:    "tides",
		Title: "Tides",
		Content: `<div id="tides-container">
			<h3>Current Tide</h3>
			<div id="tide-status">Loading tide...</div>
			<div id="tide-report-section">
				<h3>Daylight Low Tide Periods</h3>
				<div id="tide-report-controls">
						<label>Start: <input type="date" id="tide-start-date"></label>
						<label>End: <input type="date" id="tide-end-date"></label>
						<button id="tide-refresh-btn">Refresh</button>
					</div>
					<div id="tide-report-content">No data yet</div>
			</div>
			<h3>Tide History</h3>
			<div id="tide-history"></div>
		</div>`,
		JSPath:         "/api/assets/tides/tides.js",
		RenderMarkdown: true,
	}
}

// HandleWebUIAction handles actions from the WebUI.
func (svc *Service) HandleWebUIAction(action string, params map[string]any) (any, error) {
	if action != "refresh-report" {
		return nil, fmt.Errorf("unknown action: %s", action)
	}

	// Determine requested date range; fall back to today -> today+7 if absent.
	var start time.Time
	var end time.Time
	now := time.Now()
	if s, ok := params["start"].(string); ok && s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			start = localMidnight(t)
		}
	}
	if e, ok := params["end"].(string); ok && e != "" {
		if t, err := time.Parse(time.RFC3339, e); err == nil {
			end = localMidnight(t)
		}
	}
	if start.IsZero() {
		start = localMidnight(now)
	}
	if end.IsZero() {
		end = localMidnight(now).AddDate(0, 0, 7)
	}
	if end.Before(start) {
		end = start
	}

	var allPeriods []LowTidePeriod
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		f, err := svc.loadDayFile(d)
		if err != nil || len(f.Records) == 0 {
			continue
		}
		var records []TideRecord
		records = append(records, f.Records...)
		if next, err := svc.loadDayFile(d.AddDate(0, 0, 1)); err == nil {
			records = append(records, next.Records...)
		}
		sunrise, sunset := sunriseSunset(svc.lat, svc.lon, svc.alt, d)
		svc.mu.RLock()
		threshold := svc.lowTideThreshold
		svc.mu.RUnlock()
		periods := daylightLowPeriods(records, d, sunrise, sunset, threshold)
		allPeriods = append(allPeriods, periods...)
	}

	stationLabel := svc.stationID
	if svc.stationName != "" {
		stationLabel = svc.stationName
	}

	report := formatTideReport(allPeriods, svc.lowTideThreshold, stationLabel)
	return map[string]string{"status": "ok", "report": report}, nil
}
