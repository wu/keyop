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

// WebUIPanels returns panels provided by the tides service for the dashboard.
func (svc *Service) WebUIPanels() []webui.PanelInfo {
	return []webui.PanelInfo{
		{
			ID:          "tides",
			Title:       "Tides",
			Content:     `<div class="panel" id="panel-tides"><div class="panel-body">Loading...</div></div>`,
			JSPath:      "/api/assets/tides/tides-panel.js",
			Event:       "tides",
			ServiceType: svc.Cfg.Type,
		},
	}
}

// HandleWebUIAction handles actions from the WebUI.
func (svc *Service) HandleWebUIAction(action string, params map[string]any) (any, error) {
	switch action {
	case "fetch-tides":
		return svc.fetchCurrentTide()
	case "refresh-report":
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
			stationLoc := d.Location()
			if svc.tz != nil {
				stationLoc = svc.tz
			}
			dayInStation := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, stationLoc)
			sunrise, sunset := sunriseSunset(svc.lat, svc.lon, svc.alt, dayInStation)
			svc.mu.RLock()
			threshold := svc.lowTideThreshold
			svc.mu.RUnlock()
			periods := daylightLowPeriods(records, dayInStation, sunrise, sunset, threshold)
			allPeriods = append(allPeriods, periods...)
		}

		stationLabel := svc.stationID
		if svc.stationName != "" {
			stationLabel = svc.stationName
		}

		report := formatTideReport(allPeriods, svc.lowTideThreshold, stationLabel)
		return map[string]string{"status": "ok", "report": report}, nil
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// fetchCurrentTide returns the current tide information including next peak.
func (svc *Service) fetchCurrentTide() (any, error) {
	logger := svc.Deps.MustGetLogger()
	now := time.Now()

	// Ensure tide data files are loaded (just like Check() does)
	if err := svc.ensureDayFiles(now); err != nil {
		logger.Error("tides: failed to refresh tide data", "error", err)
		return nil, err
	}

	// Get tide records around now
	svc.mu.RLock()
	records, err := svc.collectRecordsAroundNow(now)
	svc.mu.RUnlock()
	if err != nil {
		logger.Error("tides: failed to collect tide records", "error", err)
		return nil, err
	}

	logger.Debug("tides: fetchCurrentTide", "recordCount", len(records))

	// Find current tide
	current, next, err := findCurrentTide(records, now)
	if err != nil {
		return nil, err
	}

	logger.Debug("tides: found current tide", "current", current)

	state := tideState(records, current)
	peak := nextPeak(records, current)

	logger.Debug("tides: calculated peak", "peak", peak, "peakIsNil", peak == nil)

	// Build the TideEvent structure
	ev := TideEvent{
		StationID: svc.stationID,
		Current:   *current,
		State:     state,
		NextPeak:  peak,
	}
	if next != nil {
		ev.Next = next
	}

	// Extract sparkline data: filter records within 12 hours before and 24 hours after now
	twelveHoursAgo := now.Add(-12 * time.Hour)
	twentyFourHoursFromNow := now.Add(24 * time.Hour)

	var sparklineRecords []TideRecord
	for _, r := range records {
		recordTime, err := time.Parse(noaaTimeFormat, r.Time)
		if err != nil {
			continue
		}
		if recordTime.After(twelveHoursAgo) && recordTime.Before(twentyFourHoursFromNow) {
			sparklineRecords = append(sparklineRecords, r)
		}
	}

	logger.Debug("tides: collected sparkline records", "count", len(sparklineRecords))

	return map[string]any{
		"event":            ev,
		"sparklineRecords": sparklineRecords,
		"currentLevel":     current.Value,
		"peakLevel":        peak.Value,
		"state":            state,
	}, nil
}
