package tasks

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"keyop/core"
	"keyop/x/logicalday"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
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

	// Subscribe to configured channels (e.g. to receive task_create events)
	if len(svc.Cfg.Subs) > 0 {
		messenger := svc.Deps.MustGetMessenger()
		ctx := svc.Deps.MustGetContext()
		for _, subInfo := range svc.Cfg.Subs {
			if err := messenger.Subscribe(ctx, svc.Cfg.Name, subInfo.Name, svc.Cfg.Type, svc.Cfg.Name, subInfo.MaxAge, svc.handleMessage); err != nil {
				return fmt.Errorf("tasks: failed to subscribe to %s: %w", subInfo.Name, err)
			}
		}
	}

	return nil
}

// handleMessage processes incoming messages on subscribed channels.
func (svc *Service) handleMessage(msg core.Message) error {
	if msg.Event != "task_create" {
		return nil
	}

	event, ok := core.AsType[*TaskCreateEvent](msg.Data)
	if !ok {
		return fmt.Errorf("tasks: task_create event has unexpected payload type %T", msg.Data)
	}

	if err := svc.insertTaskFromEvent(event); err != nil {
		svc.Deps.MustGetLogger().Error("tasks: failed to insert task from event", "error", err, "source", event.Source)
		return err
	}

	svc.Deps.MustGetLogger().Info("tasks: created task from event", "title", event.Title, "source", event.Source)
	return nil
}

// insertTaskFromEvent inserts a new task row from a TaskCreateEvent.
func (svc *Service) insertTaskFromEvent(event *TaskCreateEvent) error {
	if svc.db == nil {
		return fmt.Errorf("tasks database not available")
	}

	title := strings.TrimSpace(event.Title)
	if title == "" {
		return fmt.Errorf("task title cannot be empty")
	}

	// Resolve scheduled date from DueAt or logical today
	var scheduledDate time.Time
	hasScheduledTime := 0

	if event.DueAt != "" {
		t, err := time.Parse(time.RFC3339, event.DueAt)
		if err == nil {
			scheduledDate = t
			if event.HasScheduledTime {
				hasScheduledTime = 1
			}
		}
	}
	if scheduledDate.IsZero() {
		if svc.logicalCalc != nil {
			scheduledDate = svc.logicalCalc.Today()
		} else {
			scheduledDate = time.Now()
		}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err := svc.db.Exec(
		`INSERT INTO tasks (uuid, title, note, scheduled_date, scheduled_time, tags, color, importance, user_id, created_at, updated_at, done)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		uuid.New().String(), title, event.Note,
		scheduledDate.UTC().Format(time.RFC3339Nano), hasScheduledTime,
		event.Tags, event.Color, event.Importance, event.UserID,
		now, now,
	)
	if err != nil {
		return err
	}

	// Notify the browser so the task list refreshes immediately.
	messenger := svc.Deps.MustGetMessenger()
	if messenger != nil {
		notification := core.Message{
			Version:     "1.0",
			Timestamp:   time.Now(),
			ChannelName: "tasks",
			ServiceType: "tasks",
			ServiceName: "tasks",
			Event:       "taskCreated",
			Status:      "created",
		}
		taskDetails := map[string]any{"title": title, "source": event.Source}
		if body, jsonErr := json.Marshal(taskDetails); jsonErr == nil {
			notification.Body = string(body)
		}
		if sendErr := messenger.Send(notification); sendErr != nil {
			svc.Deps.MustGetLogger().Warn("tasks: failed to send taskCreated notification", "error", sendErr)
		}
	}
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
