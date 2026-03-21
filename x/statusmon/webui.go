package statusmon

import (
	"embed"
	"io/fs"
	"keyop/x/webui"
	"net/http"
	"time"
)

//go:embed resources
var embeddedAssets embed.FS

// WebUIAssets returns the static assets for the statusmon service.
func (svc *Service) WebUIAssets() http.FileSystem {
	sub, _ := fs.Sub(embeddedAssets, "resources")
	return http.FS(sub)
}

// WebUITab returns the tab configuration for the statusmon service.
func (svc *Service) WebUITab() webui.TabInfo {
	return webui.TabInfo{
		ID:    "statusmon",
		Title: "Status",
		Content: `<div id="statusmon-container">
<div class="statusmon-layout">
  <div class="statusmon-content">
    <div id="statusmon-list">Loading status...</div>
  </div>
</div>
</div>`,
		JSPath: "/api/assets/statusmon/statusmon.js",
	}
}

// WebUIPanels returns panels provided by the statusmon service for the dashboard.
func (svc *Service) WebUIPanels() []webui.PanelInfo {
	return []webui.PanelInfo{
		{
			ID:          "statusmon",
			Title:       "Services",
			Content:     `<div class="panel" id="panel-statusmon"><div class="panel-body">Loading...</div></div>`,
			JSPath:      "/api/assets/statusmon/statusmon-panel.js",
			Event:       "statusmon",
			ServiceType: svc.Cfg.Type,
		},
	}
}

// HandleWebUIAction handles actions from the WebUI.
func (svc *Service) HandleWebUIAction(action string, params map[string]any) (any, error) {
	switch action {
	case "fetch-status":
		return svc.fetchStatus()
	case "acknowledge-status":
		if params != nil {
			if statusName, ok := params["statusName"].(string); ok {
				return svc.acknowledgeStatus(statusName)
			}
		}
		return map[string]any{"error": "missing statusName parameter"}, nil
	case "unacknowledge-status":
		if params != nil {
			if statusName, ok := params["statusName"].(string); ok {
				return svc.unacknowledgeStatus(statusName)
			}
		}
		return map[string]any{"error": "missing statusName parameter"}, nil
	default:
		return nil, nil
	}
}

// fetchStatus returns the current status of all monitored items.
func (svc *Service) fetchStatus() (any, error) {
	type StatusItem struct {
		Name         string `json:"name"`
		Hostname     string `json:"hostname"`
		Status       string `json:"status"`
		Details      string `json:"details"`
		Level        string `json:"level"`
		LastSeen     string `json:"lastSeen"`
		Acknowledged bool   `json:"acknowledged"`
	}

	var statuses []StatusItem

	// Query sqlite for the latest status of each unique name
	if svc.db != nil && *svc.db != nil {
		// Lock while we read from svc.states to get acknowledgement status
		svc.statesMutex.RLock()
		defer svc.statesMutex.RUnlock()

		db := *svc.db
		rows, err := db.Query(`
			SELECT name, status_hostname, status, level, details, timestamp
			FROM status
			WHERE (name, timestamp) IN (
				SELECT name, MAX(timestamp)
				FROM status
				GROUP BY name
			)
			ORDER BY name
		`)
		if err != nil {
			// If query fails, return empty list (sqlite table may not exist yet)
			return map[string]any{"statuses": statuses}, nil
		}
		defer func() {
			if err := rows.Close(); err != nil {
				svc.Deps.MustGetLogger().Warn("statusmon: failed to close rows", "error", err)
			}
		}()

		for rows.Next() {
			var name, hostname, status, level, details string
			var timestamp time.Time
			if err := rows.Scan(&name, &hostname, &status, &level, &details, &timestamp); err != nil {
				continue
			}

			// Derive level from status if empty
			if level == "" {
				switch status {
				case "warning":
					level = "warning"
				case "critical", "error":
					level = "critical"
				default:
					level = "ok"
				}
			}

			// Look up acknowledged status from in-memory state
			acknowledged := false
			if state, exists := svc.states[name]; exists {
				acknowledged = state.Acknowledged
			}

			// Clear acknowledged flag if status is now OK (no need to acknowledge OK status)
			if level == "ok" {
				acknowledged = false
			}

			statuses = append(statuses, StatusItem{
				Name:         name,
				Hostname:     hostname,
				Status:       status,
				Details:      details,
				Level:        level,
				LastSeen:     timestamp.Format(time.RFC3339),
				Acknowledged: acknowledged,
			})
		}
		return map[string]any{"statuses": statuses}, nil
	}

	// Fall back to in-memory state only if no sqlite DB configured
	svc.statesMutex.RLock()
	defer svc.statesMutex.RUnlock()

	for name, state := range svc.states {
		lastSeen := ""
		if !state.LastSeen.IsZero() {
			lastSeen = state.LastSeen.Format(time.RFC3339)
		}

		level := state.Level
		if level == "" {
			switch state.Status {
			case "warning":
				level = "warning"
			case "critical", "error":
				level = "critical"
			default:
				level = "ok"
			}
		}

		// Clear acknowledged flag if status is now OK (no need to acknowledge OK status)
		acknowledged := state.Acknowledged
		if level == "ok" {
			acknowledged = false
		}

		statuses = append(statuses, StatusItem{
			Name:         name,
			Hostname:     "",
			Status:       state.Status,
			Details:      state.Details,
			Level:        level,
			LastSeen:     lastSeen,
			Acknowledged: acknowledged,
		})
	}

	return map[string]any{"statuses": statuses}, nil
}

// acknowledgeStatus marks a status problem as acknowledged.
func (svc *Service) acknowledgeStatus(statusName string) (any, error) {
	svc.statesMutex.Lock()
	defer svc.statesMutex.Unlock()

	state, exists := svc.states[statusName]
	if !exists {
		state = serviceState{}
	}

	state.Acknowledged = true
	svc.states[statusName] = state
	svc.saveState()
	return map[string]any{"acknowledged": true}, nil
}

// unacknowledgeStatus marks a status problem as not acknowledged.
func (svc *Service) unacknowledgeStatus(statusName string) (any, error) {
	svc.statesMutex.Lock()
	defer svc.statesMutex.Unlock()

	state, exists := svc.states[statusName]
	if !exists {
		state = serviceState{}
	}

	state.Acknowledged = false
	svc.states[statusName] = state
	svc.saveState()
	return map[string]any{"acknowledged": false}, nil
}
