package weatherstation

import (
	"embed"
	"io/fs"
	"keyop/x/webui"
	"net/http"
)

//go:embed resources
var embeddedAssets embed.FS

// WebUIAssets serves static assets for the weatherstation dashboard panel.
func (svc *Service) WebUIAssets() http.FileSystem {
	sub, _ := fs.Sub(embeddedAssets, "resources")
	return http.FS(sub)
}

// WebUIPanels returns the dashboard panel definition for the weatherstation service.
func (svc *Service) WebUIPanels() []webui.PanelInfo {
	return []webui.PanelInfo{
		{
			ID:          "weatherstation",
			Title:       "🌤️",
			Content:     `<div class="panel" id="panel-weatherstation"><div class="panel-body">Loading...</div></div>`,
			JSPath:      "/api/assets/weatherstation/weatherstation-panel.js",
			Event:       "weatherstation",
			ServiceType: svc.Cfg.Type,
		},
	}
}
