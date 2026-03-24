package thermostat

import (
	"database/sql"
	"encoding/json"
	"keyop/core"
)

// SQLiteSchema returns the DDL for the thermostat_events table.
func (svc *Service) SQLiteSchema() string {
	return `CREATE TABLE IF NOT EXISTS thermostat_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME,
		service_name TEXT,
		service_type TEXT,
		hostname TEXT,
		event TEXT,
		temp REAL,
		min_temp REAL,
		max_temp REAL,
		mode TEXT,
		heater_state TEXT,
		cooler_state TEXT,
		data TEXT
	);`
}

// SQLiteInsert prepares an INSERT for incoming thermostat event messages.
func (svc *Service) SQLiteInsert(msg core.Message) (string, []any) {
	var dataJSON string
	if msg.Data != nil {
		if b, err := json.Marshal(msg.Data); err == nil {
			dataJSON = string(b)
		} else {
			svc.Deps.MustGetLogger().Warn("thermostat: failed to marshal data for sqlite insert", "error", err)
		}
	}

	var temp, minTemp, maxTemp float64
	var mode, heaterState, coolerState string
	if ev, ok := core.AsType[*Event](msg.Data); ok && ev != nil {
		temp = ev.Temp
		minTemp = ev.MinTemp
		maxTemp = ev.MaxTemp
		mode = ev.Mode
		heaterState = ev.HeaterTargetState
		coolerState = ev.CoolerTargetState
	} else if ev, ok := core.AsType[Event](msg.Data); ok {
		temp = ev.Temp
		minTemp = ev.MinTemp
		maxTemp = ev.MaxTemp
		mode = ev.Mode
		heaterState = ev.HeaterTargetState
		coolerState = ev.CoolerTargetState
	}

	return `INSERT INTO thermostat_events (timestamp, service_name, service_type, hostname, event, temp, min_temp, max_temp, mode, heater_state, cooler_state, data) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		[]any{msg.Timestamp, msg.ServiceName, msg.ServiceType, msg.Hostname, msg.Event, temp, minTemp, maxTemp, mode, heaterState, coolerState, dataJSON}
}

// SetSQLiteDB allows the runtime to provide a pointer to the DB instance.
func (svc *Service) SetSQLiteDB(db **sql.DB) {
	svc.db = db
}

// PayloadTypes returns the payload type names that this provider handles.
func (svc *Service) PayloadTypes() []string {
	return []string{"thermostat.event.v1", "thermostat"}
}
