package statusmon

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"keyop/core"
	"strings"
)

// SQLiteSchema returns the DDL for the status table.
func (svc *Service) SQLiteSchema() string {
	return `CREATE TABLE IF NOT EXISTS status (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME,
		service_name TEXT,
		service_type TEXT,
		hostname TEXT,
		event TEXT,
		name TEXT,
		status_hostname TEXT,
		status TEXT,
		level TEXT,
		details TEXT,
		data TEXT
	);`
}

// Migrate applies any necessary migrations to the schema. This is called after the
// initial table creation to add new columns to existing tables.
func (svc *Service) Migrate() error {
	if svc.db == nil || *svc.db == nil {
		return nil
	}

	logger := svc.Deps.MustGetLogger()
	db := *svc.db

	// Add status_hostname column if it doesn't exist
	_, err := db.Exec(`ALTER TABLE status ADD COLUMN status_hostname TEXT DEFAULT ''`)
	if err != nil {
		errMsg := err.Error()
		// Ignore "column already exists" errors
		if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
			logger.Error("statusmon: failed to migrate status table", "error", err)
			return err
		}
		logger.Debug("statusmon: status_hostname column already exists")
	}

	return nil
}

// SQLiteInsert prepares an INSERT for incoming messages.
func (svc *Service) SQLiteInsert(msg core.Message) (string, []any) {
	logger := svc.Deps.MustGetLogger()

	var dataJSON string
	if msg.Data != nil {
		if b, err := json.Marshal(msg.Data); err == nil {
			dataJSON = string(b)
		} else {
			logger.Warn("statusmon: failed to marshal data for sqlite insert", "error", err)
		}
	}

	// Extract StatusEvent if available
	var name, statusHostname, status, level, details string
	if se, ok := core.AsType[*core.StatusEvent](msg.Data); ok && se != nil {
		name = se.Name
		statusHostname = se.Hostname
		status = se.Status
		level = se.Level
		details = se.Details
		logger.Debug("statusmon: inserting status event", "name", name, "hostname", statusHostname, "status", status, "level", level)
	} else {
		logger.Debug("statusmon: message data is not a StatusEvent", "dataType", msg.DataType, "dataKind", fmt.Sprintf("%T", msg.Data))
	}

	return `INSERT INTO status (timestamp, service_name, service_type, hostname, event, name, status_hostname, status, level, details, data) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		[]any{msg.Timestamp, msg.ServiceName, msg.ServiceType, msg.Hostname, msg.Event, name, statusHostname, status, level, details, dataJSON}
}

// SetSQLiteDB allows the runtime to provide a pointer to the DB instance.
func (svc *Service) SetSQLiteDB(db **sql.DB) {
	svc.db = db
	// Run migrations if we have a db
	if err := svc.Migrate(); err != nil {
		// Log the error but don't fail - allow the service to continue
		logger := svc.Deps.MustGetLogger()
		logger.Warn("statusmon: migration warning", "error", err)
	}
}

// SetDBPath allows the runtime to provide the database file path.
func (svc *Service) SetDBPath(path string) {
	svc.dbPath = path
}
