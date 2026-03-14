package idle

import (
	"fmt"
	"keyop/x/webui"
	"net/http"
	"time"
)

// WebUIAssets returns the static assets for the idle service.
func (svc *Service) WebUIAssets() http.FileSystem {
	// Return a file system serving from x/idle/resources
	// In production, we might want to use embed.FS
	return http.Dir("x/idle/resources")
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

	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}
