package weather

import (
	"database/sql"
	"encoding/json"
	"keyop/core"
)

// Compile-time interface assertions.
var (
	_ core.PayloadProvider     = (*Service)(nil)
	_ core.PayloadTypeProvider = (*Service)(nil)
)

// SQLiteSchema returns the DDL for the weather_forecasts table.
func (svc *Service) SQLiteSchema() string {
	return `CREATE TABLE IF NOT EXISTS weather_forecasts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME,
		periods TEXT
	);`
}

// SQLiteInsert stores incoming weather_forecast events.
func (svc *Service) SQLiteInsert(msg core.Message) (string, []any) {
	if msg.Event != "weather_forecast" {
		return "", nil
	}
	var periodsJSON string
	if ev, ok := core.AsType[ForecastEvent](msg.Data); ok {
		if b, err := json.Marshal(ev.Periods); err == nil {
			periodsJSON = string(b)
		}
	} else if evPtr, ok := core.AsType[*ForecastEvent](msg.Data); ok && evPtr != nil {
		if b, err := json.Marshal(evPtr.Periods); err == nil {
			periodsJSON = string(b)
		}
	}
	if periodsJSON == "" {
		return "", nil
	}
	return `INSERT INTO weather_forecasts (timestamp, periods) VALUES (?, ?)`,
		[]any{msg.Timestamp, periodsJSON}
}

// SetSQLiteDB allows the runtime to provide a pointer to the DB instance.
func (svc *Service) SetSQLiteDB(db **sql.DB) {
	svc.db = db
}

// PayloadTypes returns the payload type names that this provider handles.
func (svc *Service) PayloadTypes() []string {
	return []string{"service.weather.v1", "weather_forecast"}
}
