package weatherstation

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"keyop/x/webui"
	"net/http"
)

//go:embed resources
var embeddedAssets embed.FS

// WebUITab implements webui.TabProvider.
func (svc *Service) WebUITab() webui.TabInfo {
	// Load HTML from resources directory
	htmlContent, err := embeddedAssets.ReadFile("resources/weatherstation.html")
	if err != nil {
		htmlContent = []byte(`<div id="weatherstation-container" class="weatherstation-container">
    <div class="ws-grid">Loading...</div>
</div>`)
	}

	// Load CSS from resources directory
	cssContent, err := embeddedAssets.ReadFile("resources/weatherstation.css")
	if err != nil {
		cssContent = []byte{}
	}

	// Combine HTML with embedded CSS
	content := string(htmlContent) + "\n<style>\n" + string(cssContent) + "\n</style>"

	return webui.TabInfo{
		ID:      "weatherstation",
		Title:   "⛈️",
		Icon:    "🌡️",
		Content: content,
		JSPath:  "/api/assets/weatherstation/weatherstation.js",
	}
}

// WebUIAssets serves static assets for the weatherstation dashboard panel.
func (svc *Service) WebUIAssets() http.FileSystem {
	sub, _ := fs.Sub(embeddedAssets, "resources")
	return http.FS(sub)
}

// WebUIPanels returns the dashboard panel definition for the weatherstation service.
func (svc *Service) WebUIPanels() []webui.PanelInfo {
	return []webui.PanelInfo{
		{
			ID:          "weatherstation",
			Title:       "⛈️",
			Content:     `<div class="panel" id="panel-weatherstation"><div class="panel-body">Loading...</div></div>`,
			JSPath:      "/api/assets/weatherstation/weatherstation-panel.js",
			Event:       "weatherstation",
			ServiceType: svc.Cfg.Type,
		},
	}
}

// HandleWebUIAction handles actions from the WebUI.
func (svc *Service) HandleWebUIAction(action string, _ map[string]any) (any, error) {
	switch action {
	case "get-current":
		return svc.getCurrentWeather()
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// getCurrentWeather retrieves the most recent weather reading from the database.
func (svc *Service) getCurrentWeather() (any, error) {
	if svc.db == nil || *svc.db == nil {
		return nil, fmt.Errorf("database not available")
	}

	db := *svc.db

	row := db.QueryRow(`
		SELECT 
			barometer, barometer_rel, daily_rain, out_humidity, in_humidity,
			out_temp, in_temp, rain_rate, solar_radiation, uv,
			wh65_batt, wind_dir, wind_gust, wind_speed,
			recorded_at
		FROM weather_readings
		ORDER BY recorded_at DESC
		LIMIT 1
	`)

	var (
		barometer, barometerRel, dailyRain, outTemp, inTemp, rainRate, solarRadiation, windGust, windSpeed *float64
		outHumidity, inHumidity, uv, wh65Batt, windDir                                                     *int
		recordedAt                                                                                         *string
	)

	err := row.Scan(&barometer, &barometerRel, &dailyRain, &outHumidity, &inHumidity,
		&outTemp, &inTemp, &rainRate, &solarRadiation, &uv,
		&wh65Batt, &windDir, &windGust, &windSpeed, &recordedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			// Return empty data structure if no readings yet
			return map[string]any{
				"barometer":      nil,
				"barometerRel":   nil,
				"dailyRain":      nil,
				"outHumidity":    nil,
				"inHumidity":     nil,
				"outTemp":        nil,
				"inTemp":         nil,
				"rainRate":       nil,
				"solarRadiation": nil,
				"uv":             nil,
				"wh65Batt":       nil,
				"windDir":        nil,
				"windGust":       nil,
				"windSpeed":      nil,
			}, nil
		}
		return nil, err
	}

	return map[string]any{
		"barometer":      barometer,
		"barometerRel":   barometerRel,
		"dailyRain":      dailyRain,
		"outHumidity":    outHumidity,
		"inHumidity":     inHumidity,
		"outTemp":        outTemp,
		"inTemp":         inTemp,
		"rainRate":       rainRate,
		"solarRadiation": solarRadiation,
		"uv":             uv,
		"wh65Batt":       wh65Batt,
		"windDir":        windDir,
		"windGust":       windGust,
		"windSpeed":      windSpeed,
		"recordedAt":     recordedAt,
	}, nil
}
