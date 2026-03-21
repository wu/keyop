package aurora

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"keyop/x/webui"
	"net/http"
	"time"
)

//go:embed resources
var embeddedAssets embed.FS

// WebUIAssets returns the static assets for the aurora service.
func (svc *Service) WebUIAssets() http.FileSystem {
	sub, _ := fs.Sub(embeddedAssets, "resources")
	return http.FS(sub)
}

// WebUITab returns the tab configuration for the aurora service.
func (svc *Service) WebUITab() webui.TabInfo {
	return webui.TabInfo{
		ID:    "aurora",
		Title: "Aurora",
		Content: `<div id="aurora-container">
<h3>Current Aurora</h3>
<div id="aurora-status">Loading aurora forecast...</div>
<h3>3-Day Forecast</h3>
<div id="aurora-forecast">Loading forecast...</div>
<h3>Aurora History</h3>
<div id="aurora-history"></div>
</div>`,
		JSPath:         "/api/assets/aurora/aurora.js",
		RenderMarkdown: false,
	}
}

// WebUIPanels returns panels provided by the aurora service for the dashboard.
func (svc *Service) WebUIPanels() []webui.PanelInfo {
	return []webui.PanelInfo{
		{
			ID:          "aurora",
			Title:       "Aurora",
			Content:     `<div class="panel" id="panel-aurora"><div class="panel-body">Loading...</div></div>`,
			JSPath:      "/api/assets/aurora/aurora-panel.js",
			Event:       "aurora_check",
			ServiceType: svc.Cfg.Type,
		},
	}
}

// HandleWebUIAction handles actions from the WebUI for aurora.
func (svc *Service) HandleWebUIAction(action string, _ map[string]any) (any, error) {
	switch action {
	case "get-current":
		// Return the most recent aurora event and latest forecast from sqlite if available.
		if svc.db == nil || *svc.db == nil {
			return map[string]any{"status": "ok", "current": nil, "forecast": nil}, nil
		}
		db := *svc.db
		row := db.QueryRow(`SELECT timestamp, likelihood, lat, lon, forecast_time, data FROM aurora_events ORDER BY timestamp DESC LIMIT 1`)
		var ts time.Time
		var likelihood float64
		var lat, lon float64
		var forecast sql.NullString
		var dataJSON sql.NullString
		if err := row.Scan(&ts, &likelihood, &lat, &lon, &forecast, &dataJSON); err != nil {
			if err == sql.ErrNoRows {
				// No events but maybe forecasts
				return map[string]any{"status": "ok", "current": nil, "forecast": nil}, nil
			}
			return nil, fmt.Errorf("aurora: failed to query current event: %w", err)
		}

		var data any
		if dataJSON.Valid {
			if err := json.Unmarshal([]byte(dataJSON.String), &data); err != nil {
				// If we can't unmarshal, return the raw JSON string
				data = dataJSON.String
			}
		}

		current := map[string]any{
			"timestamp":     ts.Format(time.RFC3339),
			"likelihood":    likelihood,
			"lat":           lat,
			"lon":           lon,
			"forecast_time": nil,
			"data":          data,
		}
		if forecast.Valid {
			current["forecast_time"] = forecast.String
		}

		// Also fetch latest forecast row if present
		frow := db.QueryRow(`SELECT fetched_at, source_url, data FROM aurora_forecasts ORDER BY fetched_at DESC LIMIT 1`)
		var fetchedAt time.Time
		var sourceURL sql.NullString
		var fcJSON sql.NullString
		forecastResp := map[string]any{"fetched_at": nil, "source_url": nil, "data": nil}
		if err := frow.Scan(&fetchedAt, &sourceURL, &fcJSON); err == nil {
			var fcData any
			if fcJSON.Valid {
				if err := json.Unmarshal([]byte(fcJSON.String), &fcData); err != nil {
					fcData = fcJSON.String
				}
			}
			forecastResp["fetched_at"] = fetchedAt.Format(time.RFC3339)
			if sourceURL.Valid {
				forecastResp["source_url"] = sourceURL.String
			}
			forecastResp["data"] = fcData
		} else if err != sql.ErrNoRows {
			return nil, fmt.Errorf("aurora: failed to query forecast: %w", err)
		}

		return map[string]any{"status": "ok", "current": current, "forecast": forecastResp}, nil
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}
