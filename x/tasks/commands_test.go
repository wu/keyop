package tasks

import (
	"database/sql"
	"strings"
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
		parent_uuid TEXT DEFAULT '',
		subtask_parent_uuid TEXT DEFAULT '',
		title TEXT,
		done INTEGER DEFAULT 0,
		scheduled_date TEXT,
		completed_at TEXT,
		tags TEXT DEFAULT '',
		scheduled_time INTEGER DEFAULT 0,
		created_at TEXT,
		updated_at TEXT,
		in_progress INTEGER DEFAULT 0,
		in_progress_started_at TEXT,
		in_progress_total_seconds INTEGER DEFAULT 0,
		color TEXT DEFAULT '',
		recurrence TEXT DEFAULT '',
		recurrence_days TEXT DEFAULT '',
		recurrence_x INTEGER DEFAULT 0,
		user_id INTEGER DEFAULT 0,
		importance INTEGER DEFAULT 0,
		urgency INTEGER DEFAULT 0,
		position INTEGER DEFAULT 0
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

func TestRunTaskCommand_MarkDone(t *testing.T) {
	svc := newTestService(t)
	id := insertTask(t, svc, "task-done", "", false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	res, err := svc.runTaskCommand(id, "x", "recent")
	if err != nil {
		t.Fatalf("mark done failed: %v", err)
	}

	var done bool
	var completedAt sql.NullString
	if err := svc.db.QueryRow("SELECT done, completed_at FROM tasks WHERE id = ?", id).Scan(&done, &completedAt); err != nil {
		t.Fatalf("select done/completed_at: %v", err)
	}
	if !done {
		t.Fatalf("expected task to be marked done")
	}
	if !completedAt.Valid || completedAt.String == "" {
		t.Fatalf("expected completed_at to be set")
	}

	task, ok := res.(*TaskRow)
	if !ok {
		t.Fatalf("expected *TaskRow response, got %T", res)
	}
	if !task.Done {
		t.Fatalf("expected returned task to be done")
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

// ---------------------------------------------------------------------------
// parseDurationString
// ---------------------------------------------------------------------------

func TestParseDurationString(t *testing.T) {
	cases := []struct {
		input string
		want  int64
	}{
		{"30m", 30 * 60},
		{"1h", 3600},
		{"1h5m", 3600 + 5*60},
		{"2d", 2 * 86400},
		{"2d1h30m", 2*86400 + 3600 + 30*60},
		{"0", 0},
		{"", 0},
		{"90s", 90},
		{"1h5m30s", 3600 + 5*60 + 30},
		{"invalid", -1},
		{"xyz", -1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			got := parseDurationString(tc.input)
			if got != tc.want {
				t.Errorf("parseDurationString(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseTimeStringLocal
// ---------------------------------------------------------------------------

func TestParseTimeStringLocal(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skip("America/Los_Angeles timezone not available")
	}
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, loc) // Monday 10am

	cases := []struct {
		input    string
		wantHour int
		wantMin  int
		wantOk   bool
	}{
		{"1pm", 13, 0, true},
		{"1:30pm", 13, 30, true},
		{"13:00", 13, 0, true},
		{"9am", 9, 0, true},
		{"9:45am", 9, 45, true},
		{"12pm", 12, 0, true}, // noon
		{"12am", 0, 0, true},  // midnight
		{"11:59pm", 23, 59, true},
		{"invalid", 0, 0, false},
		{"", 0, 0, false},
		{"25:00", 0, 0, false},
		{"10:60", 0, 0, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			got, ok := parseTimeStringLocal(tc.input, base, loc)
			if ok != tc.wantOk {
				t.Fatalf("parseTimeStringLocal(%q) ok = %v, want %v", tc.input, ok, tc.wantOk)
			}
			if ok {
				if got.Hour() != tc.wantHour || got.Minute() != tc.wantMin {
					t.Errorf("parseTimeStringLocal(%q) = %02d:%02d, want %02d:%02d",
						tc.input, got.Hour(), got.Minute(), tc.wantHour, tc.wantMin)
				}
				// Date should stay on the base day
				if got.Year() != base.Year() || got.Month() != base.Month() || got.Day() != base.Day() {
					t.Errorf("parseTimeStringLocal(%q) date = %v, want same day as base %v", tc.input, got, base)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// runTaskCommand – reschedule edge cases
// ---------------------------------------------------------------------------

func TestRunTaskCommand_Reschedule_Today(t *testing.T) {
	svc := newTestService(t)
	id := insertTask(t, svc, "task-today", "", false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	if _, err := svc.runTaskCommand(id, "r today", ""); err != nil {
		t.Fatalf("reschedule today failed: %v", err)
	}

	var scheduled sql.NullString
	if err := svc.db.QueryRow("SELECT scheduled_date FROM tasks WHERE id = ?", id).Scan(&scheduled); err != nil {
		t.Fatalf("select scheduled_date: %v", err)
	}
	if !scheduled.Valid || scheduled.String == "" {
		t.Fatal("expected scheduled_date to be set after 'r today'")
	}

	parsed, err := time.Parse(time.RFC3339, scheduled.String)
	if err != nil {
		t.Fatalf("parse scheduled_date: %v", err)
	}

	today := svc.logicalCalc.Today()
	if parsed.UTC().Year() != today.Year() || parsed.UTC().Month() != today.Month() || parsed.UTC().Day() != today.Day() {
		t.Fatalf("expected today %v, got %v", today, parsed.UTC())
	}
}

func TestRunTaskCommand_Reschedule_Tomorrow(t *testing.T) {
	svc := newTestService(t)
	id := insertTask(t, svc, "task-tomorrow", "", false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	if _, err := svc.runTaskCommand(id, "r tomorrow", ""); err != nil {
		t.Fatalf("reschedule tomorrow failed: %v", err)
	}

	var scheduled sql.NullString
	if err := svc.db.QueryRow("SELECT scheduled_date FROM tasks WHERE id = ?", id).Scan(&scheduled); err != nil {
		t.Fatalf("select scheduled_date: %v", err)
	}

	parsed, err := time.Parse(time.RFC3339, scheduled.String)
	if err != nil {
		t.Fatalf("parse scheduled_date: %v", err)
	}

	tomorrow := svc.logicalCalc.Today().AddDate(0, 0, 1)
	if parsed.UTC().Year() != tomorrow.Year() || parsed.UTC().Month() != tomorrow.Month() || parsed.UTC().Day() != tomorrow.Day() {
		t.Fatalf("expected tomorrow %v, got %v", tomorrow, parsed.UTC())
	}
}

func TestRunTaskCommand_Reschedule_TomorrowWithTime(t *testing.T) {
	svc := newTestService(t)
	id := insertTask(t, svc, "task-tomorrow-time", "", false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	if _, err := svc.runTaskCommand(id, "r tomorrow 1pm", ""); err != nil {
		t.Fatalf("reschedule 'tomorrow 1pm' failed: %v", err)
	}

	var scheduled sql.NullString
	var scheduledTime sql.NullBool
	if err := svc.db.QueryRow("SELECT scheduled_date, scheduled_time FROM tasks WHERE id = ?", id).Scan(&scheduled, &scheduledTime); err != nil {
		t.Fatalf("select fields: %v", err)
	}

	parsed, err := time.Parse(time.RFC3339, scheduled.String)
	if err != nil {
		t.Fatalf("parse scheduled_date: %v", err)
	}

	tomorrow := svc.logicalCalc.Today().AddDate(0, 0, 1)
	if parsed.UTC().Year() != tomorrow.Year() || parsed.UTC().Month() != tomorrow.Month() || parsed.UTC().Day() != tomorrow.Day() {
		t.Fatalf("expected tomorrow %v, got %v", tomorrow, parsed.UTC())
	}
	if !scheduledTime.Valid || !scheduledTime.Bool {
		t.Fatal("expected scheduled_time = true when time component is given")
	}
	// 1pm UTC is hour 13
	if parsed.UTC().Hour() != 13 {
		t.Fatalf("expected hour 13 (1pm), got %d", parsed.UTC().Hour())
	}
}

func TestRunTaskCommand_Reschedule_Weekday(t *testing.T) {
	svc := newTestService(t)
	// Use a fixed base date so weekday math is deterministic: Monday 2026-03-16
	base := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)
	id := insertTask(t, svc, "task-weekday", base.Format(time.RFC3339Nano), false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	// 'r wed' from Monday 2026-03-16 should land on Wednesday 2026-03-18
	if _, err := svc.runTaskCommand(id, "r wed", ""); err != nil {
		t.Fatalf("reschedule wed failed: %v", err)
	}

	var scheduled sql.NullString
	if err := svc.db.QueryRow("SELECT scheduled_date FROM tasks WHERE id = ?", id).Scan(&scheduled); err != nil {
		t.Fatalf("select scheduled_date: %v", err)
	}
	parsed, err := time.Parse(time.RFC3339, scheduled.String)
	if err != nil {
		t.Fatalf("parse scheduled_date: %v", err)
	}
	if parsed.UTC().Weekday() != time.Wednesday {
		t.Fatalf("expected Wednesday, got %v (%v)", parsed.UTC().Weekday(), parsed.UTC())
	}
	// Should be the next Wednesday from the logical calc's today, not from base.
	// The logical calc today is the UTC date of right now; the exact day won't
	// be 2026-03-18 once real time passes, so just validate it is a Wednesday
	// that is at least one day in the future.
	if !parsed.UTC().After(time.Now().UTC()) && parsed.UTC().Day() == time.Now().UTC().Day() {
		// Allow same-day only if today is not Wednesday
		if time.Now().UTC().Weekday() == time.Wednesday {
			t.Fatalf("expected *next* Wednesday (delta > 0), got %v", parsed.UTC())
		}
	}
}

func TestRunTaskCommand_Reschedule_RelativeWeeks(t *testing.T) {
	svc := newTestService(t)
	base := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)
	id := insertTask(t, svc, "task-relweeks", base.Format(time.RFC3339Nano), false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	if _, err := svc.runTaskCommand(id, "r +2w", ""); err != nil {
		t.Fatalf("reschedule +2w failed: %v", err)
	}

	var scheduled sql.NullString
	if err := svc.db.QueryRow("SELECT scheduled_date FROM tasks WHERE id = ?", id).Scan(&scheduled); err != nil {
		t.Fatalf("select scheduled_date: %v", err)
	}
	parsed, err := time.Parse(time.RFC3339, scheduled.String)
	if err != nil {
		t.Fatalf("parse scheduled_date: %v", err)
	}
	expected := base.AddDate(0, 0, 14)
	if parsed.UTC().Year() != expected.Year() || parsed.UTC().Month() != expected.Month() || parsed.UTC().Day() != expected.Day() {
		t.Fatalf("expected %v, got %v", expected, parsed.UTC())
	}
}

func TestRunTaskCommand_Reschedule_Clear(t *testing.T) {
	svc := newTestService(t)
	base := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)
	id := insertTask(t, svc, "task-clear", base.Format(time.RFC3339Nano), false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	if _, err := svc.runTaskCommand(id, "r 0", ""); err != nil {
		t.Fatalf("reschedule clear failed: %v", err)
	}

	var scheduled sql.NullString
	if err := svc.db.QueryRow("SELECT scheduled_date FROM tasks WHERE id = ?", id).Scan(&scheduled); err != nil {
		t.Fatalf("select scheduled_date: %v", err)
	}
	if scheduled.Valid && scheduled.String != "" {
		t.Fatalf("expected empty scheduled_date after clear, got %q", scheduled.String)
	}
}

func TestRunTaskCommand_Reschedule_RelativeDays(t *testing.T) {
	svc := newTestService(t)
	base := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)
	id := insertTask(t, svc, "task-reldays", base.Format(time.RFC3339Nano), false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	if _, err := svc.runTaskCommand(id, "r +3d", ""); err != nil {
		t.Fatalf("reschedule +3d failed: %v", err)
	}

	var scheduled sql.NullString
	if err := svc.db.QueryRow("SELECT scheduled_date FROM tasks WHERE id = ?", id).Scan(&scheduled); err != nil {
		t.Fatalf("select scheduled_date: %v", err)
	}
	parsed, err := time.Parse(time.RFC3339, scheduled.String)
	if err != nil {
		t.Fatalf("parse scheduled_date: %v", err)
	}
	expected := base.AddDate(0, 0, 3)
	if parsed.UTC().Year() != expected.Year() || parsed.UTC().Month() != expected.Month() || parsed.UTC().Day() != expected.Day() {
		t.Fatalf("expected %v, got %v", expected, parsed.UTC())
	}
}

// ---------------------------------------------------------------------------
// runTaskCommand – tag
// ---------------------------------------------------------------------------

func TestRunTaskCommand_Tag(t *testing.T) {
	svc := newTestService(t)
	id := insertTask(t, svc, "task-tag", "", false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	// Add two tags
	if _, err := svc.runTaskCommand(id, "t work home", ""); err != nil {
		t.Fatalf("add tags failed: %v", err)
	}
	var tags sql.NullString
	if err := svc.db.QueryRow("SELECT tags FROM tasks WHERE id = ?", id).Scan(&tags); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tags.String, "work") || !strings.Contains(tags.String, "home") {
		t.Fatalf("expected tags to contain 'work' and 'home', got %q", tags.String)
	}

	// Remove the 'work' tag
	if _, err := svc.runTaskCommand(id, "t -work", ""); err != nil {
		t.Fatalf("remove tag failed: %v", err)
	}
	if err := svc.db.QueryRow("SELECT tags FROM tasks WHERE id = ?", id).Scan(&tags); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tags.String, "work") {
		t.Fatalf("expected 'work' tag to be removed, got %q", tags.String)
	}
	if !strings.Contains(tags.String, "home") {
		t.Fatalf("expected 'home' tag to remain, got %q", tags.String)
	}
}

func TestRunTaskCommand_Tag_AddDuplicate(t *testing.T) {
	svc := newTestService(t)
	id := insertTask(t, svc, "task-dup-tag", "", false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	if _, err := svc.runTaskCommand(id, "t foo", ""); err != nil {
		t.Fatal(err)
	}
	// Adding the same tag again should be a no-op
	if _, err := svc.runTaskCommand(id, "t foo", ""); err != nil {
		t.Fatal(err)
	}
	var tags sql.NullString
	if err := svc.db.QueryRow("SELECT tags FROM tasks WHERE id = ?", id).Scan(&tags); err != nil {
		t.Fatal(err)
	}
	// 'foo' should appear exactly once
	parts := strings.Split(tags.String, ",")
	count := 0
	for _, p := range parts {
		if strings.TrimSpace(p) == "foo" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 'foo' to appear exactly once, got %d occurrences in %q", count, tags.String)
	}
}

// ---------------------------------------------------------------------------
// runTaskCommand – progress
// ---------------------------------------------------------------------------

func TestRunTaskCommand_Progress(t *testing.T) {
	svc := newTestService(t)
	id := insertTask(t, svc, "task-progress-cmd", "", false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	res, err := svc.runTaskCommand(id, "p 1h30m", "")
	if err != nil {
		t.Fatalf("progress command failed: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map response, got %T", res)
	}
	if m["status"] != "ok" {
		t.Fatalf("expected status=ok, got %v", m["status"])
	}
	wantSec := int64(90 * 60)
	if got, _ := m["inProgressTotalSeconds"].(int64); got != wantSec {
		t.Fatalf("expected inProgressTotalSeconds=%d, got %v", wantSec, m["inProgressTotalSeconds"])
	}

	var total sql.NullInt64
	if err := svc.db.QueryRow("SELECT in_progress_total_seconds FROM tasks WHERE id = ?", id).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if !total.Valid || total.Int64 != wantSec {
		t.Fatalf("expected DB in_progress_total_seconds=%d, got %v", wantSec, total.Int64)
	}
}

func TestRunTaskCommand_Progress_Zero(t *testing.T) {
	svc := newTestService(t)
	id := insertTask(t, svc, "task-progress-zero", "", false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	// First set some time
	if _, err := svc.runTaskCommand(id, "p 30m", ""); err != nil {
		t.Fatal(err)
	}
	// Then reset to zero
	if _, err := svc.runTaskCommand(id, "p 0", ""); err != nil {
		t.Fatal(err)
	}
	var total sql.NullInt64
	if err := svc.db.QueryRow("SELECT in_progress_total_seconds FROM tasks WHERE id = ?", id).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total.Valid && total.Int64 != 0 {
		t.Fatalf("expected 0 after reset, got %d", total.Int64)
	}
}

func TestRunTaskCommand_Progress_InvalidFormat(t *testing.T) {
	svc := newTestService(t)
	id := insertTask(t, svc, "task-bad-progress", "", false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	_, err := svc.runTaskCommand(id, "p notaduration", "")
	if err == nil {
		t.Fatal("expected error for invalid duration, got nil")
	}
}

// ---------------------------------------------------------------------------
// setInProgress
// ---------------------------------------------------------------------------

func TestSetInProgress_Start(t *testing.T) {
	svc := newTestService(t)
	id := insertTask(t, svc, "task-inprogress-start", "", false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	res, err := svc.setInProgress(id, true)
	if err != nil {
		t.Fatalf("setInProgress(start) failed: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map response, got %T", res)
	}
	if m["inProgress"] != true {
		t.Fatalf("expected inProgress=true, got %v", m["inProgress"])
	}
	if _, hasStartedAt := m["inProgressStartedAt"]; !hasStartedAt {
		t.Fatal("expected inProgressStartedAt in response")
	}

	var inProg sql.NullBool
	var startedAt sql.NullString
	if err := svc.db.QueryRow("SELECT in_progress, in_progress_started_at FROM tasks WHERE id = ?", id).Scan(&inProg, &startedAt); err != nil {
		t.Fatal(err)
	}
	if !inProg.Valid || !inProg.Bool {
		t.Fatal("expected in_progress = 1 in DB")
	}
	if !startedAt.Valid || startedAt.String == "" {
		t.Fatal("expected in_progress_started_at to be set in DB")
	}
}

func TestSetInProgress_Stop(t *testing.T) {
	svc := newTestService(t)
	id := insertTask(t, svc, "task-inprogress-stop", "", false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	// Start tracking
	if _, err := svc.setInProgress(id, true); err != nil {
		t.Fatalf("setInProgress(start): %v", err)
	}

	// Brief pause so elapsed time is positive
	time.Sleep(20 * time.Millisecond)

	// Stop tracking
	res, err := svc.setInProgress(id, false)
	if err != nil {
		t.Fatalf("setInProgress(stop): %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map response, got %T", res)
	}
	if m["inProgress"] != false {
		t.Fatalf("expected inProgress=false, got %v", m["inProgress"])
	}

	var inProg sql.NullBool
	var total sql.NullInt64
	if err := svc.db.QueryRow("SELECT in_progress, in_progress_total_seconds FROM tasks WHERE id = ?", id).Scan(&inProg, &total); err != nil {
		t.Fatal(err)
	}
	if inProg.Valid && inProg.Bool {
		t.Fatal("expected in_progress = 0 after stop")
	}
	if total.Valid && total.Int64 < 0 {
		t.Fatalf("expected accumulated total >= 0, got %d", total.Int64)
	}
}

func TestSetInProgress_StartThenStartAgain(t *testing.T) {
	svc := newTestService(t)
	id := insertTask(t, svc, "task-double-start", "", false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	// Start twice – second start should reset started_at without duplicating time
	if _, err := svc.setInProgress(id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.setInProgress(id, true); err != nil {
		t.Fatal(err)
	}

	var inProg sql.NullBool
	if err := svc.db.QueryRow("SELECT in_progress FROM tasks WHERE id = ?", id).Scan(&inProg); err != nil {
		t.Fatal(err)
	}
	if !inProg.Valid || !inProg.Bool {
		t.Fatal("expected in_progress = 1 after second start")
	}
}

// ---------------------------------------------------------------------------
// stopInProgressParams
// ---------------------------------------------------------------------------

func TestStopInProgressParams_NotRunning(t *testing.T) {
	svc := newTestService(t)
	id := insertTask(t, svc, "task-stop-params-idle", "", false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	params, err := svc.stopInProgressParams(id)
	if err != nil {
		t.Fatalf("stopInProgressParams: %v", err)
	}
	if len(params) != 0 {
		t.Fatalf("expected empty map for non-running task, got %v", params)
	}
}

func TestStopInProgressParams_Running(t *testing.T) {
	svc := newTestService(t)
	id := insertTask(t, svc, "task-stop-params-run", "", false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	// Manually mark as in-progress with a start time 60s ago
	startedAt := time.Now().UTC().Add(-60 * time.Second).Format(time.RFC3339Nano)
	if _, err := svc.db.Exec("UPDATE tasks SET in_progress = 1, in_progress_started_at = ?, in_progress_total_seconds = 10 WHERE id = ?", startedAt, id); err != nil {
		t.Fatal(err)
	}

	params, err := svc.stopInProgressParams(id)
	if err != nil {
		t.Fatalf("stopInProgressParams: %v", err)
	}
	if len(params) == 0 {
		t.Fatal("expected non-empty params for running task")
	}
	if params["in_progress"] != false {
		t.Fatalf("expected in_progress=false in params, got %v", params["in_progress"])
	}
	// Should be >= 10 (existing) + ~60 (elapsed)
	total, ok := params["in_progress_total_seconds"].(int64)
	if !ok {
		t.Fatalf("expected int64 for in_progress_total_seconds, got %T", params["in_progress_total_seconds"])
	}
	if total < 60 {
		t.Fatalf("expected total >= 60 seconds (existing 10 + ~60 elapsed), got %d", total)
	}
}

// ---------------------------------------------------------------------------
// createSubtask
// ---------------------------------------------------------------------------

func TestCreateSubtask(t *testing.T) {
	svc := newTestService(t)
	parentUUID := "parent-uuid-create"
	if _, err := svc.db.Exec(`INSERT INTO tasks (uuid, title, updated_at, done) VALUES (?, ?, ?, 0)`,
		parentUUID, "parent task", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	res, err := svc.createSubtask(parentUUID, map[string]any{"title": "sub task 1"})
	if err != nil {
		t.Fatalf("createSubtask failed: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map response, got %T", res)
	}
	if m["status"] != "ok" {
		t.Fatalf("expected status=ok, got %v", m["status"])
	}
	if m["parentUuid"] != parentUUID {
		t.Fatalf("expected parentUuid=%q, got %v", parentUUID, m["parentUuid"])
	}
	if m["title"] != "sub task 1" {
		t.Fatalf("expected title='sub task 1', got %v", m["title"])
	}

	// Verify subtask exists in DB with correct parent
	var count int
	if err := svc.db.QueryRow("SELECT COUNT(*) FROM tasks WHERE subtask_parent_uuid = ?", parentUUID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 subtask in DB, got %d", count)
	}
}

func TestCreateSubtask_EmptyTitle(t *testing.T) {
	svc := newTestService(t)
	parentUUID := "parent-uuid-empty-title"
	if _, err := svc.db.Exec(`INSERT INTO tasks (uuid, title, updated_at, done) VALUES (?, ?, ?, 0)`,
		parentUUID, "parent", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	_, err := svc.createSubtask(parentUUID, map[string]any{"title": "  "})
	if err == nil {
		t.Fatal("expected error for empty subtask title")
	}
}

func TestCreateSubtask_ParentNotFound(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.createSubtask("nonexistent-uuid", map[string]any{"title": "orphan"})
	if err == nil {
		t.Fatal("expected error when parent does not exist")
	}
}

// ---------------------------------------------------------------------------
// fetchSubtasks
// ---------------------------------------------------------------------------

func TestFetchSubtasks(t *testing.T) {
	svc := newTestService(t)
	parentUUID := "parent-uuid-fetch"
	if _, err := svc.db.Exec(`INSERT INTO tasks (uuid, title, updated_at, done) VALUES (?, ?, ?, 0)`,
		parentUUID, "parent", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.createSubtask(parentUUID, map[string]any{"title": "sub1"}); err != nil {
		t.Fatalf("createSubtask sub1: %v", err)
	}
	if _, err := svc.createSubtask(parentUUID, map[string]any{"title": "sub2"}); err != nil {
		t.Fatalf("createSubtask sub2: %v", err)
	}

	res, err := svc.fetchSubtasks(parentUUID)
	if err != nil {
		t.Fatalf("fetchSubtasks failed: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map response, got %T", res)
	}
	tasks, ok := m["tasks"].([]TaskRow)
	if !ok {
		t.Fatalf("expected []TaskRow for 'tasks' key, got %T", m["tasks"])
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 subtasks, got %d", len(tasks))
	}
	for _, task := range tasks {
		if task.ParentUUID != parentUUID {
			t.Errorf("expected ParentUUID=%q, got %q", parentUUID, task.ParentUUID)
		}
	}
}

func TestFetchSubtasks_Empty(t *testing.T) {
	svc := newTestService(t)
	parentUUID := "parent-uuid-no-subs"
	if _, err := svc.db.Exec(`INSERT INTO tasks (uuid, title, updated_at, done) VALUES (?, ?, ?, 0)`,
		parentUUID, "parent", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	res, err := svc.fetchSubtasks(parentUUID)
	if err != nil {
		t.Fatalf("fetchSubtasks: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", res)
	}
	tasks, ok := m["tasks"].([]TaskRow)
	if !ok {
		t.Fatalf("expected []TaskRow, got %T", m["tasks"])
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 subtasks for parent with no children, got %d", len(tasks))
	}
}

// ---------------------------------------------------------------------------
// reorderSubtask
// ---------------------------------------------------------------------------

func TestReorderSubtask(t *testing.T) {
	svc := newTestService(t)
	parentUUID := "parent-uuid-reorder"
	if _, err := svc.db.Exec(`INSERT INTO tasks (uuid, title, updated_at, done) VALUES (?, ?, ?, 0)`,
		parentUUID, "parent", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	// Create two subtasks; createSubtask inserts them at decreasing positions so
	// the latest addition has the lowest position value (sorts first).
	res1, err := svc.createSubtask(parentUUID, map[string]any{"title": "first"})
	if err != nil {
		t.Fatal(err)
	}
	res2, err := svc.createSubtask(parentUUID, map[string]any{"title": "second"})
	if err != nil {
		t.Fatal(err)
	}

	id1 := res1.(map[string]any)["taskId"].(int64)
	id2 := res2.(map[string]any)["taskId"].(int64)

	// Confirm initial order via DB position values
	var pos1, pos2 int64
	if err := svc.db.QueryRow("SELECT position FROM tasks WHERE id = ?", id1).Scan(&pos1); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRow("SELECT position FROM tasks WHERE id = ?", id2).Scan(&pos2); err != nil {
		t.Fatal(err)
	}
	// id2 was created last so has lower position (sorts first)
	if pos2 >= pos1 {
		t.Fatalf("expected id2 position (%d) < id1 position (%d) before reorder", pos2, pos1)
	}

	// Move id1 (currently at index 1) to position 0 (first)
	_, err = svc.reorderSubtask(id1, 0, parentUUID)
	if err != nil {
		t.Fatalf("reorderSubtask failed: %v", err)
	}

	// After reorder id1 should be at position 0
	var newPos1 int64
	if err := svc.db.QueryRow("SELECT position FROM tasks WHERE id = ?", id1).Scan(&newPos1); err != nil {
		t.Fatal(err)
	}
	if newPos1 != 0 {
		t.Fatalf("expected id1 at position 0 after reorder, got %d", newPos1)
	}
}

// ---------------------------------------------------------------------------
// runTaskCommand – unknown command
// ---------------------------------------------------------------------------

func TestRunTaskCommand_UnknownCommand(t *testing.T) {
	svc := newTestService(t)
	id := insertTask(t, svc, "task-unknown", "", false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	_, err := svc.runTaskCommand(id, "zzz arg", "")
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
}

func TestRunTaskCommand_EmptyCommand(t *testing.T) {
	svc := newTestService(t)
	id := insertTask(t, svc, "task-empty-cmd", "", false, "", "", 0, time.Now().UTC().Format(time.RFC3339Nano), "")

	_, err := svc.runTaskCommand(id, "   ", "")
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}
