package owntracks

import (
	"database/sql"
	"encoding/json"
	"keyop/core"
	"keyop/x/sqlite"
	"keyop/x/webui"
)

// Compile-time interface assertions.
var (
	_ core.PayloadProvider  = (*Service)(nil)
	_ sqlite.SchemaProvider = (*Service)(nil)
	_ sqlite.Consumer       = (*Service)(nil)
	_ webui.TabProvider     = (*Service)(nil)
	_ webui.ActionProvider  = (*Service)(nil)
)

// SQLiteSchema returns the DDL for owntracks tables.
func (svc *Service) SQLiteSchema() string {
	return `CREATE TABLE IF NOT EXISTS gps_locations (
id INTEGER PRIMARY KEY AUTOINCREMENT,
timestamp DATETIME NOT NULL,
device TEXT,
lat REAL,
lon REAL,
alt REAL,
acc REAL,
batt REAL,
raw TEXT
);
CREATE TABLE IF NOT EXISTS gps_region_events (
id INTEGER PRIMARY KEY AUTOINCREMENT,
timestamp DATETIME NOT NULL,
device TEXT,
event_type TEXT,
region TEXT,
lat REAL,
lon REAL
);`
}

// SQLiteInsert returns the SQL and args to persist a message.
func (svc *Service) SQLiteInsert(msg core.Message) (string, []any) {
	switch msg.Event {
	case "gps":
		ev, ok := core.AsType[*LocationEvent](msg.Data)
		if !ok || ev == nil {
			return "", nil
		}
		b, _ := json.Marshal(ev)
		return `INSERT INTO gps_locations (timestamp, device, lat, lon, alt, acc, batt, raw) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			[]any{msg.Timestamp, ev.Device, ev.Lat, ev.Lon, ev.Alt, ev.Acc, ev.Batt, string(b)}
	case "region_enter":
		ev, ok := core.AsType[*LocationEnterEvent](msg.Data)
		if !ok || ev == nil {
			return "", nil
		}
		return `INSERT INTO gps_region_events (timestamp, device, event_type, region, lat, lon) VALUES (?, ?, 'enter', ?, ?, ?)`,
			[]any{msg.Timestamp, ev.Device, ev.Region, ev.Lat, ev.Lon}
	case "region_exit":
		ev, ok := core.AsType[*LocationExitEvent](msg.Data)
		if !ok || ev == nil {
			return "", nil
		}
		return `INSERT INTO gps_region_events (timestamp, device, event_type, region, lat, lon) VALUES (?, ?, 'exit', ?, ?, ?)`,
			[]any{msg.Timestamp, ev.Device, ev.Region, ev.Lat, ev.Lon}
	}
	return "", nil
}

// PayloadTypes returns the payload type names this provider handles for SQLite persistence.
func (svc *Service) PayloadTypes() []string {
	return []string{
		"service.owntracks.location.v1",
		"service.owntracks.enter.v1",
		"service.owntracks.exit.v1",
	}
}

// SetSQLiteDB injects the shared SQLite database handle.
func (svc *Service) SetSQLiteDB(db **sql.DB) {
	svc.db = db
}
