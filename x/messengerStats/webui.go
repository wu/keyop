package messengerStats

import (
	"keyop/x/webui"
	"net/http"
)

// WebUIAssets returns the static assets for the messengerStats service.
func (svc *Service) WebUIAssets() http.FileSystem {
	return http.Dir("x/messengerStats/resources")
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
