package tasks

import (
	"database/sql"
	"fmt"
	"keyop/x/recurrence"
	"keyop/x/webui"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // sqlite driver
)

// WebUIAssets returns the static assets for the tasks service.
func (svc *Service) WebUIAssets() http.FileSystem {
	return http.Dir("x/tasks/resources")
}

// WebUITab returns the tab configuration for the tasks service.
func (svc *Service) WebUITab() webui.TabInfo {
	// Load HTML from resources directory
	htmlPath := filepath.Join("x/tasks/resources", "tasks.html")
	htmlContent, err := os.ReadFile(htmlPath) // #nosec G304: path is fixed at compile time
	if err != nil {
		// Fallback if file not found
		htmlContent = []byte(`<div id="tasks-container">
<div class="tasks-layout">
  <div class="filter-sidebar">
    <div class="filter-title">Tags</div>
    <div class="tag-list">
      <div class="tag-item active" data-tag="all">all</div>
    </div>
  </div>
  <div class="tasks-content">
    <div id="tasks-list">Loading tasks...</div>
  </div>
</div>
</div>`)
	}

	return webui.TabInfo{
		ID:      "tasks",
		Title:   "Tasks",
		Content: string(htmlContent),
		JSPath:  "/api/assets/tasks/tasks.js",
	}
}

