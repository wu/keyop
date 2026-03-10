package idle

import (
	"database/sql"
	"keyop/core"
)

// SQLiteSchema returns the SQL DDL for the table needed by the idle service.
func (svc *Service) SQLiteSchema() string {
	return `CREATE TABLE IF NOT EXISTS idle_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME,
		hostname TEXT,
		status TEXT,
		idle_seconds REAL,
		active_seconds REAL
	);`
}

// SQLiteInsert returns the SQL INSERT statement and the arguments to be used for a given message.
func (svc *Service) SQLiteInsert(msg core.Message) (string, []any) {
	if msg.Event != "idle_status" {
		return "", nil
	}

	var idleSecs float64
	var activeSecs float64

	if event, ok := core.AsType[*Event](msg.Data); ok {
		idleSecs = event.IdleDurationSeconds
		activeSecs = event.ActiveDurationSeconds
	} else if data, ok := msg.Data.(map[string]any); ok {
		// Try both camelCase (JSON) and snake_case (legacy/internal)
		if v, ok := data["idleDurationSeconds"].(float64); ok {
			idleSecs = v
		} else if v, ok := data["idle_duration_seconds"].(float64); ok {
			idleSecs = v
		}

		if v, ok := data["activeDurationSeconds"].(float64); ok {
			activeSecs = v
		} else if v, ok := data["active_duration_seconds"].(float64); ok {
			activeSecs = v
		}
	} else {
		return "", nil
	}

	return `INSERT INTO idle_events (timestamp, hostname, status, idle_seconds, active_seconds) VALUES (?, ?, ?, ?, ?)`,
		[]any{msg.Timestamp, msg.Hostname, msg.Status, idleSecs, activeSecs}
}

// SetSQLiteDB sets the database pointer to be used by the service.
func (svc *Service) SetSQLiteDB(db **sql.DB) {
	svc.db = db
}
