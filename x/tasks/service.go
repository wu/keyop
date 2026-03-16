package tasks

import (
	"database/sql"
	"fmt"
	"keyop/core"
	"keyop/x/logicalday"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // sqlite driver
)

// Service implements the tasks service for displaying tasks scheduled for today or completed today.
type Service struct {
	Deps         core.Dependencies
	Cfg          core.ServiceConfig
	db           *sql.DB
	dbPath       string
	logicalCalc  *logicalday.Calculator
	endOfDayTime string
}

// NewService constructs the tasks service.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps:         deps,
		Cfg:          cfg,
		dbPath:       "~/.keyop/sqlite/tasks.sql",
		endOfDayTime: "04:00", // default
	}
}

// Check implements core.Service.Check.
func (svc *Service) Check() error {
	if svc.db == nil {
		return fmt.Errorf("tasks database not initialized")
	}
	var count int
	err := svc.db.QueryRow("SELECT COUNT(*) FROM tasks").Scan(&count)
	if err != nil {
		return fmt.Errorf("tasks database check failed: %w", err)
	}
	return nil
}

// ValidateConfig performs minimal validation.
func (svc *Service) ValidateConfig() []error {
	return nil
}

// Initialize opens the tasks database connection and creates the logical day calculator.
func (svc *Service) Initialize() error {
	dbPath := svc.dbPath

	// Expand tilde prefix
	if strings.HasPrefix(dbPath, "~/") {
		home, err := svc.Deps.MustGetOsProvider().UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		dbPath = filepath.Join(home, dbPath[2:])
	}

	svc.Deps.MustGetLogger().Info("tasks: initializing database", "dbPath", dbPath)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open tasks database: %w", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping tasks database: %w", err)
	}

	svc.db = db

	// Ensure in-progress columns exist in the tasks table (added automatically if missing)
	if err := svc.ensureInProgressColumns(); err != nil {
		svc.Deps.MustGetLogger().Warn("tasks: failed to ensure in-progress columns", "error", err)
	}

	// Get end-of-day time from global config if provided
	// Note: We look for it in the Dependencies, not in ServiceConfig
	// For now, use the default
	logger := svc.Deps.MustGetLogger()
	logger.Debug("tasks: using end-of-day time", "time", svc.endOfDayTime)

	// Get local timezone - default to America/Los_Angeles, fallback to UTC
	var loc *time.Location
	loc, tzErr := time.LoadLocation("America/Los_Angeles")
	if tzErr != nil {
		logger.Warn("tasks: failed to load America/Los_Angeles timezone, falling back to UTC", "error", tzErr)
		loc = time.UTC
	}

	// Create logical day calculator
	svc.logicalCalc = logicalday.NewCalculator(svc.endOfDayTime, loc)

	return nil
}

// OnShutdown closes the database connection.
func (svc *Service) OnShutdown() error {
	if svc.db != nil {
		return svc.db.Close()
	}
	return nil
}

// ensureInProgressColumns checks the tasks table schema and adds in-progress columns if missing.
func (svc *Service) ensureInProgressColumns() error {
	if svc.db == nil {
		return nil
	}
	cols := map[string]bool{}
	if pr, err := svc.db.Query("PRAGMA table_info(tasks)"); err == nil {
		defer func() {
			if err := pr.Close(); err != nil {
				svc.Deps.MustGetLogger().Warn("tasks: failed to close pragma rows", "error", err)
			}
		}()
		for pr.Next() {
			var cid int
			var name string
			var ctype string
			var notnull int
			var dflt interface{}
			var pk int
			if err := pr.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err == nil {
				cols[name] = true
			}
		}
	} else {
		return err
	}

	if !cols["in_progress"] {
		if _, err := svc.db.Exec("ALTER TABLE tasks ADD COLUMN in_progress INTEGER DEFAULT 0"); err != nil {
			svc.Deps.MustGetLogger().Warn("tasks: failed to add in_progress column", "error", err)
		}
	}
	if !cols["in_progress_started_at"] {
		if _, err := svc.db.Exec("ALTER TABLE tasks ADD COLUMN in_progress_started_at TEXT"); err != nil {
			svc.Deps.MustGetLogger().Warn("tasks: failed to add in_progress_started_at column", "error", err)
		}
	}
	if !cols["in_progress_total_seconds"] {
		if _, err := svc.db.Exec("ALTER TABLE tasks ADD COLUMN in_progress_total_seconds INTEGER DEFAULT 0"); err != nil {
			svc.Deps.MustGetLogger().Warn("tasks: failed to add in_progress_total_seconds column", "error", err)
		}
	}
	return nil
}
