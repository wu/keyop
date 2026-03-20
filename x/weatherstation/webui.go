package weatherstation

import (
	"keyop/x/webui"
	"net/http"
)

// WebUIAssets serves static assets for the weatherstation dashboard panel.
func (svc *Service) WebUIAssets() http.FileSystem {
	return http.Dir("x/weatherstation/resources")
}

// WebUIPanels returns the dashboard panel definition for the weatherstation service.
func (svc *Service) WebUIPanels() []webui.PanelInfo {
	return []webui.PanelInfo{
		{
			ID:          "weatherstation",
			Title:       "Weather",
			Content:     `<div class="panel" id="panel-weatherstation"><div class="panel-body">Loading...</div></div>`,
			JSPath:      "/api/assets/weatherstation/weatherstation-panel.js",
			Event:       "weatherstation",
			ServiceType: svc.Cfg.Type,
		},
	}
}
