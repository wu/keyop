// Package sqlite provides a SQLite-backed event storage service.
package sqlite

import (
	"database/sql"
	"fmt"
	"keyop/core"
	"path/filepath"
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

// RegisterProvider registers a schema provider for a specific service type.
func (svc *Service) RegisterProvider(serviceType string, provider SchemaProvider) {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	svc.providers[serviceType] = provider
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
	if p, ok := svc.Cfg.Config["dbPath"].(string); ok {
		dbPath = p
	}

	if !filepath.IsAbs(dbPath) {
		home, err := svc.Deps.MustGetOsProvider().UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		dbPath = filepath.Join(home, ".keyop", dbPath)
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
	for serviceType, provider := range svc.providers {
		schema := provider.SQLiteSchema()
		if schema != "" {
			if _, err := db.Exec(schema); err != nil {
				svc.mu.RUnlock()
				return fmt.Errorf("failed to initialize schema for %s: %w", serviceType, err)
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
	svc.mu.RLock()
	provider, ok := svc.providers[msg.ServiceType]
	svc.mu.RUnlock()

	if !ok {
		// Skip messages from services that didn't register a schema.
		return nil
	}

	query, args := provider.SQLiteInsert(msg)
	if query == "" {
		return nil
	}

	_, err := svc.db.Exec(query, args...)
	if err != nil {
		svc.Deps.MustGetLogger().Error("failed to insert message into sqlite", "error", err, "serviceType", msg.ServiceType, "query", query)
		return err
	}

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
