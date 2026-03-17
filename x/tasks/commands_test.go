package tasks

import (
	"database/sql"
	"testing"
	"time"

	"keyop/core"
	"keyop/core/testutil"
	"keyop/x/logicalday"

	_ "modernc.org/sqlite"
)

// newTestService creates a Service with an in-memory sqlite DB and minimal schema for testing commands.
func newTestService(t *testing.T) *Service {
	svc := &Service{}
	var deps core.Dependencies
	deps.SetLogger(&core.FakeLogger{})
	deps.SetMessenger(testutil.NewFakeMessenger())
	svc.Deps = deps

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	svc.db = db

	// Minimal tasks table used by the command code (expanded to include columns used by fetchRecentTasks)
	_, err = db.Exec(`CREATE TABLE tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		uuid TEXT,
		title TEXT,
		done INTEGER DEFAULT 0,
		scheduled_date TEXT,
		completed_at TEXT,
		tags TEXT DEFAULT '',
		scheduled_time INTEGER DEFAULT 0,
		updated_at TEXT,
		in_progress INTEGER DEFAULT 0,
		in_progress_started_at TEXT,
		in_progress_total_seconds INTEGER DEFAULT 0,
		color TEXT,
		recurrence TEXT,
		recurrence_days TEXT,
		recurrence_x INTEGER
	)`)
	if err != nil {
		t.Fatal(err)
	}

	svc.logicalCalc = logicalday.NewCalculator("04:00", time.UTC)
	return svc
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func insertTask(t *testing.T, svc *Service, title string, scheduledAt string, hasTime bool, recurrence string, recurrenceDays string, recurrenceX int, updatedAt string, color string) int64 {
	res, err := svc.db.Exec(`INSERT INTO tasks (uuid, title, scheduled_date, scheduled_time, updated_at, color, recurrence, recurrence_days, recurrence_x) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"uuid-test", title, scheduledAt, boolToInt(hasTime), updatedAt, color, recurrence, recurrenceDays, recurrenceX)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRunTaskCommand_Color(t *testing.T) {
	svc := newTestService(t)
	id := insertTask(t, svc, "task-color", "", false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	if _, err := svc.runTaskCommand(id, "c red", ""); err != nil {
		t.Fatalf("runTaskCommand set color failed: %v", err)
	}
	var col sql.NullString
	if err := svc.db.QueryRow("SELECT color FROM tasks WHERE id = ?", id).Scan(&col); err != nil {
		t.Fatalf("select color: %v", err)
	}
	if col.String != "red" {
		t.Fatalf("expected color 'red', got %q", col.String)
	}

	// Clear color
	if _, err := svc.runTaskCommand(id, "c 0", ""); err != nil {
		t.Fatalf("runTaskCommand clear color failed: %v", err)
	}
	if err := svc.db.QueryRow("SELECT color FROM tasks WHERE id = ?", id).Scan(&col); err != nil {
		t.Fatalf("select color after clear: %v", err)
	}
	if col.Valid && col.String != "" {
		t.Fatalf("expected empty color after clear, got %q", col.String)
	}
}

func TestRunTaskCommand_Reschedule(t *testing.T) {
	svc := newTestService(t)
	base := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)
	id := insertTask(t, svc, "task-res", base.Format(time.RFC3339Nano), false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	if _, err := svc.runTaskCommand(id, "r +1d", ""); err != nil {
		t.Fatalf("reschedule failed: %v", err)
	}
	var scheduled sql.NullString
	if err := svc.db.QueryRow("SELECT scheduled_date FROM tasks WHERE id = ?", id).Scan(&scheduled); err != nil {
		t.Fatalf("select scheduled_date: %v", err)
	}
	if scheduled.String == "" {
		t.Fatalf("expected scheduled_date to be set")
	}
	parsed, err := time.Parse(time.RFC3339Nano, scheduled.String)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, scheduled.String)
		if err != nil {
			t.Fatalf("parse scheduled_date: %v", err)
		}
	}
	expected := base.Add(24 * time.Hour)
	if parsed.UTC().Year() != expected.Year() || parsed.UTC().Month() != expected.Month() || parsed.UTC().Day() != expected.Day() {
		t.Fatalf("expected %v got %v", expected, parsed)
	}
}

func TestRunTaskCommand_Skip(t *testing.T) {
	svc := newTestService(t)
	base := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)
	id := insertTask(t, svc, "task-skip", base.Format(time.RFC3339Nano), false, "daily", "", 1, time.Now().UTC().Format(time.RFC3339Nano), "")

	if _, err := svc.runTaskCommand(id, "skip", ""); err != nil {
		t.Fatalf("skip failed: %v", err)
	}
	var scheduled sql.NullString
	if err := svc.db.QueryRow("SELECT scheduled_date FROM tasks WHERE id = ?", id).Scan(&scheduled); err != nil {
		t.Fatalf("select scheduled_date after skip: %v", err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, scheduled.String)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, scheduled.String)
		if err != nil {
			t.Fatalf("parse scheduled_date: %v", err)
		}
	}
	expected := base.Add(24 * time.Hour)
	if parsed.UTC().Year() != expected.Year() || parsed.UTC().Month() != expected.Month() || parsed.UTC().Day() != expected.Day() {
		t.Fatalf("expected %v got %v", expected, parsed)
	}
}

func TestFetchRecentTasks(t *testing.T) {
	svc := newTestService(t)
	now := time.Now().UTC()
	insertTask(t, svc, "t1", "", false, "", "", 0, now.Add(-24*time.Hour).Format(time.RFC3339Nano), "")
	insertTask(t, svc, "t2", "", false, "", "", 0, now.Format(time.RFC3339Nano), "")

	// Debug: check raw DB count before calling fetch
	var cnt int
	if err := svc.db.QueryRow("SELECT COUNT(*) FROM tasks").Scan(&cnt); err != nil {
		t.Fatalf("failed to count tasks: %v", err)
	}
	t.Logf("DB row count: %d", cnt)

	res, err := svc.fetchTasks("recent")
	if err != nil {
		t.Fatalf("fetchTasks recent error: %v", err)
	}
	// Debug: log the raw res for troubleshooting
	// t.Logf("fetchTasks recent returned: %#v", res)
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("unexpected fetchTasks return type: %T", res)
	}
	tasksIface, ok := m["tasks"]
	if !ok {
		t.Fatalf("no tasks in fetchTasks result; res keys: %v", m)
	}
	// Try multiple possible types for tasksIface ([]TaskRow or []interface{})
	if tasksSlice, ok := tasksIface.([]TaskRow); ok {
		if len(tasksSlice) < 2 {
			t.Fatalf("expected >=2 tasks, got %d", len(tasksSlice))
		}
		if tasksSlice[0].Title != "t2" || tasksSlice[1].Title != "t1" {
			t.Fatalf("expected recent tasks ordered by updated_at desc, got %q then %q", tasksSlice[0].Title, tasksSlice[1].Title)
		}
		return
	}
	if tasksIFaceSlice, ok := tasksIface.([]interface{}); ok {
		if len(tasksIFaceSlice) < 2 {
			t.Fatalf("expected >=2 tasks (interface slice), got %d", len(tasksIFaceSlice))
		}
		return
	}
	// Fallback: print type and fail
	t.Fatalf("tasks type mismatch, got %T", tasksIface)
}