// HandleWebUIAction handles actions from the WebUI.
func (svc *Service) HandleWebUIAction(action string, params map[string]any) (any, error) {
	switch action {
	case "fetch-tasks":
		view := "today" // default view
		if v, ok := params["view"].(string); ok {
			view = v
		}
		return svc.fetchTasks(view)
	case "toggle-task":
		if taskID, ok := params["taskID"].(float64); ok {
			return svc.toggleTask(int64(taskID))
		}
		return nil, fmt.Errorf("invalid taskID")
	case "update-task":
		if taskID, ok := params["taskId"].(float64); ok {
			return svc.updateTask(int64(taskID), params)
		}
		return nil, fmt.Errorf("invalid taskId")
	case "create-task":
		return svc.createTask(params)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// PatternData describes a recurrence pattern for a task (type, interval and optional days).
// Example types: "daily", "weekly", "monthly", "yearly".
type PatternData struct {
	Type       string `json:"type"`
	Interval   int    `json:"interval"`
	DaysOfWeek []int  `json:"daysOfWeek,omitempty"`
	DayOfMonth int    `json:"dayOfMonth,omitempty"`
	Month      int    `json:"month,omitempty"`
}

// TaskRow represents a task for the Web UI.
type TaskRow struct {
	ID               int64        `json:"id"`
	Title            string       `json:"title"`
	Done             bool         `json:"done"`
	Tags             string       `json:"tags"`
	ScheduledAt      string       `json:"scheduledAt"`
	HasScheduledTime bool         `json:"hasScheduledTime"`
	CompletedAt      string       `json:"completedAt"`
	UpdatedAt        string       `json:"updatedAt"`
	Category         string       `json:"category"` // For today view: "today" or "past"; for other views: empty
	Color            string       `json:"color"`    // Hex color code
	Recurring        bool         `json:"recurring"`
	RecurrenceID     int64        `json:"recurrenceId"`
	Pattern          *PatternData `json:"pattern,omitempty"`
}

// parsePatternData normalizes legacy recurrence fields and builds a PatternData.
// Supports legacy recurrence types like "days" with recurrence_days like "sat".
func parsePatternData(recurrenceType string, recurrenceDays string, recurrenceInterval int) *PatternData {
	rt := strings.ToLower(strings.TrimSpace(recurrenceType))
	if rt == "" && recurrenceDays == "" {
		return nil
	}
	// Legacy: "days" with day names -> weekly
	if rt == "days" {
		if strings.IndexFunc(recurrenceDays, func(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }) != -1 {
			rt = "weekly"
		} else {
			rt = "daily"
		}
	}

	interval := recurrenceInterval
	if interval < 1 {
		interval = 1
	}

	p := &PatternData{Type: rt, Interval: interval}

	if rt == "weekly" && recurrenceDays != "" {
		for _, part := range strings.Split(recurrenceDays, ",") {
			part = strings.TrimSpace(strings.ToLower(part))
			if part == "" {
				continue
			}
			if n, err := strconv.Atoi(part); err == nil {
				p.DaysOfWeek = append(p.DaysOfWeek, n)
				continue
			}
			switch {
			case strings.HasPrefix(part, "sun"):
				p.DaysOfWeek = append(p.DaysOfWeek, 0)
			case strings.HasPrefix(part, "mon"):
				p.DaysOfWeek = append(p.DaysOfWeek, 1)
			case strings.HasPrefix(part, "tue"):
				p.DaysOfWeek = append(p.DaysOfWeek, 2)
			case strings.HasPrefix(part, "wed"):
				p.DaysOfWeek = append(p.DaysOfWeek, 3)
			case strings.HasPrefix(part, "thu"):
				p.DaysOfWeek = append(p.DaysOfWeek, 4)
			case strings.HasPrefix(part, "fri"):
				p.DaysOfWeek = append(p.DaysOfWeek, 5)
			case strings.HasPrefix(part, "sat"):
				p.DaysOfWeek = append(p.DaysOfWeek, 6)
			default:
				// ignore unknown
			}
		}
	}

	return p
}

// fetchTasks queries the tasks database for tasks scheduled for a specific logical day or range,
// taking into account the logical day configuration.
// view can be "past", "yesterday", "today", "tomorrow", "future", or "recent".
func (svc *Service) fetchTasks(view string) (any, error) {
	if svc.db == nil {
		return nil, fmt.Errorf("tasks database not available")
	}

	if svc.logicalCalc == nil {
		return nil, fmt.Errorf("logical day calculator not initialized")
	}

	// Handle "recent" view separately - just order by updated_at
	if view == "recent" {
		return svc.fetchRecentTasks()
	}

	// Handle "all" view - return all tasks without filtering by logical day
	if view == "all" {
		return svc.fetchAllTasks()
	}

	// Determine which logical day(s) to display
	var targetDay time.Time
	var isPastOrFuture bool

	switch view {
	case "past":
		isPastOrFuture = true
		targetDay = svc.logicalCalc.DayBeforeYesterday()
	case "yesterday":
		targetDay = svc.logicalCalc.Yesterday()
	case "tomorrow":
		targetDay = svc.logicalCalc.Tomorrow()
	case "future":
		isPastOrFuture = true
		targetDay = svc.logicalCalc.DayAfterTomorrow()
	default:
		targetDay = svc.logicalCalc.Today()
	}

	// Query all tasks that could potentially be today (scheduled in a reasonable range)
	// We'll filter by logical day in Go
	rows, err := svc.db.Query(`
		SELECT id, title, done, scheduled_date, completed_at, tags, scheduled_time, updated_at, color, recurrence, recurrence_days, recurrence_x
		FROM tasks
		ORDER BY done ASC, CASE WHEN done = 0 THEN scheduled_date ELSE completed_at END ASC
	`)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			svc.Deps.MustGetLogger().Warn("tasks: failed to close rows", "error", err)
		}
	}()

	var tasks []TaskRow
	tagCounts := make(map[string]int)

	for rows.Next() {
		var task TaskRow
		var scheduledAt, completedAt, updatedAt sql.NullTime
		var hasScheduledTime bool
		var recurrenceType string
		var recurrenceDays string
		var recurrenceInterval int

		if err := rows.Scan(
			&task.ID, &task.Title, &task.Done, &scheduledAt, &completedAt,
			&task.Tags, &hasScheduledTime, &updatedAt, &task.Color, &recurrenceType, &recurrenceDays, &recurrenceInterval,
		); err != nil {
			continue
		}

		// Determine if task is recurring and build pattern
		task.Recurring = recurrenceType != ""
		task.HasScheduledTime = hasScheduledTime
		if task.Recurring {
			pattern := parsePatternData(recurrenceType, recurrenceDays, recurrenceInterval)
			if pattern != nil {
				task.Pattern = pattern
			}
		}

		// Check if this task should be included based on logical day
		shouldInclude := false

		if !task.Done && scheduledAt.Valid {
			logicalDay := svc.logicalCalc.GetLogicalDay(scheduledAt.Time, hasScheduledTime)

			if isPastOrFuture {
				// For past/future, check if task is before/after the cutoff
				switch view {
				case "past":
					// Show tasks scheduled before yesterday
					if logicalDay.Before(svc.logicalCalc.Yesterday()) {
						shouldInclude = true
						task.ScheduledAt = scheduledAt.Time.Format(time.RFC3339Nano)
					}
				case "future":
					// Show tasks scheduled after tomorrow
					if logicalDay.After(svc.logicalCalc.Tomorrow()) {
						shouldInclude = true
						task.ScheduledAt = scheduledAt.Time.Format(time.RFC3339Nano)
					}
				}
			} else {
				// For specific days, check exact match or include past tasks for "today" view
				if logicalDay.Year() == targetDay.Year() &&
					logicalDay.Month() == targetDay.Month() &&
					logicalDay.Day() == targetDay.Day() {
					shouldInclude = true
					task.ScheduledAt = scheduledAt.Time.Format(time.RFC3339Nano)
					task.Category = "today"
				} else if view == "today" && logicalDay.Before(targetDay) {
					// For today view, also include past incomplete tasks
					shouldInclude = true
					task.ScheduledAt = scheduledAt.Time.Format(time.RFC3339Nano)
					task.Category = "past"
				}
			}
		}

		// Show completed tasks if viewing today or yesterday
		if task.Done && completedAt.Valid && (view == "today" || view == "yesterday") {
			// For completed tasks, check if they were completed on the target calendar day
			completedDate := completedAt.Time.In(time.Local)
			completedDateOnly := time.Date(completedDate.Year(), completedDate.Month(), completedDate.Day(), 0, 0, 0, 0, time.Local)

			if completedDateOnly.Year() == targetDay.Year() &&
				completedDateOnly.Month() == targetDay.Month() &&
				completedDateOnly.Day() == targetDay.Day() {
				shouldInclude = true
				task.CompletedAt = completedAt.Time.Format(time.RFC3339Nano)
			}
		}

		if !shouldInclude {
			continue
		}

		// Set UpdatedAt for display
		if updatedAt.Valid {
			task.UpdatedAt = updatedAt.Time.Format(time.RFC3339Nano)
		}

		tasks = append(tasks, task)

		// Count tags
		if task.Tags != "" {
			tagList := strings.Split(task.Tags, ",")
			for _, tag := range tagList {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					tagCounts[tag]++
				}
			}
		} else {
			// Tasks with no tags go under "untagged"
			tagCounts["untagged"]++
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return map[string]any{"tasks": tasks, "tagCounts": tagCounts}, nil
}

// fetchRecentTasks returns tasks ordered by most recently modified (most recent first).
func (svc *Service) fetchRecentTasks() (any, error) {
	rows, err := svc.db.Query(`
		SELECT id, title, done, scheduled_date, completed_at, tags, scheduled_time, updated_at, color, recurrence, recurrence_days, recurrence_x
		FROM tasks
		ORDER BY updated_at DESC NULLS LAST
	`)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			svc.Deps.MustGetLogger().Warn("tasks: failed to close rows", "error", err)
		}
	}()

	var tasks []TaskRow
	tagCounts := make(map[string]int)

	for rows.Next() {
		var task TaskRow
		var scheduledAt, completedAt, updatedAt sql.NullTime
		var hasScheduledTime bool
		var recurrenceType string
		var recurrenceDays string
		var recurrenceInterval int

		if err := rows.Scan(
			&task.ID, &task.Title, &task.Done, &scheduledAt, &completedAt,
			&task.Tags, &hasScheduledTime, &updatedAt, &task.Color, &recurrenceType, &recurrenceDays, &recurrenceInterval,
		); err != nil {
			continue
		}

		// Determine if task is recurring and build pattern
		task.Recurring = recurrenceType != ""
		task.HasScheduledTime = hasScheduledTime
		if task.Recurring {
			pattern := parsePatternData(recurrenceType, recurrenceDays, recurrenceInterval)
			if pattern != nil {
				task.Pattern = pattern
			}

		}

		// Set date fields for display
		if scheduledAt.Valid {
			task.ScheduledAt = scheduledAt.Time.Format(time.RFC3339Nano)
		}
		if completedAt.Valid {
			task.CompletedAt = completedAt.Time.Format(time.RFC3339Nano)
		}
		if updatedAt.Valid {
			task.UpdatedAt = updatedAt.Time.Format(time.RFC3339Nano)
		}

		tasks = append(tasks, task)

		// Count tags
		if task.Tags != "" {
			tagList := strings.Split(task.Tags, ",")
			for _, tag := range tagList {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					tagCounts[tag]++
				}
			}
		} else {
			// Tasks with no tags go under "untagged"
			tagCounts["untagged"]++
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return map[string]any{"tasks": tasks, "tagCounts": tagCounts}, nil
}

