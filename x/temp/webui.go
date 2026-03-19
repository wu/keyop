package temp

import (
	"fmt"
	"keyop/x/webui"
	"net/http"
	"time"
)

// WebUIAssets returns the static assets for the temp service.
func (svc *Service) WebUIAssets() http.FileSystem {
	return http.Dir("x/temp/resources")
}

// WebUITab returns the tab configuration for the temp service.
func (svc *Service) WebUITab() webui.TabInfo {
	return webui.TabInfo{
		ID:    "temps",
		Title: "Temps",
		Content: `<div id="temps-container">
<canvas id="temps-chart"></canvas>
</div>`,
		JSPath: "/api/assets/temp/temps.js",
	}
}

// WebUIPanels returns panels provided by the temp service for the dashboard.
func (svc *Service) WebUIPanels() []webui.PanelInfo {
	return []webui.PanelInfo{
		{
			ID:          "temps",
			Title:       "Temps",
			Content:     `<div class="panel" id="panel-temps"><div class="panel-body">Loading...</div></div>`,
			JSPath:      "/api/assets/temp/temps-panel.js",
			Event:       "temps",
			ServiceType: svc.Cfg.Type,
		},
	}
}

// HandleWebUIAction handles actions from the WebUI.
func (svc *Service) HandleWebUIAction(action string, _ map[string]any) (any, error) {
	switch action {
	case "fetch-temps":
		return svc.fetchTemps()
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// fetchTemps queries the SQLite database for temperature readings from the last 4 hours.
func (svc *Service) fetchTemps() (any, error) {
	if svc.db == nil || *svc.db == nil {
		return nil, fmt.Errorf("temps database not available")
	}

	// Query temps from the last 4 hours
	fourHoursAgo := time.Now().Add(-4 * time.Hour)

	rows, err := (*svc.db).Query(`
		SELECT id, timestamp, service_name, service_type, temp_c, temp_f
		FROM temps
		WHERE timestamp > ?
		ORDER BY timestamp ASC
	`, fourHoursAgo)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			svc.Deps.MustGetLogger().Warn("temps: failed to close rows", "error", err)
		}
	}()

	type TempReading struct {
		ID          int64   `json:"id"`
		Timestamp   string  `json:"timestamp"`
		ServiceName string  `json:"serviceName"`
		ServiceType string  `json:"serviceType"`
		TempC       float64 `json:"tempC"`
		TempF       float64 `json:"tempF"`
	}

	var readings []TempReading
	for rows.Next() {
		var reading TempReading
		if err := rows.Scan(
			&reading.ID, &reading.Timestamp, &reading.ServiceName, &reading.ServiceType,
			&reading.TempC, &reading.TempF,
		); err != nil {
			return nil, err
		}
		readings = append(readings, reading)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return map[string]any{"readings": readings}, nil
}
