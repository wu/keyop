// Package alerts provides a lightweight service that acts as a schema
// provider for storing alert messages into SQLite. It intentionally keeps
// behaviour minimal: the runtime will register this provider with any
// configured sqlite service that accepts the "alerts" service type.
package alerts

import (
	"database/sql"
	"encoding/json"
	"keyop/core"
)

// Service implements a minimal alerts service that also acts as a SQLite
// schema provider so alerts can be persisted to a sqlite instance configured
// to accept the "alerts" service type.
type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig
	db   **sql.DB
}

// NewService constructs the alerts service.
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

// SQLiteSchema returns the DDL for the alerts table.
func (svc *Service) SQLiteSchema() string {
	return `CREATE TABLE IF NOT EXISTS alerts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME,
		service_name TEXT,
		service_type TEXT,
		hostname TEXT,
		event TEXT,
		severity TEXT,
		summary TEXT,
		text TEXT,
		data TEXT
	);`
}

// SQLiteInsert prepares an INSERT for incoming messages. It marshals the Data
// field to JSON for storage in the data column.
func (svc *Service) SQLiteInsert(msg core.Message) (string, []any) {
	// Accept any message for insertion; the sqlite service will only call this
	// provider for messages whose ServiceType matched during registration.
	var dataJSON string
	if msg.Data != nil {
		if b, err := json.Marshal(msg.Data); err == nil {
			dataJSON = string(b)
		} else {
			// Best-effort: log but don't fail the insert generation
			svc.Deps.MustGetLogger().Warn("alerts: failed to marshal data for sqlite insert", "error", err)
		}
	}

	// Prefer typed payload (core.AlertEvent).
	var summary, severity string
	if ap, ok := core.AsType[*core.AlertEvent](msg.Data); ok {
		if ap != nil {
			summary = ap.Summary
			severity = ap.Level
		}
	} else if av, ok := core.AsType[core.AlertEvent](msg.Data); ok {
		summary = av.Summary
		severity = av.Level
	}

	return `INSERT INTO alerts (timestamp, service_name, service_type, hostname, event, severity, summary, text, data) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		[]any{msg.Timestamp, msg.ServiceName, msg.ServiceType, msg.Hostname, msg.Event, severity, summary, msg.Text, dataJSON}
}

// SetSQLiteDB allows the runtime to provide a pointer to the DB instance.
func (svc *Service) SetSQLiteDB(db **sql.DB) {
	svc.db = db
}