// fetchAllTasks returns all tasks in the database without filtering by logical day.
func (svc *Service) fetchAllTasks() (any, error) {
	rows, err := svc.db.Query(`
		SELECT id, title, done, scheduled_date, completed_at, tags, scheduled_time, updated_at, color, recurrence, recurrence_days, recurrence_x
		FROM tasks
		ORDER BY done ASC, CASE WHEN done = 0 THEN scheduled_date ELSE completed_at END ASC
	`)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			svc.Deps.MustGetLogger().Warn("tasks: failed to close rows", "error", err)
		}
	}()

	var tasks []TaskRow
	tagCounts := make(map[string]int)

	for rows.Next() {
		var task TaskRow
		var scheduledAt, completedAt, updatedAt sql.NullTime
		var hasScheduledTime bool
		var recurrenceType string
		var recurrenceDays string
		var recurrenceInterval int

		if err := rows.Scan(
			&task.ID, &task.Title, &task.Done, &scheduledAt, &completedAt,
			&task.Tags, &hasScheduledTime, &updatedAt, &task.Color, &recurrenceType, &recurrenceDays, &recurrenceInterval,
		); err != nil {
			continue
		}

		// Determine if task is recurring and build pattern
		task.Recurring = recurrenceType != ""
		task.HasScheduledTime = hasScheduledTime
		if task.Recurring {
			pattern := parsePatternData(recurrenceType, recurrenceDays, recurrenceInterval)
			if pattern != nil {
				task.Pattern = pattern
			}

		}

		// Set date fields for display
		if scheduledAt.Valid {
			task.ScheduledAt = scheduledAt.Time.Format(time.RFC3339Nano)
		}
		if completedAt.Valid {
			task.CompletedAt = completedAt.Time.Format(time.RFC3339Nano)
		}
		if updatedAt.Valid {
			task.UpdatedAt = updatedAt.Time.Format(time.RFC3339Nano)
		}

		tasks = append(tasks, task)

		// Count tags
		if task.Tags != "" {
			tagList := strings.Split(task.Tags, ",")
			for _, tag := range tagList {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					tagCounts[tag]++
				}
			}
		} else {
			// Tasks with no tags go under "untagged"
			tagCounts["untagged"]++
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return map[string]any{"tasks": tasks, "tagCounts": tagCounts}, nil
}

