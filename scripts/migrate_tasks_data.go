package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func main() {
	dbPath := os.Getenv("TASKS_DB_PATH")
	if dbPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("Failed to get home dir: %v", err)
		}
		dbPath = filepath.Join(home, ".keyop/sqlite/tasks.sql")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT id, scheduled_date, scheduled_time, created_at, updated_at, completed_at FROM tasks")
	if err != nil {
		log.Fatalf("Failed to query tasks: %v", err)
	}
	defer rows.Close()

	type taskUpdate struct {
		id               int64
		scheduledDate    string
		hasScheduledTime bool
		createdAt        string
		updatedAt        string
		completedAt      *string
	}

	var updates []taskUpdate

	for rows.Next() {
		var id int64
		var schedDateRaw, createdAtRaw, updatedAtRaw, completedAtRaw sql.NullString
		var schedTime int

		if err := rows.Scan(&id, &schedDateRaw, &schedTime, &createdAtRaw, &updatedAtRaw, &completedAtRaw); err != nil {
			log.Printf("Error scanning row %d: %v", id, err)
			continue
		}

		update := taskUpdate{id: id}

		// 1. Standardize scheduled_date and hasScheduledTime
		if schedDateRaw.Valid && schedDateRaw.String != "" {
			t, hasTime := parseFlexibleDate(schedDateRaw.String)
			if hasTime {
				update.scheduledDate = t.Format(time.RFC3339Nano)
				update.hasScheduledTime = true
			} else {
				// For date-only, ensure it's midnight in local time but stored in RFC3339Nano
				localMidnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
				update.scheduledDate = localMidnight.Format(time.RFC3339Nano)
				update.hasScheduledTime = false
			}
		} else {
			update.scheduledDate = ""
			update.hasScheduledTime = false
		}

		// If schedTime was already 1, keep it 1 if we couldn't determine otherwise
		if schedTime == 1 && !update.hasScheduledTime && schedDateRaw.Valid && schedDateRaw.String != "" {
			update.hasScheduledTime = true
		}

		// 2. Standardize created_at, updated_at, completed_at to RFC3339Nano (UTC)
		update.createdAt = standardizeTimestamp(createdAtRaw.String)
		update.updatedAt = standardizeTimestamp(updatedAtRaw.String)
		if completedAtRaw.Valid && completedAtRaw.String != "" {
			ts := standardizeTimestamp(completedAtRaw.String)
			update.completedAt = &ts
		}

		updates = append(updates, update)
	}

	tx, err := db.Begin()
	if err != nil {
		log.Fatalf("Failed to begin transaction: %v", err)
	}

	stmt, err := tx.Prepare("UPDATE tasks SET scheduled_date = ?, scheduled_time = ?, created_at = ?, updated_at = ?, completed_at = ? WHERE id = ?")
	if err != nil {
		log.Fatalf("Failed to prepare statement: %v", err)
	}
	defer stmt.Close()

	for _, u := range updates {
		var compAt interface{}
		if u.completedAt != nil {
			compAt = *u.completedAt
		}
		_, err := stmt.Exec(u.scheduledDate, u.hasScheduledTime, u.createdAt, u.updatedAt, compAt, u.id)
		if err != nil {
			log.Printf("Failed to update task %d: %v", u.id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Fatalf("Failed to commit transaction: %v", err)
	}

	fmt.Printf("Successfully migrated %d tasks\n", len(updates))
}

func parseFlexibleDate(s string) (time.Time, bool) {
	layouts := []struct {
		layout  string
		hasTime bool
	}{
		{time.RFC3339Nano, true},
		{time.RFC3339, true},
		{"2006-01-02 15:04:05.999999", true},
		{"2006-01-02 15:04:05", true},
		{"2006-01-02 15:04:05+00:00", true},
		{"2006-01-02", false},
	}

	for _, l := range layouts {
		if t, err := time.Parse(l.layout, s); err == nil {
			return t, l.hasTime
		}
	}

	// Fallback for formats like "2026-02-28 03:00:00+00:00" which might fail time.RFC3339 if space instead of T
	if strings.Contains(s, " ") && strings.Contains(s, "+") {
		if t, err := time.Parse("2006-01-02 15:04:05-07:00", s); err == nil {
			return t, true
		}
	}

	return time.Time{}, false
}

func standardizeTimestamp(s string) string {
	if s == "" {
		return time.Now().UTC().Format(time.RFC3339Nano)
	}

	// Try common formats
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.999",
	}

	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC().Format(time.RFC3339Nano)
		}
	}

	return s // Return as is if we can't parse it
}
