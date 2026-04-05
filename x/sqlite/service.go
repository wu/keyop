// Package sqlite provides a SQLite-backed event storage service.
package sqlite

import (
	"database/sql"
	"fmt"
	"keyop/core"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite" // sqlite driver used for embedded builds
)

// Consumer is an optional interface that services can implement to receive
// a reference to the SQLite database.
type Consumer interface {
	SetSQLiteDB(db **sql.DB)
}

// SchemaProvider is the interface that services must implement to register with the SQLite service.
type SchemaProvider interface {
	// SQLiteSchema returns the SQL DDL for the table(s) needed by the service.
	SQLiteSchema() string
	// SQLiteInsert returns the SQL INSERT statement and the arguments to be used for a given message.
	// If the message is not handled by this provider, it should return an empty string.
	SQLiteInsert(msg core.Message) (query string, args []any)
}

// Service implements a SQLite-backed event storage service.
type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig

	db        *sql.DB
	providers map[string]SchemaProvider
	mu        sync.RWMutex
}

// NewService creates a new SQLite service.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps:      deps,
		Cfg:       cfg,
		providers: make(map[string]SchemaProvider),
	}
}

// RegisterProvider registers a schema provider keyed by payload type.
func (svc *Service) RegisterProvider(payloadType string, provider SchemaProvider) {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	svc.providers[payloadType] = provider
}

// ValidateConfig validates the service configuration.
func (svc *Service) ValidateConfig() []error {
	var errs []error
	if len(svc.Cfg.Subs) == 0 {
		errs = append(errs, fmt.Errorf("sqlite service requires at least one subscription in 'subs'"))
	}
	return errs
}

// Initialize sets up the SQLite database and starts the message listener.
func (svc *Service) Initialize() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()
	ctx := svc.Deps.MustGetContext()

	dbPath := "events.db"
	// Prefer explicit configuration, but allow an override from the persistent state store.
	if p, ok := svc.Cfg.Config["dbPath"].(string); ok {
		dbPath = p
	}
	// DB path may be configured via svc.Cfg.Config["dbPath"].
	// Note: the runtime may populate this from the webui service configuration before initialization.

	// Expand non-absolute paths into the ~/.keyop directory
	if !filepath.IsAbs(dbPath) {
		home, err := svc.Deps.MustGetOsProvider().UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		// Support ~ prefix
		if strings.HasPrefix(dbPath, "~/") {
			dbPath = filepath.Join(home, dbPath[2:])
		} else {
			dbPath = filepath.Join(home, ".keyop", dbPath)
		}
	}

	// Ensure directory exists
	if err := svc.Deps.MustGetOsProvider().MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("failed to create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open sqlite database at %s: %w", dbPath, err)
	}
	svc.db = db

	// Initialize schemas
	svc.mu.RLock()
	for payloadType, provider := range svc.providers {
		schema := provider.SQLiteSchema()
		if schema != "" {
			// Split multiple statements and execute them separately
			// SQLite driver doesn't support multiple statements in a single Exec() call
			statements := strings.Split(schema, ";")
			for _, stmt := range statements {
				stmt = strings.TrimSpace(stmt)
				if stmt == "" {
					continue
				}
				// Execute the statement. For migration purposes, we handle some errors gracefully.
				if _, err := db.Exec(stmt); err != nil {
					// Check if this is a "duplicate column" error (for ALTER TABLE migrations)
					errMsg := err.Error()
					if strings.Contains(errMsg, "duplicate column") || strings.Contains(errMsg, "already exists") {
						logger.Debug("SQLite: migration already applied", "payloadType", payloadType, "stmt", stmt[:minInt(50, len(stmt))])
					} else {
						svc.mu.RUnlock()
						return fmt.Errorf("failed to initialize schema for %s: %w", payloadType, err)
					}
				}
			}
		}
	}
	svc.mu.RUnlock()

	// Subscribe to all channels listed in the 'subs' section
	for _, subInfo := range svc.Cfg.Subs {
		err = messenger.Subscribe(ctx, svc.Cfg.Name, subInfo.Name, svc.Cfg.Type, svc.Cfg.Name, subInfo.MaxAge, svc.handleMessage)
		if err != nil {
			return fmt.Errorf("failed to subscribe to %s: %w", subInfo.Name, err)
		}
	}

	logger.Info("SQLite service initialized", "dbPath", dbPath)
	return nil
}

func (svc *Service) handleMessage(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()

	// Only route by typed payload DataType. Legacy service-type routing removed.
	if msg.DataType == "" {
		return nil
	}

	// Normalize timestamp to UTC so SQLite string comparisons work correctly
	// regardless of the sender's local timezone.
	if !msg.Timestamp.IsZero() {
		msg.Timestamp = msg.Timestamp.UTC()
	}

	svc.mu.RLock()
	provider, ok := svc.providers[msg.DataType]
	svc.mu.RUnlock()

	if !ok {
		logger.Debug("sqlite: no provider registered for this payload type", "dataType", msg.DataType)
		return nil
	}

	query, args := provider.SQLiteInsert(msg)
	if query == "" {
		logger.Debug("sqlite: no insert query provided by provider", "dataType", msg.DataType)
		return nil
	}

	logger.Debug("sqlite: inserting data", "dataType", msg.DataType)
	_, err := svc.db.Exec(query, args...)
	if err != nil {
		logger.Error("sqlite: failed to insert message", "error", err, "dataType", msg.DataType, "query", query)
		return err
	}

	logger.Debug("sqlite: insertion successful", "dataType", msg.DataType)
	return nil
}

// Check performs a health check on the database.
func (svc *Service) Check() error {
	if svc.db == nil {
		return fmt.Errorf("database not initialized")
	}
	return svc.db.Ping()
}

// DB returns the underlying sql.DB instance.
func (svc *Service) DB() *sql.DB {

	return svc.db
}

// GetSQLiteDB returns a pointer to the sql.DB pointer so that it can be
// updated after the service is initialized.
func (svc *Service) GetSQLiteDB() **sql.DB {
	return &svc.db
}

// AcceptsPayloadType returns true if this sqlite service should accept messages
// for the provided payloadType. This is controlled by the optional
// 'payloadTypes' configuration key which may be a comma-separated string or
// an array of strings. If absent, the sqlite service accepts all payload types
// (backwards compatibility).
func (svc *Service) AcceptsPayloadType(payloadType string) bool {
	if svc.Cfg.Config == nil {
		return true
	}
	if v, ok := svc.Cfg.Config["payloadTypes"]; ok {
		switch t := v.(type) {
		case string:
			for _, s := range strings.Split(t, ",") {
				if strings.TrimSpace(s) == payloadType {
					return true
				}
			}
			return false
		case []interface{}:
			for _, vi := range t {
				if s, ok := vi.(string); ok && s == payloadType {
					return true
				}
			}
			return false
		case []string:
			for _, s := range t {
				if s == payloadType {
					return true
				}
			}
			return false
		default:
			// Unknown type, be permissive
			return true
		}
	}
	return true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
