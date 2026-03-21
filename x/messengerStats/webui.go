//nolint:revive
package messengerStats

import (
	"embed"
	"io/fs"
	"keyop/x/webui"
	"net/http"
)

//go:embed resources
var embeddedAssets embed.FS

// WebUIAssets returns the static assets for the messengerStats service.
func (svc *Service) WebUIAssets() http.FileSystem {
	sub, _ := fs.Sub(embeddedAssets, "resources")
	return http.FS(sub)
}

// WebUITab returns the tab configuration for the messengerStats service.
func (svc *Service) WebUITab() webui.TabInfo {
	return webui.TabInfo{
		ID:    "messages",
		Title: "Messages",
		Content: `<div id="messages-container">
<h3>Message Statistics</h3>
<div id="messages-summary" style="display:flex;gap:2rem;margin-bottom:1rem;">
  <span>Total: <strong id="messages-total">—</strong></span>
  <span>Failures: <strong id="messages-failures">—</strong></span>
</div>
<canvas id="messages-chart" style="width:100%;display:block;border-radius:6px;"></canvas>
</div>`,
		JSPath: "/api/assets/messengerStats/messages.js",
	}
}

// WebUIPanels returns panels provided by the messengerStats service for the dashboard.
func (svc *Service) WebUIPanels() []webui.PanelInfo {
	return []webui.PanelInfo{
		{
			ID:          "messages",
			Title:       "Messages",
			Content:     `<div class="panel" id="panel-messages"><div class="panel-body">Loading...</div></div>`,
			JSPath:      "/api/assets/messengerStats/messages-panel.js",
			Event:       "messenger_stats",
			ServiceType: svc.Cfg.Type,
		},
	}
}
