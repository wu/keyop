package owntracks

import (
	"database/sql"
	"fmt"
	"keyop/x/webui"
	"net/http"
	"time"
)

// WebUIAssets returns the static assets for the owntracks/gps service.
func (svc *Service) WebUIAssets() http.FileSystem {
	return http.Dir("x/owntracks/resources")
}

// WebUITab returns the tab definition for the GPS tab.
func (svc *Service) WebUITab() webui.TabInfo {
	return webui.TabInfo{
		ID:    "gps",
		Title: "GPS",
		Content: `<div id="gps-container">
			<div id="gps-status" style="padding: 16px; opacity: 0.6;">Loading GPS data...</div>
		</div>`,
		JSPath: "/api/assets/owntracks/gps.js",
	}
}

// HandleWebUIAction handles actions from the GPS tab.
func (svc *Service) HandleWebUIAction(action string, _ map[string]any) (any, error) {
	switch action {
	case "get-current":
		return svc.getCurrentGPS()
	case "get-map":
		return svc.getMapForCurrentLocation()
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

func (svc *Service) getCurrentGPS() (map[string]any, error) {
	if svc.db == nil || *svc.db == nil {
		return map[string]any{"status": "ok", "location": nil, "events": []any{}}, nil
	}
	db := *svc.db

	// Latest location
	var location map[string]any
	row := db.QueryRow(`SELECT timestamp, device, lat, lon, alt, acc, batt FROM gps_locations ORDER BY timestamp DESC LIMIT 1`)
	var ts time.Time
	var device sql.NullString
	var lat, lon, alt, acc, batt float64
	if err := row.Scan(&ts, &device, &lat, &lon, &alt, &acc, &batt); err == nil {
		location = map[string]any{
			"timestamp": ts.Format(time.RFC3339),
			"device":    device.String,
			"lat":       lat,
			"lon":       lon,
			"alt":       alt,
			"acc":       acc,
			"batt":      batt,
		}
	} else if err != sql.ErrNoRows {
		return nil, fmt.Errorf("gps: failed to query location: %w", err)
	}

	// Recent region events
	rows, err := db.Query(`SELECT timestamp, device, event_type, region, lat, lon FROM gps_region_events ORDER BY timestamp DESC LIMIT 50`)
	if err != nil {
		return nil, fmt.Errorf("gps: failed to query region events: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	events := []map[string]any{}
	for rows.Next() {
		var evTs time.Time
		var evDevice sql.NullString
		var evType, region string
		var evLat, evLon float64
		if err := rows.Scan(&evTs, &evDevice, &evType, &region, &evLat, &evLon); err != nil {
			continue
		}
		events = append(events, map[string]any{
			"timestamp":  evTs.Format(time.RFC3339),
			"device":     evDevice.String,
			"event_type": evType,
			"region":     region,
			"lat":        evLat,
			"lon":        evLon,
		})
	}

	return map[string]any{
		"status":   "ok",
		"location": location,
		"events":   events,
	}, nil
}
