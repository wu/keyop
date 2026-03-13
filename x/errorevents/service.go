// Package errorevents provides a lightweight service that acts as a schema
// provider for storing error messages into SQLite.
package errorevents

import (
	"database/sql"
	"encoding/json"
	"keyop/core"
)

// Service implements a minimal errors service that also acts as a SQLite
// schema provider so errors can be persisted to a sqlite instance configured
// to accept the "errors" payload type.
type Service struct {
	Deps   core.Dependencies
	Cfg    core.ServiceConfig
	db     **sql.DB
	dbPath string
}

// NewService constructs the errors service.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{Deps: deps, Cfg: cfg}
}

// Check implements core.Service.Check.
func (svc *Service) Check() error { return nil }

// ValidateConfig performs minimal validation.
func (svc *Service) ValidateConfig() []error {
	return nil
}

// Initialize does not need to subscribe itself; the sqlite service will use
// the SchemaProvider to insert messages. Still, implement Initialize to
// satisfy core.Service contract.
func (svc *Service) Initialize() error {
	return nil
}

// SQLiteSchema returns the DDL for the errors table.
func (svc *Service) SQLiteSchema() string {
	return `CREATE TABLE IF NOT EXISTS errors (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME,
		service_name TEXT,
		service_type TEXT,
		hostname TEXT,
		event TEXT,
		severity TEXT,
		summary TEXT,
		text TEXT,
		data TEXT,
		seen INTEGER DEFAULT 0
	);
	ALTER TABLE errors ADD COLUMN seen INTEGER DEFAULT 0;`
}

// SQLiteInsert prepares an INSERT for incoming messages. It marshals the Data
// field to JSON for storage in the data column.
func (svc *Service) SQLiteInsert(msg core.Message) (string, []any) {
	// Accept any message for insertion; the sqlite service will only call this
	// provider for messages whose DataType matched during registration.
	var dataJSON string
	if msg.Data != nil {
		if b, err := json.Marshal(msg.Data); err == nil {
			dataJSON = string(b)
		} else {
			// Best-effort: log but don't fail the insert generation
			svc.Deps.MustGetLogger().Warn("errors: failed to marshal data for sqlite insert", "error", err)
		}
	}

	// Prefer typed payload (core.ErrorEvent).
	var summary, severity string
	if ep, ok := core.AsType[*core.ErrorEvent](msg.Data); ok {
		if ep != nil {
			summary = ep.Summary
			severity = ep.Level
		}
	} else if ev, ok := core.AsType[core.ErrorEvent](msg.Data); ok {
		summary = ev.Summary
		severity = ev.Level
	}

	return `INSERT INTO errors (timestamp, service_name, service_type, hostname, event, severity, summary, text, data) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		[]any{msg.Timestamp, msg.ServiceName, msg.ServiceType, msg.Hostname, msg.Event, severity, summary, msg.Text, dataJSON}
}

// SetSQLiteDB allows the runtime to provide a pointer to the DB instance.
func (svc *Service) SetSQLiteDB(db **sql.DB) {
	svc.db = db
}

// SetDBPath allows the runtime to provide the database file path.
func (svc *Service) SetDBPath(path string) {
	svc.dbPath = path
}

// PayloadTypes returns the payload type names that this provider handles.
// Errors are represented by the core.ErrorEvent in the payload registry and
// the legacy alias "error" for backwards compatibility.
func (svc *Service) PayloadTypes() []string {
	return []string{"core.error.v1", "error"}
}
