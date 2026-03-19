package heartbeat

import (
	"keyop/x/webui"
	"net/http"
)

// WebUIAssets returns the static assets for the heartbeat service.
func (svc *Service) WebUIAssets() http.FileSystem {
	return http.Dir("x/heartbeat/resources")
}

// WebUITab returns the tab configuration for the heartbeat service.
func (svc *Service) WebUITab() webui.TabInfo {
	return webui.TabInfo{
		ID:    "heartbeat",
		Title: "Heartbeat",
		Content: `<div id="heartbeat-container">
<div style="padding: 16px;">
  <div id="heartbeat-list" style="font-size: 0.85rem;">Loading heartbeat data...</div>
</div>
</div>`,
		JSPath: "/api/assets/heartbeat/heartbeat-tab.js",
	}
}

// WebUIPanels returns panels provided by the heartbeat service for the dashboard.
func (svc *Service) WebUIPanels() []webui.PanelInfo {
	return []webui.PanelInfo{
		{
			ID:          "heartbeat",
			Title:       "Heartbeat",
			Content:     `<div class="panel" id="panel-heartbeat"><div class="panel-body">Loading...</div></div>`,
			JSPath:      "/api/assets/heartbeat/heartbeat-panel.js",
			Event:       "uptime_check",
			ServiceType: svc.Cfg.Type,
		},
	}
}
