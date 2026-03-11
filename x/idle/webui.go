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
		Title: "Idle Report",
		Content: `<div id="idle-container">
<h3>Idle Status</h3>
<div id="idle-status">Loading status...</div>
<div id="idle-report-section">
<h3>Recent Daily Report</h3>
<div id="idle-report-controls">
<label>Start: <input type="date" id="idle-start-date"></label>
<label>End: <input type="date" id="idle-end-date"></label>
<button id="idle-refresh-btn">Refresh</button>
</div>
<div id="idle-report-content">Loading report...</div>
</div>
<h3>Activity Log</h3>
<div id="idle-history"></div>
</div>`,
		JSPath:         "/api/assets/idleMacos/idle.js",
		RenderMarkdown: true,
	}
}

// HandleWebUIAction handles actions from the WebUI.
func (svc *Service) HandleWebUIAction(action string, params map[string]any) (any, error) {
	if action == "refresh-report" {
		messenger := svc.Deps.MustGetMessenger()
		// If params has start/end, use them
		var start, end time.Time
		if s, ok := params["start"].(string); ok {
			start, _ = time.Parse(time.RFC3339, s)
		}
		if e, ok := params["end"].(string); ok {
			end, _ = time.Parse(time.RFC3339, e)
		}

		report, err := svc.generateIdleReport(messenger, time.Now(), start, end, true)
		if err != nil {
			return nil, err
		}
		return map[string]string{"status": "ok", "report": report}, nil
	}
	return nil, fmt.Errorf("unknown action: %s", action)
}
