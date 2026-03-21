package weather

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"keyop/x/webui"
	"net/http"
	"os"
	"time"
)

// WebUIAssets returns the static assets for the weather service.
func (svc *Service) WebUIAssets() http.FileSystem {
	return http.Dir("x/weather/resources")
}

// WebUITab returns the tab configuration for the weather service.
func (svc *Service) WebUITab() webui.TabInfo {
	css, _ := os.ReadFile("x/weather/resources/weather.css") // #nosec G304
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
