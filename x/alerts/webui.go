package alerts

import (
	"fmt"
	"keyop/x/webui"
	"net/http"
)

// WebUIAssets returns the static assets for the alerts service.
func (svc *Service) WebUIAssets() http.FileSystem {
	return http.Dir("x/alerts/resources")
}

// WebUITab returns the tab configuration for the alerts service.
func (svc *Service) WebUITab() webui.TabInfo {
	return webui.TabInfo{
		ID:    "alerts",
		Title: "Alerts",
		Content: `<div id="alerts-container">
<div class="alerts-layout">
  <div class="filter-sidebar">
    <div class="filter-title">Services</div>
    <div class="service-list">
      <div class="service-item active" data-service="all">all</div>
    </div>
  </div>
  <div class="alerts-content">
    <div class="alerts-header">
      <button id="mark-all-seen-btn" class="mark-all-seen-btn">Mark All Seen</button>
    </div>
    <div id="alerts-list">Loading alerts...</div>
  </div>
</div>
</div>`,
		JSPath: "/api/assets/alerts/alerts.js",
	}
}

// HandleWebUIAction handles actions from the WebUI.
func (svc *Service) HandleWebUIAction(action string, params map[string]any) (any, error) {
	switch action {
	case "fetch-alerts":
		return svc.fetchAlerts()
	case "mark-seen":
		if alertID, ok := params["alertID"].(float64); ok {
			return svc.markAlertSeen(int64(alertID))
		}
		return nil, fmt.Errorf("invalid alertID")
	case "mark-all-seen":
		if serviceFilter, ok := params["serviceFilter"].(string); ok {
			return svc.markAllAlertsSeen(serviceFilter)
		}
		return nil, fmt.Errorf("invalid serviceFilter")
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// fetchAlerts queries the SQLite database for unseen alerts.
func (svc *Service) fetchAlerts() (any, error) {
	if svc.db == nil || *svc.db == nil {
		return nil, fmt.Errorf("alerts database not available")
	}

	rows, err := (*svc.db).Query(`
		SELECT id, timestamp, service_name, service_type, hostname, event, 
		       severity, summary, text, data, seen
		FROM alerts
		WHERE seen = 0
		ORDER BY timestamp DESC
	`)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			svc.Deps.MustGetLogger().Warn("alerts: failed to close rows", "error", err)
		}
	}()

	type AlertRow struct {
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

	var alerts []AlertRow
	serviceCounts := make(map[string]int)

	for rows.Next() {
		var alert AlertRow
		if err := rows.Scan(
			&alert.ID, &alert.Timestamp, &alert.ServiceName, &alert.ServiceType,
			&alert.Hostname, &alert.Event, &alert.Severity, &alert.Summary,
			&alert.Text, &alert.Data, &alert.Seen,
		); err != nil {
			return nil, err
		}
		alerts = append(alerts, alert)
		serviceCounts[alert.ServiceName]++
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return map[string]any{"alerts": alerts, "serviceCounts": serviceCounts}, nil
}

// markAlertSeen updates the seen flag for an alert.
func (svc *Service) markAlertSeen(alertID int64) (any, error) {
	if svc.db == nil || *svc.db == nil {
		return nil, fmt.Errorf("alerts database not available")
	}

	_, err := (*svc.db).Exec(
		"UPDATE alerts SET seen = 1 WHERE id = ?",
		alertID,
	)
	if err != nil {
		return nil, err
	}

	return map[string]string{"status": "ok"}, nil
}

// markAllAlertsSeen marks all unseen alerts as seen, optionally filtered by service.
func (svc *Service) markAllAlertsSeen(serviceFilter string) (any, error) {
	if svc.db == nil || *svc.db == nil {
		return nil, fmt.Errorf("alerts database not available")
	}

	var err error
	if serviceFilter == "all" || serviceFilter == "" {
		// Mark all unseen alerts
		_, err = (*svc.db).Exec("UPDATE alerts SET seen = 1 WHERE seen = 0")
	} else {
		// Mark unseen alerts for the specific service
		_, err = (*svc.db).Exec(
			"UPDATE alerts SET seen = 1 WHERE seen = 0 AND service_name = ?",
			serviceFilter,
		)
	}

	if err != nil {
		return nil, err
	}

	return map[string]string{"status": "ok"}, nil
}
