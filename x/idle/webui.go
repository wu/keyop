package idle

import (
	"embed"
	"fmt"
	"io/fs"
	"keyop/x/webui"
	"net/http"
	"time"
)

//go:embed resources
var embeddedAssets embed.FS

// WebUIAssets returns the static assets for the idle service.
func (svc *Service) WebUIAssets() http.FileSystem {
	sub, _ := fs.Sub(embeddedAssets, "resources")
	return http.FS(sub)
}

// WebUITab returns the tab configuration for the idle service.
func (svc *Service) WebUITab() webui.TabInfo {
	return webui.TabInfo{
		ID:    "idle",
		Title: "Idle",
		Content: `<div id="idle-container">
<div id="idle-status">Loading status...</div>
<div id="idle-totals"></div>
<div id="idle-report-section">
<canvas id="idle-report-canvas"></canvas>
<div id="idle-summary">
<table id="idle-active-periods-table">
<thead>
<tr><th>Hostname</th><th>Start</th><th>Stop</th><th>Duration</th></tr>
</thead>
<tbody id="idle-periods-body"></tbody>
</table>
</div>
</div>
<h3 style="color: #c77dff;">Activity Log</h3>
<div id="idle-history"></div>
</div>`,
		JSPath:         "/api/assets/idle/idle.js",
		RenderMarkdown: false,
	}
}

// WebUIPanels returns panels provided by the idle service for the dashboard.
func (svc *Service) WebUIPanels() []webui.PanelInfo {
	return []webui.PanelInfo{
		{
			ID:          "idle",
			Title:       "Idle",
			Content:     `<div class="panel" id="panel-idle"><div class="panel-body">Loading...</div></div>`,
			JSPath:      "/api/assets/idle/idle-panel.js",
			Event:       "idle",
			ServiceType: svc.Cfg.Type,
		},
	}
}

// HandleWebUIAction handles actions from the WebUI.
func (svc *Service) HandleWebUIAction(action string, params map[string]any) (any, error) {
	switch action {
	case "refresh-report":
		// Legacy: markdown-based report for backward compatibility
		messenger := svc.Deps.MustGetMessenger()
		var start, end time.Time
		if s, ok := params["start"].(string); ok {
			start, _ = time.Parse(time.RFC3339, s)
		}
		if e, ok := params["end"].(string); ok {
			end, _ = time.Parse(time.RFC3339, e)
		}

		md, _, err := svc.generateIdleReport(messenger, time.Now(), start, end, true)
		if err != nil {
			return nil, err
		}
		return map[string]string{"status": "ok", "report": md}, nil

	case "fetch-idle-report":
		// New: structured report data for incremental updates
		messenger := svc.Deps.MustGetMessenger()
		var start, end time.Time
		if s, ok := params["start"].(string); ok {
			utcTime, _ := time.Parse(time.RFC3339, s)
			// Convert UTC time to local time to match database storage format
			start = utcTime.In(time.Local)
		}
		if e, ok := params["end"].(string); ok {
			utcTime, _ := time.Parse(time.RFC3339, e)
			// Convert UTC time to local time to match database storage format
			end = utcTime.In(time.Local)
		}

		_, report, err := svc.generateIdleReport(messenger, time.Now(), start, end, true)
		if err != nil {
			return nil, err
		}
		if report == nil {
			return map[string]any{"status": "ok", "data": nil}, nil
		}
		// Return as JSON (will be marshaled by webui)
		return map[string]any{"status": "ok", "data": report}, nil

	case "fetch-idle-dashboard":
		// Dashboard panel: current state and 24-hour totals
		return svc.fetchIdleDashboard()

	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// fetchIdleDashboard returns the current idle state and 24-hour totals for the dashboard panel.
func (svc *Service) fetchIdleDashboard() (any, error) {
	type DashboardData struct {
		CurrentStatus          string  `json:"currentStatus"`          // "idle", "active", or "unknown"
		TimeSinceChangeSeconds float64 `json:"timeSinceChangeSeconds"` // seconds in current state
		PreviousStatus         string  `json:"previousStatus"`         // previous state
		TimeInPreviousSeconds  float64 `json:"timeInPreviousSeconds"`  // seconds in previous state
		TotalIdleSeconds       float64 `json:"totalIdleSeconds"`       // last 24h
		TotalActiveSeconds     float64 `json:"totalActiveSeconds"`     // last 24h
		TotalUnknownSeconds    float64 `json:"totalUnknownSeconds"`    // last 24h
	}

	data := DashboardData{
		CurrentStatus:          "unknown",
		TimeSinceChangeSeconds: 0,
		PreviousStatus:         "unknown",
		TimeInPreviousSeconds:  0,
		TotalIdleSeconds:       0,
		TotalActiveSeconds:     0,
		TotalUnknownSeconds:    0,
	}

	// Get current state from memory
	if svc.isIdle {
		data.CurrentStatus = "idle"
	} else {
		data.CurrentStatus = "active"
	}
	data.TimeSinceChangeSeconds = time.Since(svc.lastTransition).Seconds()

	// Get 24-hour report to calculate totals
	messenger := svc.Deps.MustGetMessenger()
	_, report, err := svc.generateIdleReport(messenger, time.Now(), time.Time{}, time.Time{}, true)
	if err == nil && report != nil {
		data.TotalIdleSeconds = report.TotalIdleDurationSecs
		data.TotalActiveSeconds = report.TotalActiveDurationSecs
		data.TotalUnknownSeconds = report.TotalUnknownDurationSecs
	}

	// Try to determine previous status from database
	if svc.db != nil && *svc.db != nil {
		db := *svc.db
		var status string
		var isIdle bool
		// Get the most recent idle_event before the current state's last transition
		err := db.QueryRow(`
			SELECT status, idle_seconds, active_seconds
			FROM idle_events
			WHERE timestamp < ?
			ORDER BY timestamp DESC
			LIMIT 1
		`, svc.lastTransition).Scan(&status, nil, nil)
		if err == nil {
			if status == "idle" {
				data.PreviousStatus = "idle"
				isIdle = true
			} else {
				data.PreviousStatus = "active"
				isIdle = false
			}
			// Calculate time in previous state (approximate: from previous event to transition)
			if isIdle == svc.isIdle {
				// Edge case: if detected state is same as previous, previous duration is 0
				data.TimeInPreviousSeconds = 0
			} else {
				// Get the event before the last transition to estimate previous duration
				var prevTs time.Time
				err := db.QueryRow(`
					SELECT timestamp
					FROM idle_events
					WHERE timestamp < ?
					ORDER BY timestamp DESC
					LIMIT 2
				`, svc.lastTransition).Scan(&prevTs)
				if err == nil && !prevTs.IsZero() {
					data.TimeInPreviousSeconds = svc.lastTransition.Sub(prevTs).Seconds()
				}
			}
		}
	}

	return map[string]any{"status": "ok", "data": data}, nil
}
