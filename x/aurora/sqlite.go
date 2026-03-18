package aurora

import (
	"database/sql"
	"encoding/json"
	"keyop/core"
)

// SQLiteSchema returns the DDL for storing aurora events and forecasts.
func (svc *Service) SQLiteSchema() string {
	return `CREATE TABLE IF NOT EXISTS aurora_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME,
		likelihood REAL,
		lat REAL,
		lon REAL,
		forecast_time TEXT,
		data TEXT
	);

	CREATE TABLE IF NOT EXISTS aurora_forecasts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		fetched_at DATETIME,
		source_url TEXT,
		data TEXT
	);
	`
}

// SQLiteInsert returns an INSERT statement for aurora messages. Persist only when a typed payload is present.
func (svc *Service) SQLiteInsert(msg core.Message) (string, []any) {
	switch msg.Event {
	case "aurora_check":
		// Require typed Event payload only.
		if evPtr, ok := core.AsType[*Event](msg.Data); ok && evPtr != nil {
			b, _ := json.Marshal(evPtr)
			return `INSERT INTO aurora_events (timestamp, likelihood, lat, lon, forecast_time, data) VALUES (?, ?, ?, ?, ?, ?)`,
				[]any{msg.Timestamp, evPtr.Likelihood, evPtr.Lat, evPtr.Lon, evPtr.ForecastTime, string(b)}
		}
		if evVal, ok := core.AsType[Event](msg.Data); ok {
			b, _ := json.Marshal(evVal)
			return `INSERT INTO aurora_events (timestamp, likelihood, lat, lon, forecast_time, data) VALUES (?, ?, ?, ?, ?, ?)`,
				[]any{msg.Timestamp, evVal.Likelihood, evVal.Lat, evVal.Lon, evVal.ForecastTime, string(b)}
		}
		// Not a typed Event — do not persist.
		return "", nil
	case "aurora_forecast":
		// Require typed Forecast payload only.
		if fcPtr, ok := core.AsType[*Forecast](msg.Data); ok && fcPtr != nil {
			// Marshal the structured ParsedForecast to JSON for persistence (if present)
			if fcPtr.Data != nil {
				b, _ := json.Marshal(fcPtr.Data)
				return `INSERT INTO aurora_forecasts (fetched_at, source_url, data) VALUES (?, ?, ?)`,
					[]any{msg.Timestamp, fcPtr.SourceURL, string(b)}
			}
			// If Data is nil, insert empty string
			return `INSERT INTO aurora_forecasts (fetched_at, source_url, data) VALUES (?, ?, ?)`,
				[]any{msg.Timestamp, fcPtr.SourceURL, ""}
		}
		if fcVal, ok := core.AsType[Forecast](msg.Data); ok {
			if fcVal.Data != nil {
				b, _ := json.Marshal(fcVal.Data)
				return `INSERT INTO aurora_forecasts (fetched_at, source_url, data) VALUES (?, ?, ?)`,
					[]any{msg.Timestamp, fcVal.SourceURL, string(b)}
			}
			return `INSERT INTO aurora_forecasts (fetched_at, source_url, data) VALUES (?, ?, ?)`,
				[]any{msg.Timestamp, fcVal.SourceURL, ""}
		}
		return "", nil
	default:
		return "", nil
	}
}

// SetSQLiteDB allows the runtime to provide a pointer to the DB instance.
func (svc *Service) SetSQLiteDB(db **sql.DB) {
	svc.db = db
}

// PayloadTypes returns the payload type(s) this provider handles.
func (svc *Service) PayloadTypes() []string {
	return []string{"service.aurora.v1", "aurora", "service.aurora.forecast.v1", "aurora_forecast"}
}
