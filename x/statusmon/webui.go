package statusmon

import (
	"keyop/x/webui"
	"net/http"
	"time"
)

// WebUIAssets returns the static assets for the statusmon service.
func (svc *Service) WebUIAssets() http.FileSystem {
	return http.Dir("x/statusmon/resources")
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

// HandleWebUIAction handles actions from the WebUI.
func (svc *Service) HandleWebUIAction(action string, _ map[string]any) (any, error) {
	switch action {
	case "fetch-status":
		return svc.fetchStatus()
	default:
		return nil, nil
	}
}

// fetchStatus returns the current status of all monitored items.
func (svc *Service) fetchStatus() (any, error) {
	type StatusItem struct {
		Name     string `json:"name"`
		Hostname string `json:"hostname"`
		Status   string `json:"status"`
		Details  string `json:"details"`
		Level    string `json:"level"`
		LastSeen string `json:"lastSeen"`
	}

	var statuses []StatusItem

	// Query sqlite for the latest status of each unique name
	if svc.db != nil && *svc.db != nil {
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
		defer rows.Close()

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

			statuses = append(statuses, StatusItem{
				Name:     name,
				Hostname: hostname,
				Status:   status,
				Details:  details,
				Level:    level,
				LastSeen: timestamp.Format(time.RFC3339),
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

		statuses = append(statuses, StatusItem{
			Name:     name,
			Hostname: "",
			Status:   state.Status,
			Details:  state.Details,
			Level:    level,
			LastSeen: lastSeen,
		})
	}

	return map[string]any{"statuses": statuses}, nil
}