// createTask creates a new task scheduled for today.
func (svc *Service) createTask(params map[string]any) (any, error) {
	if svc.db == nil {
		return nil, fmt.Errorf("tasks database not available")
	}

	title, _ := params["title"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("task title cannot be empty")
	}

	// Determine scheduled date and flag
	scheduledDate := time.Now().UTC().Format(time.RFC3339Nano)
	hasScheduledTimeFlag := 0
	if scheduledAt, ok := params["scheduledAt"].(string); ok && scheduledAt != "" {
		t, err := time.Parse(time.RFC3339, scheduledAt)
		if err == nil {
			scheduledDate = t.UTC().Format(time.RFC3339Nano)
		}
	} else {
		// Default to midnight UTC for today (legacy fallback)
		today := time.Now().Format("2006-01-02")
		t, _ := time.ParseInLocation("2006-01-02", today, time.UTC)
		scheduledDate = t.Format(time.RFC3339Nano)
	}

	if val, ok := params["hasScheduledTime"].(bool); ok && val {
		hasScheduledTimeFlag = 1
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	result, err := svc.db.Exec(
		"INSERT INTO tasks (title, scheduled_date, created_at, updated_at, done, scheduled_time) VALUES (?, ?, ?, ?, 0, ?)",
		title, scheduledDate, now, now, hasScheduledTimeFlag,
	)
	if err != nil {
		return nil, err
	}

	taskID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return map[string]any{"status": "ok", "taskId": taskID}, nil
}

// toggleTask marks a task as done or undone.
func (svc *Service) toggleTask(taskID int64) (any, error) {
	if svc.db == nil {
		return nil, fmt.Errorf("tasks database not available")
	}

	// Get current done status and recurrence metadata
	var done bool
	var title sql.NullString
	var tags sql.NullString
	var color sql.NullString
	var scheduledDate sql.NullTime
	var scheduledTime sql.NullBool
	var recurrenceType sql.NullString
	var recurrenceDays sql.NullString
	var recurrenceInterval sql.NullInt64

	err := svc.db.QueryRow(`
		SELECT done, title, tags, color, scheduled_date, scheduled_time, recurrence, recurrence_days, recurrence_x
		FROM tasks WHERE id = ?`, taskID).Scan(&done, &title, &tags, &color, &scheduledDate, &scheduledTime, &recurrenceType, &recurrenceDays, &recurrenceInterval)
	if err != nil {
		return nil, err
	}

	// Update: toggle done status and set/clear completed_at
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var newDone bool
	if !done {
		// Marking as done
		newDone = true
		_, err = svc.db.Exec(
			"UPDATE tasks SET done = ?, completed_at = ?, updated_at = ? WHERE id = ?",
			newDone, now, now, taskID,
		)
	} else {
		// Marking as not done
		newDone = false
		_, err = svc.db.Exec(
			"UPDATE tasks SET done = ?, completed_at = NULL, updated_at = ? WHERE id = ?",
			newDone, now, taskID,
		)
	}

	if err != nil {
		return nil, err
	}

	// If we just marked it done and it has a recurrence pattern, create the next instance
	if !done && recurrenceType.Valid && recurrenceType.String != "" {
		interval := 0
		if recurrenceInterval.Valid {
			interval = int(recurrenceInterval.Int64)
		}

		patternData := parsePatternData(recurrenceType.String, recurrenceDays.String, interval)
		if patternData != nil {
			// Build recurrence.Pattern
			rp := &recurrence.Pattern{
				Type:       patternData.Type,
				Interval:   patternData.Interval,
				DayOfMonth: patternData.DayOfMonth,
				Month:      time.Month(patternData.Month),
			}
			for _, d := range patternData.DaysOfWeek {
				rp.DaysOfWeek = append(rp.DaysOfWeek, time.Weekday(d))
			}

			// Determine base time for next calculation
			base := time.Now().UTC()
			if scheduledDate.Valid {
				base = scheduledDate.Time.UTC()
			}

			next := rp.Next(base).UTC()
			if !next.IsZero() {
				var newScheduled string
				scheduledFlag := 0
				if scheduledTime.Valid && scheduledTime.Bool {
					newScheduled = next.Format(time.RFC3339Nano)
					scheduledFlag = 1
				} else {
					// Date only: ensure it's midnight local time, but stored as UTC
					// We use the logical day calculator's location if available, otherwise time.Local
					loc := time.Local
					if svc.logicalCalc != nil && svc.logicalCalc.Location() != nil {
						loc = svc.logicalCalc.Location()
					}
					nextMidnight := time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, loc).UTC()
					newScheduled = nextMidnight.Format(time.RFC3339Nano)
					scheduledFlag = 0
				}

				_, err = svc.db.Exec(`
					INSERT INTO tasks (title, tags, color, scheduled_date, scheduled_time, recurrence, recurrence_days, recurrence_x, created_at, updated_at, done)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
				`, title.String, tags.String, color.String, newScheduled, scheduledFlag, recurrenceType.String, recurrenceDays.String, interval, now, now)
				if err != nil {
					svc.Deps.MustGetLogger().Warn("tasks: failed to create next recurring task", "error", err)
				}
			}
		}
	}

	return map[string]any{"status": "ok", "done": newDone}, nil
}

