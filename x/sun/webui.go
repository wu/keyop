package sun

import (
	"keyop/x/webui"
	"net/http"
)

// WebUIAssets returns the static assets for the sun service.
func (svc *Service) WebUIAssets() http.FileSystem {
	return http.Dir("x/sun/resources")
}

// WebUIPanels returns panels provided by the sun service for the dashboard.
func (svc *Service) WebUIPanels() []webui.PanelInfo {
	return []webui.PanelInfo{
		{
			ID:          "sun",
			Title:       "Sun",
			Content:     `<div class="panel" id="panel-sun"><div class="panel-body">Loading...</div></div>`,
			JSPath:      "/api/assets/sun/sun-panel.js",
			Event:       "sun_check",
			ServiceType: svc.Cfg.Type,
		},
	}
}
