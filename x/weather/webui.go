package weather

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

// WebUIAssets returns the static assets for the weather service.
func (svc *Service) WebUIAssets() http.FileSystem {
	sub, _ := fs.Sub(embeddedAssets, "resources")
	return http.FS(sub)
}

// WebUIPanels returns panels provided by the weather service for the dashboard.
func (svc *Service) WebUIPanels() []webui.PanelInfo {
	css, _ := embeddedAssets.ReadFile("resources/weather-panel.css")
	content := `<link rel="stylesheet" href="/api/assets/weather/weather-icons/css/weather-icons.min.css">` +
		"\n" + `<div class="panel" id="panel-weather"><div class="panel-body"></div></div>`
	if len(css) > 0 {
		content += "\n<style>\n" + string(css) + "\n</style>"
	}
	return []webui.PanelInfo{
		{
			ID:          "weather",
			Title:       "Weather",
			Content:     content,
			JSPath:      "/api/assets/weather/weather-panel.js",
			Event:       "weather_forecast",
			ServiceType: svc.Cfg.Type,
		},
	}
}

// WebUITab returns the tab configuration for the weather service.
func (svc *Service) WebUITab() webui.TabInfo {
	css, _ := embeddedAssets.ReadFile("resources/weather.css")
	content := "<div id=\"weather-container\">\n<div id=\"weather-forecast\">Loading forecast...</div>\n</div>"
	if len(css) > 0 {
		content += "\n<style>\n" + string(css) + "\n</style>"
	}
	return webui.TabInfo{
		ID:      "weather",
		Title:   "Weather",
		Content: content,
		JSPath:  "/api/assets/weather/weather.js",
	}
}

// HandleWebUIAction handles web UI actions for the weather service.
func (svc *Service) HandleWebUIAction(action string, _ map[string]any) (any, error) {
	switch action {
	case "fetch-forecast":
		return svc.fetchForecast()
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

func (svc *Service) fetchForecast() (any, error) {
	if svc.db == nil || *svc.db == nil {
		return map[string]any{"periods": nil}, nil
	}
	db := *svc.db
	row := db.QueryRow(`SELECT timestamp, periods FROM weather_forecasts ORDER BY timestamp DESC LIMIT 1`)
	var ts time.Time
	var periodsJSON sql.NullString
	if err := row.Scan(&ts, &periodsJSON); err != nil {
		if err == sql.ErrNoRows {
			return map[string]any{"periods": nil}, nil
		}
		return nil, fmt.Errorf("weather: query failed: %w", err)
	}
	var periods []ForecastPeriod
	if periodsJSON.Valid {
		if err := json.Unmarshal([]byte(periodsJSON.String), &periods); err != nil {
			return nil, fmt.Errorf("weather: failed to parse periods: %w", err)
		}
	}
	return map[string]any{
		"timestamp": ts.Format(time.RFC3339),
		"periods":   periods,
	}, nil
}