// updateTask updates task details (title, tags, color, recurrence).
func (svc *Service) updateTask(taskID int64, params map[string]any) (any, error) {
	if svc.db == nil {
		return nil, fmt.Errorf("tasks database not available")
	}

	title, _ := params["title"].(string)
	tags, _ := params["tags"].(string)
	color, _ := params["color"].(string)
	scheduledDateStr, _ := params["scheduledDate"].(string)
	scheduledTimeStr, _ := params["scheduledTime"].(string)

	// Parse pattern if provided (frontend sends a `pattern` object)
	var patternType string
	var patternInterval int
	var patternDaysCSV string
	if p, ok := params["pattern"].(map[string]any); ok && p != nil {
		if t, ok := p["type"].(string); ok {
			patternType = t
		}
		if ivf, ok := p["interval"].(float64); ok {
			patternInterval = int(ivf)
		} else if iv, ok := p["interval"].(int); ok {
			patternInterval = iv
		}
		// daysOfWeek may be an array of numbers
		if days, ok := p["daysOfWeek"].([]any); ok {
			parts := []string{}
			for _, d := range days {
				switch v := d.(type) {
				case float64:
					parts = append(parts, strconv.Itoa(int(v)))
				case int:
					parts = append(parts, strconv.Itoa(v))
				case string:
					parts = append(parts, strings.TrimSpace(v))
				}
			}
			patternDaysCSV = strings.Join(parts, ",")
		} else if s, ok := p["daysOfWeek"].(string); ok {
			patternDaysCSV = s
		}
	}

	// Fallback to legacy recurrence fields if pattern wasn't provided
	if patternType == "" {
		patternType, _ = params["recurrenceType"].(string)
		if patternInterval == 0 {
			if ivf, ok := params["recurrenceInterval"].(float64); ok {
				patternInterval = int(ivf)
			} else if iv, ok := params["recurrenceInterval"].(int); ok {
				patternInterval = iv
			}
		}
		if pd, ok := params["recurrence_days"].(string); ok {
			patternDaysCSV = pd
		} else if pd, ok := params["recurrenceDays"].(string); ok {
			patternDaysCSV = pd
		}
	}

	// Build update query
	now := time.Now().UTC().Format(time.RFC3339Nano)
	updateFields := []string{"updated_at = ?"}
	updateArgs := []any{now}

	if title != "" {
		updateFields = append(updateFields, "title = ?")
		updateArgs = append(updateArgs, title)
	}

	if tags != "" {
		updateFields = append(updateFields, "tags = ?")
		updateArgs = append(updateArgs, tags)
	}

	// Handle color
	if color != "" {
		updateFields = append(updateFields, "color = ?")
		updateArgs = append(updateArgs, color)
	}

	// Handle recurrence: check if `pattern` key was provided at all so we can clear recurrence when user sets it to null
	if _, hasPatternKey := params["pattern"]; hasPatternKey {
		// User provided pattern field explicitly
		if params["pattern"] == nil {
			// Clear recurrence
			updateFields = append(updateFields, "recurrence = ?", "recurrence_x = ?", "recurrence_days = ?")
			updateArgs = append(updateArgs, "", 0, "")
		} else if patternType != "" {
			// Persist provided pattern
			if patternInterval < 1 {
				patternInterval = 1
			}
			updateFields = append(updateFields, "recurrence = ?", "recurrence_x = ?", "recurrence_days = ?")
			updateArgs = append(updateArgs, patternType, patternInterval, patternDaysCSV)
		}
	} else {
		// No pattern key provided; fallback to legacy recurrenceType param handling (if present)
		if rt, ok := params["recurrenceType"].(string); ok && rt != "" {
			interval := 1
			if ivf, ok := params["recurrenceInterval"].(float64); ok {
				interval = int(ivf)
			}
			if interval < 1 {
				interval = 1
			}
			updateFields = append(updateFields, "recurrence = ?", "recurrence_x = ?")
			updateArgs = append(updateArgs, rt, interval)
		}
	}

	// Handle scheduledAt ISO string if provided
	if scheduledAt, ok := params["scheduledAt"].(string); ok && scheduledAt != "" {
		t, err := time.Parse(time.RFC3339, scheduledAt)
		if err == nil {
			updateFields = append(updateFields, "scheduled_date = ?")
			updateArgs = append(updateArgs, t.UTC().Format(time.RFC3339Nano))
		}
	} else if scheduledDateStr != "" {
		var t time.Time
		var err error
		if scheduledTimeStr != "" {
			// Parse as UTC wall clock time (legacy fallback).
			t, err = time.ParseInLocation("2006-01-02 15:04", scheduledDateStr+" "+scheduledTimeStr, time.UTC)
		} else {
			// Date-only tasks (legacy fallback).
			t, err = time.ParseInLocation("2006-01-02", scheduledDateStr, time.UTC)
		}

		if err == nil {
			updateFields = append(updateFields, "scheduled_date = ?")
			updateArgs = append(updateArgs, t.Format(time.RFC3339Nano))
		}
	}

	// Handle hasScheduledTime
	if hasTime, ok := params["hasScheduledTime"].(bool); ok {
		updateFields = append(updateFields, "scheduled_time = ?")
		updateArgs = append(updateArgs, hasTime)
	} else if hasTimeAny, ok := params["hasScheduledTime"]; ok {
		// Handle potential float64/int if JSON parsing didn't convert to bool
		if hasTimeF, ok := hasTimeAny.(float64); ok {
			updateFields = append(updateFields, "scheduled_time = ?")
			updateArgs = append(updateArgs, hasTimeF != 0)
		}
	}

	updateArgs = append(updateArgs, taskID)

	query := fmt.Sprintf("UPDATE tasks SET %s WHERE id = ?", strings.Join(updateFields, ", ")) // #nosec G201 - field names are hardcoded
	_, err := svc.db.Exec(query, updateArgs...)
	if err != nil {
		return nil, err
	}

	return map[string]any{"status": "ok"}, nil
}
