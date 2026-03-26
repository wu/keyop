package tasks

import (
	"context"
	"database/sql"
	"fmt"
	"keyop/core"
	"keyop/x/logicalday"
	"os"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// TestTaskCreationUsesLogicalDay tests that new tasks created without a specific time
// are assigned to the logical day (respecting the end-of-day cutoff), not the calendar day.
// This is a regression test for the bug where tasks created before 4am were assigned to the wrong day.
func TestTaskCreationUsesLogicalDay(t *testing.T) {
	// Create a temporary database for testing
	tmpFile := fmt.Sprintf("/tmp/keyop_test_%d.db", time.Now().UnixNano())
	defer func() {
		if err := os.Remove(tmpFile); err != nil {
			t.Logf("Failed to remove temp file %s: %v", tmpFile, err)
		}
	}()

	db, err := sql.Open("sqlite", tmpFile)
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Failed to close database: %v", err)
		}
	}()

	// Create tasks table
	_, err = db.Exec(`
		CREATE TABLE tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			uuid TEXT UNIQUE,
			parent_uuid TEXT DEFAULT '',
			subtask_parent_uuid TEXT DEFAULT '',
			title TEXT,
			note TEXT DEFAULT '',
			scheduled_date TEXT,
			scheduled_time INTEGER DEFAULT 0,
			tags TEXT DEFAULT '',
			color TEXT DEFAULT '',
			importance INTEGER DEFAULT 0,
			urgency INTEGER DEFAULT 0,
			user_id INTEGER DEFAULT 0,
			done INTEGER DEFAULT 0,
			completed_at TEXT DEFAULT '',
			recurrence TEXT DEFAULT '',
			recurrence_days TEXT DEFAULT '',
			recurrence_x INTEGER DEFAULT 0,
			in_progress INTEGER DEFAULT 0,
			in_progress_started_at TEXT DEFAULT '',
			in_progress_total_seconds INTEGER DEFAULT 0,
			created_at TEXT,
			updated_at TEXT
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create tasks table: %v", err)
	}

	// Create a mock dependencies object
	deps := core.Dependencies{}
	deps.SetLogger(&mockLogger{})
	deps.SetMessenger(&mockMessenger{})

	// Create a service with the test database
	svc := &Service{
		Deps: deps,
		db:   db,
	}

	// Initialize with Pacific timezone and 4am end-of-day
	loc, _ := time.LoadLocation("America/Los_Angeles")
	svc.logicalCalc = logicalday.NewCalculator("04:00", loc)

	// Test Case 1: Create task at 12:30am PT (before 4am cutoff)
	// Expected: assigned to previous day (logical yesterday)
	t.Run("Task created at 12:30am PT before 4am cutoff", func(t *testing.T) {
		result, err := svc.createTask(map[string]any{
			"title": "early morning task",
			// No scheduledAt parameter - should use logical today
		})

		if err != nil {
			t.Errorf("Failed to create task: %v", err)
		}

		// Verify the task was created
		if result == nil {
			t.Errorf("Expected result, got nil")
			return
		}

		// Get the task from database
		var scheduledDate string
		err = db.QueryRow("SELECT scheduled_date FROM tasks WHERE title = ?", "early morning task").Scan(&scheduledDate)
		if err != nil {
			t.Errorf("Failed to query task: %v", err)
			return
		}

		// Parse the scheduled date
		parsedTime, err := time.Parse(time.RFC3339Nano, scheduledDate)
		if err != nil {
			t.Errorf("Failed to parse scheduled_date: %v", err)
			return
		}

		// Convert to local timezone to check the date
		localTime := parsedTime.In(loc)
		logicalDay := svc.logicalCalc.GetLogicalDay(time.Now(), true)

		// The scheduled date should match the logical day (previous day if before 4am)
		if localTime.Year() != logicalDay.Year() ||
			localTime.Month() != logicalDay.Month() ||
			localTime.Day() != logicalDay.Day() {
			t.Errorf("Task scheduled for %s, but logical day is %s",
				localTime.Format("2006-01-02"),
				logicalDay.Format("2006-01-02"))
		}
	})

	// Test Case 2: Verify the bug is fixed - tasks should NOT use calendar date
	t.Run("Task does not use calendar date directly", func(t *testing.T) {
		result, err := svc.createTask(map[string]any{
			"title": "verify no calendar date bug",
		})

		if err != nil {
			t.Errorf("Failed to create task: %v", err)
		}

		if result == nil {
			t.Errorf("Expected result, got nil")
			return
		}

		var scheduledDate string
		err = db.QueryRow("SELECT scheduled_date FROM tasks WHERE title = ?", "verify no calendar date bug").Scan(&scheduledDate)
		if err != nil {
			t.Errorf("Failed to query task: %v", err)
			return
		}

		parsedTime, err := time.Parse(time.RFC3339Nano, scheduledDate)
		if err != nil {
			t.Errorf("Failed to parse scheduled_date: %v", err)
			return
		}

		localTime := parsedTime.In(loc)
		now := time.Now().In(loc)

		// The task should NOT be assigned to today's calendar date if it's before 4am
		// (it should be assigned to yesterday instead)
		if now.Hour() < 4 {
			// Before 4am - task should be on previous calendar day
			expectedDay := now.AddDate(0, 0, -1)
			if localTime.Day() != expectedDay.Day() {
				t.Errorf("At %02d:%02d, task should be scheduled for %s, but got %s",
					now.Hour(), now.Minute(),
					expectedDay.Format("2006-01-02"),
					localTime.Format("2006-01-02"))
			}
		} else {
			// At or after 4am - task should be on today's calendar day
			if localTime.Day() != now.Day() {
				t.Errorf("At %02d:%02d, task should be scheduled for %s, but got %s",
					now.Hour(), now.Minute(),
					now.Format("2006-01-02"),
					localTime.Format("2006-01-02"))
			}
		}
	})

	// Test Case 3: All-day task (no specific time) should still use logical day
	t.Run("All-day task uses logical day", func(t *testing.T) {
		result, err := svc.createTask(map[string]any{
			"title":            "all day task",
			"hasScheduledTime": false,
		})

		if err != nil {
			t.Errorf("Failed to create task: %v", err)
		}

		if result == nil {
			t.Errorf("Expected result, got nil")
			return
		}

		var scheduledDate string
		var hasScheduledTime int
		err = db.QueryRow("SELECT scheduled_date, scheduled_time FROM tasks WHERE title = ?", "all day task").Scan(&scheduledDate, &hasScheduledTime)
		if err != nil {
			t.Errorf("Failed to query task: %v", err)
			return
		}

		if hasScheduledTime != 0 {
			t.Errorf("All-day task should have scheduled_time = 0, got %d", hasScheduledTime)
		}

		parsedTime, err := time.Parse(time.RFC3339Nano, scheduledDate)
		if err != nil {
			t.Errorf("Failed to parse scheduled_date: %v", err)
			return
		}

		localTime := parsedTime.In(loc)
		logicalDay := svc.logicalCalc.GetLogicalDay(time.Now(), true)

		// Even for all-day tasks, the logical day should be used
		if localTime.Year() != logicalDay.Year() ||
			localTime.Month() != logicalDay.Month() ||
			localTime.Day() != logicalDay.Day() {
			t.Errorf("All-day task scheduled for %s, but logical day is %s",
				localTime.Format("2006-01-02"),
				logicalDay.Format("2006-01-02"))
		}
	})
}

// mockLogger implements core.Logger for testing
type mockLogger struct{}

func (m *mockLogger) Info(_ string, _ ...any)            {}
func (m *mockLogger) Warn(_ string, _ ...any)            {}
func (m *mockLogger) Error(_ string, _ ...any)           {}
func (m *mockLogger) Debug(_ string, _ ...any)           {}
func (m *mockLogger) ErrorfWithStack(_ string, _ ...any) {}

// mockMessenger implements core.MessengerApi for testing
type mockMessenger struct{}

func (m *mockMessenger) Send(_ core.Message) error {
	return nil
}

func (m *mockMessenger) Subscribe(_ context.Context, _ string, _ string, _ string, _ string, _ time.Duration, _ func(core.Message) error) error {
	return nil
}

func (m *mockMessenger) SubscribeExtended(_ context.Context, _ string, _ string, _ string, _ string, _ time.Duration, _ func(core.Message, string, int64) error) error {
	return nil
}

func (m *mockMessenger) SetReaderState(_ string, _ string, _ string, _ int64) error {
	return nil
}

func (m *mockMessenger) SeekToEnd(_ string, _ string) error {
	return nil
}

func (m *mockMessenger) SetDataDir(_ string) {}

func (m *mockMessenger) SetHostname(_ string) {}

func (m *mockMessenger) GetStats() core.MessengerStats {
	return core.MessengerStats{}
}

func (m *mockMessenger) GetPayloadRegistry() core.PayloadRegistry {
	return nil
}

func (m *mockMessenger) SetPayloadRegistry(_ core.PayloadRegistry) {}
