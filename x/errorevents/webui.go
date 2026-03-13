package errorevents

import (
	"fmt"
	"keyop/x/webui"
	"net/http"
)

// WebUIAssets returns the static assets for the errorevents service.
func (svc *Service) WebUIAssets() http.FileSystem {
	return http.Dir("x/errorevents/resources")
}

// WebUITab returns the tab configuration for the errorevents service.
func (svc *Service) WebUITab() webui.TabInfo {
	return webui.TabInfo{
		ID:    "errors",
		Title: "Errors",
		Content: `<div id="errors-container">
<h3>Recent Errors</h3>
<div id="errors-list">Loading errors...</div>
</div>`,
		JSPath: "/api/assets/errorevents/errors.js",
	}
}

// HandleWebUIAction handles actions from the WebUI.
func (svc *Service) HandleWebUIAction(action string, params map[string]any) (any, error) {
	switch action {
	case "fetch-errors":
		return svc.fetchErrors()
	case "mark-seen":
		if errorID, ok := params["errorID"].(float64); ok {
			return svc.markErrorSeen(int64(errorID))
		}
		return nil, fmt.Errorf("invalid errorID")
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// fetchErrors queries the SQLite database for unseen errors.
func (svc *Service) fetchErrors() (any, error) {
	if svc.db == nil || *svc.db == nil {
		return nil, fmt.Errorf("errors database not available")
	}

	rows, err := (*svc.db).Query(`
		SELECT id, timestamp, service_name, service_type, hostname, event, 
		       severity, summary, text, data, seen
		FROM errors
		WHERE seen = 0
		ORDER BY timestamp DESC
	`)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			svc.Deps.MustGetLogger().Warn("errors: failed to close rows", "error", err)
		}
	}()

	type ErrorRow struct {
		ID          int64  `json:"id"`
		Timestamp   string `json:"timestamp"`
		ServiceName string `json:"serviceName"`
		ServiceType string `json:"serviceType"`
		Hostname    string `json:"hostname"`
		Event       string `json:"event"`
		Severity    string `json:"severity"`
		Summary     string `json:"summary"`
		Text        string `json:"text"`
		Data        string `json:"data"`
		Seen        int    `json:"seen"`
	}

	var errors []ErrorRow
	for rows.Next() {
		var err ErrorRow
		if err := rows.Scan(
			&err.ID, &err.Timestamp, &err.ServiceName, &err.ServiceType,
			&err.Hostname, &err.Event, &err.Severity, &err.Summary,
			&err.Text, &err.Data, &err.Seen,
		); err != nil {
			return nil, err
		}
		errors = append(errors, err)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return map[string]any{"errors": errors}, nil
}

// markErrorSeen updates the seen flag for an error.
func (svc *Service) markErrorSeen(errorID int64) (any, error) {
	if svc.db == nil || *svc.db == nil {
		return nil, fmt.Errorf("errors database not available")
	}

	_, err := (*svc.db).Exec(
		"UPDATE errors SET seen = 1 WHERE id = ?",
		errorID,
	)
	if err != nil {
		return nil, err
	}

	return map[string]string{"status": "ok"}, nil
}
