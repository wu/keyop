package moon

import (
	"keyop/x/webui"
	"net/http"
)

// WebUIAssets returns the static assets for the moon service.
func (svc *Service) WebUIAssets() http.FileSystem {
	return http.Dir("x/moon/resources")
}

// WebUIPanels returns panels provided by the moon service for the dashboard.
func (svc *Service) WebUIPanels() []webui.PanelInfo {
	return []webui.PanelInfo{
		{
			ID:          "moon",
			Title:       "Moon",
			Content:     `<div class="panel" id="panel-moon"><div class="panel-body">Loading...</div></div>`,
			JSPath:      "/api/assets/moon/moon-panel.js",
			Event:       "moon_phase",
			ServiceType: svc.Cfg.Type,
		},
	}
}
