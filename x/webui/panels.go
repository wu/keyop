package webui

import (
	"encoding/json"
	"net/http"

	"keyop/core"
)

// PanelInfo describes a small widget or panel that can be shown on the dashboard tab.
// Panels are provided by other services via the PanelProvider interface.
type PanelInfo struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	JSPath      string `json:"jsPath,omitempty"`
	ServiceType string `json:"serviceType,omitempty"`
	Event       string `json:"event,omitempty"` // optional event name to filter incoming messages
}

// PanelProvider is implemented by services that want to expose one or more dashboard panels.
type PanelProvider interface {
	WebUIPanels() []PanelInfo
}

// RegisterPanelProvider registers a PanelProvider and, if it implements AssetProvider,
// also registers its asset filesystem so panel modules can be loaded via /api/assets.
// After registering, broadcast a 'panels_updated' SSE message so clients refresh.
func (svc *Service) RegisterPanelProvider(serviceType string, provider PanelProvider) {
	svc.panelProvidersMu.Lock()
	svc.panelProviders[serviceType] = provider
	svc.panelProvidersMu.Unlock()

	if ap, ok := provider.(AssetProvider); ok {
		svc.assetProvidersMu.Lock()
		svc.assetProviders[serviceType] = ap
		svc.assetProvidersMu.Unlock()
	}

	// Notify connected clients that panels list changed.
	msg := core.Message{
		ServiceType: svc.Cfg.Type,
		Event:       "panels_updated",
		Summary:     "Panel providers updated",
	}
	if b, err := json.Marshal(msg); err == nil {
		svc.broadcast(b)
	}
}

func (svc *Service) handleGetPanels(w http.ResponseWriter, _ *http.Request) {
	svc.panelProvidersMu.RLock()
	defer svc.panelProvidersMu.RUnlock()

	panels := make([]PanelInfo, 0, len(svc.panelProviders))
	for _, p := range svc.panelProviders {
		panels = append(panels, p.WebUIPanels()...)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(panels)
}

// WebUIAssets returns webui's own static assets so the dashboard module can be loaded via /api/assets/webui/...
func (svc *Service) WebUIAssets() http.FileSystem {
	return http.Dir("x/webui/resources")
}

// WebUITab returns the dashboard tab owned by the webui service.
func (svc *Service) WebUITab() TabInfo {
	return TabInfo{
		ID:      "dashboard",
		Title:   "keyop",
		Content: `<div id="dashboard-container"><div id="dashboard-panels">Loading dashboard...</div></div>`,
		JSPath:  "/api/assets/webui/js/dashboard.js",
	}
}
