package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "/Users/wu/.keyop/sqlite/tasks.sql")
	if err != nil {
		log.Fatalf("open err: %v", err)
	}
	defer db.Close()

	selectCols := "id, title, done, scheduled_date, completed_at, tags, scheduled_time, updated_at, color, recurrence, recurrence_days, recurrence_x, uuid, subtask_parent_uuid"
	query := fmt.Sprintf("SELECT %s FROM tasks ORDER BY (updated_at IS NULL), updated_at DESC LIMIT 20", selectCols)
	fmt.Println("Query:", query)
	rows, err := db.Query(query)
	if err != nil {
		log.Fatalf("query err: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
		var id int64
		var title string
		var done bool
		var scheduledAtStr, completedAtStr, updatedAtStr sql.NullString
		var tags string
		var hasScheduledTime bool
		var color string
		var recurrenceType string
		var recurrenceDays string
		var recurrenceInterval int
		var uuid string
		var parent string

		scanTargets := []interface{}{&id, &title, &done, &scheduledAtStr, &completedAtStr, &tags, &hasScheduledTime, &updatedAtStr, &color, &recurrenceType, &recurrenceDays, &recurrenceInterval, &uuid, &parent}
		if err := rows.Scan(scanTargets...); err != nil {
			fmt.Printf("row %d scan error: %v\n", count, err)
			continue
		}
		fmt.Printf("row %d: id=%d title=%q done=%v scheduled=%q updated=%q tags=%q color=%q uuid=%q parent=%q rec=%q days=%q ix=%d\n", count, id, title, done, scheduledAtStr.String, updatedAtStr.String, tags, color, uuid, parent, recurrenceType, recurrenceDays, recurrenceInterval)
	}
	if err := rows.Err(); err != nil {
		fmt.Printf("rows err: %v\n", err)
	}
	fmt.Printf("processed rows=%d\n", count)
}
