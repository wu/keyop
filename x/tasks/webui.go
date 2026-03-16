package tasks

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"keyop/core"
	"keyop/x/recurrence"
	"keyop/x/webui"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
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
		res, err := svc.fetchTasks(view)
		if err != nil {
			return nil, err
		}
		if m, ok := res.(map[string]any); ok {
			if tasksIface, ok := m["tasks"]; ok {
				if tasksSlice, ok := tasksIface.([]TaskRow); ok {
					svc.annotateSubtaskInfo(tasksSlice)
					return m, nil
				}
			}
		}
		return res, nil
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
	case "delete-task":
		if taskID, ok := params["taskId"].(float64); ok {
			return svc.deleteTask(int64(taskID))
		}
		return nil, fmt.Errorf("invalid taskId")
	case "create-subtask":
		if parentUUID, ok := params["parentUuid"].(string); ok {
			return svc.createSubtask(parentUUID, params)
		}
		return nil, fmt.Errorf("invalid parentUuid")
	case "fetch-subtasks":
		if parentUUID, ok := params["parentUuid"].(string); ok {
			return svc.fetchSubtasks(parentUUID)
		}
		return nil, fmt.Errorf("invalid parentUuid")
	case "reorder-subtask":
		if taskID, ok := params["taskId"].(float64); ok {
			var newPosition int64
			if pos, ok := params["newPosition"].(float64); ok {
				newPosition = int64(pos)
			}
			var parentUUID string
			if uuid, ok := params["parentUuid"].(string); ok {
				parentUUID = uuid
			}
			return svc.reorderSubtask(int64(taskID), newPosition, parentUUID)
		}
		return nil, fmt.Errorf("invalid reorder params")
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
	UUID             string       `json:"uuid,omitempty"`
	ParentUUID       string       `json:"parentUuid,omitempty"`
	Title            string       `json:"title"`
	Done             bool         `json:"done"`
	Tags             string       `json:"tags"`
	HasScheduledTime bool         `json:"hasScheduledTime"`
	ScheduledAt      string       `json:"scheduledAt"`
	CompletedAt      string       `json:"completedAt"`
	UpdatedAt        string       `json:"updatedAt"`
	Category         string       `json:"category"` // For today view: "today" or "past"; for other views: empty
	Color            string       `json:"color"`    // Hex color code
	Recurring        bool         `json:"recurring"`
	HasSubtasks      bool         `json:"hasSubtasks"`
	RecurrenceID     int64        `json:"recurrenceId"`
	Pattern          *PatternData `json:"pattern,omitempty"`
	SortOrder        int64        `json:"sortOrder,omitempty"` // Manual sort order for subtasks
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

	// First, calculate what "today" is in logical day terms
	currentLogicalDayStart := svc.logicalCalc.LogicalTodayStart()
	logicalToday := time.Date(currentLogicalDayStart.Year(), currentLogicalDayStart.Month(), currentLogicalDayStart.Day(), 0, 0, 0, 0, svc.logicalCalc.Location())

	switch view {
	case "past":
		isPastOrFuture = true
		// Show tasks scheduled two or more days before logical today
		targetDay = logicalToday.AddDate(0, 0, -2)
	case "yesterday":
		// Show tasks from the logical day before today
		targetDay = logicalToday.AddDate(0, 0, -1)
	case "today":
		// Show tasks from the current logical day
		targetDay = logicalToday
	case "tomorrow":
		// Show tasks from the logical day after today
		targetDay = logicalToday.AddDate(0, 0, 1)
	case "future":
		isPastOrFuture = true
		// Show tasks scheduled two or more days after logical today
		targetDay = logicalToday.AddDate(0, 0, 2)
	default:
		// Default to "today"
		targetDay = logicalToday
	}

	// Determine optional columns (uuid, subtask_parent_uuid) and build select dynamically
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
	}

	selectCols := "id, title, done, scheduled_date, completed_at, tags, scheduled_time, updated_at, color, recurrence, recurrence_days, recurrence_x"
	if cols["uuid"] {
		selectCols += ", uuid"
	}
	if cols["subtask_parent_uuid"] {
		selectCols += ", subtask_parent_uuid"
	}

	rows, err := svc.db.Query(fmt.Sprintf(`
		SELECT %s
		FROM tasks
		ORDER BY done ASC, CASE WHEN done = 0 THEN scheduled_date ELSE completed_at END ASC
	`, selectCols))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			svc.Deps.MustGetLogger().Warn("tasks: failed to close rows", "error", err)
		}
	}()

	tasks := []TaskRow{}
	tagCounts := make(map[string]int)

	for rows.Next() {
		var task TaskRow
		var scheduledAtStr, completedAtStr, updatedAtStr sql.NullString
		var hasScheduledTime bool
		var recurrenceType string
		var recurrenceDays string
		var recurrenceInterval int

		// Build dynamic scan target list to support optional uuid/subtask_parent_uuid columns
		// Note: SQLite returns datetime as strings, so we scan into NullString and parse
		scanTargets := []interface{}{
			&task.ID, &task.Title, &task.Done, &scheduledAtStr, &completedAtStr,
			&task.Tags, &hasScheduledTime, &updatedAtStr, &task.Color, &recurrenceType, &recurrenceDays, &recurrenceInterval,
		}
		if cols["uuid"] {
			scanTargets = append(scanTargets, &task.UUID)
		}
		if cols["subtask_parent_uuid"] {
			scanTargets = append(scanTargets, &task.ParentUUID)
		}
		if err := rows.Scan(scanTargets...); err != nil {
			continue
		}

		// Parse datetime strings
		var scheduledAt, completedAt, updatedAt time.Time
		if scheduledAtStr.Valid && scheduledAtStr.String != "" {
			if t, err := time.Parse(time.RFC3339Nano, scheduledAtStr.String); err == nil {
				scheduledAt = t
			}
		}
		if completedAtStr.Valid && completedAtStr.String != "" {
			if t, err := time.Parse(time.RFC3339Nano, completedAtStr.String); err == nil {
				completedAt = t
			}
		}
		if updatedAtStr.Valid && updatedAtStr.String != "" {
			if t, err := time.Parse(time.RFC3339Nano, updatedAtStr.String); err == nil {
				updatedAt = t
			}
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

		if !task.Done && !scheduledAt.IsZero() {
			logicalDay := svc.logicalCalc.GetLogicalDay(scheduledAt, hasScheduledTime)

			if isPastOrFuture {
				// For past/future, check if task is before/after the cutoff
				switch view {
				case "past":
					// Show tasks scheduled before yesterday
					if logicalDay.Before(svc.logicalCalc.Yesterday()) {
						shouldInclude = true
						task.ScheduledAt = scheduledAt.Format(time.RFC3339Nano)
					}
				case "future":
					// Show tasks scheduled after tomorrow
					if logicalDay.After(svc.logicalCalc.Tomorrow()) {
						shouldInclude = true
						task.ScheduledAt = scheduledAt.Format(time.RFC3339Nano)
					}
				}
			} else {
				// For specific days, check exact match or include past tasks for "today" view
				if logicalDay.Year() == targetDay.Year() &&
					logicalDay.Month() == targetDay.Month() &&
					logicalDay.Day() == targetDay.Day() {
					shouldInclude = true
					task.ScheduledAt = scheduledAt.Format(time.RFC3339Nano)
					task.Category = "today"
				} else if view == "today" && logicalDay.Before(targetDay) {
					// For today view, also include past incomplete tasks
					shouldInclude = true
					task.ScheduledAt = scheduledAt.Format(time.RFC3339Nano)
					task.Category = "past"
				}
			}
		}

		// Show completed tasks if viewing today or yesterday
		if task.Done && !completedAt.IsZero() && (view == "today" || view == "yesterday") {
			// For completed tasks, use the logical day calculation to determine which logical day they belong to
			// We use the completedAt time with hasScheduledTime=true since it's a specific time
			completedLogicalDay := svc.logicalCalc.GetLogicalDay(completedAt, true)

			if completedLogicalDay.Year() == targetDay.Year() &&
				completedLogicalDay.Month() == targetDay.Month() &&
				completedLogicalDay.Day() == targetDay.Day() {
				shouldInclude = true
				task.CompletedAt = completedAt.Format(time.RFC3339Nano)
			}
		}

		if !shouldInclude {
			continue
		}

		// Set UpdatedAt for display
		if !updatedAt.IsZero() {
			task.UpdatedAt = updatedAt.Format(time.RFC3339Nano)
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

	// Annotate subtasks so the frontend knows which tasks have children and can
	// display the collapse/expand toggle. Doing this here ensures all callers
	// receive the flag regardless of wrapper logic.
	svc.annotateSubtaskInfo(tasks)

	return map[string]any{"tasks": tasks, "tagCounts": tagCounts}, nil
}

// fetchRecentTasks returns tasks ordered by most recently modified (most recent first).
func (svc *Service) fetchRecentTasks() (any, error) {
	cols := svc.detectedCols()
	selectCols := "id, title, done, scheduled_date, completed_at, tags, scheduled_time, updated_at, color, recurrence, recurrence_days, recurrence_x"
	if cols["uuid"] {
		selectCols += ", uuid"
	}
	if cols["subtask_parent_uuid"] {
		selectCols += ", subtask_parent_uuid"
	}
	rows, err := svc.db.Query(fmt.Sprintf(`
		SELECT %s
		FROM tasks
		ORDER BY updated_at DESC NULLS LAST
	`, selectCols))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			svc.Deps.MustGetLogger().Warn("tasks: failed to close rows", "error", err)
		}
	}()
	tasks := []TaskRow{}
	tagCounts := make(map[string]int)

	for rows.Next() {
		var task TaskRow
		var scheduledAtStr, completedAtStr, updatedAtStr sql.NullString
		var hasScheduledTime bool
		var recurrenceType string
		var recurrenceDays string
		var recurrenceInterval int

		// Build dynamic scan target list to support optional uuid/subtask_parent_uuid columns
		// Note: SQLite returns datetime as strings, so we scan into NullString and parse
		scanTargets := []interface{}{
			&task.ID, &task.Title, &task.Done, &scheduledAtStr, &completedAtStr,
			&task.Tags, &hasScheduledTime, &updatedAtStr, &task.Color, &recurrenceType, &recurrenceDays, &recurrenceInterval,
		}
		if cols["uuid"] {
			scanTargets = append(scanTargets, &task.UUID)
		}
		if cols["subtask_parent_uuid"] {
			scanTargets = append(scanTargets, &task.ParentUUID)
		}
		if err := rows.Scan(scanTargets...); err != nil {
			continue
		}

		// Parse datetime strings
		var scheduledAt, completedAt, updatedAt time.Time
		if scheduledAtStr.Valid && scheduledAtStr.String != "" {
			if t, err := time.Parse(time.RFC3339Nano, scheduledAtStr.String); err == nil {
				scheduledAt = t
			}
		}
		if completedAtStr.Valid && completedAtStr.String != "" {
			if t, err := time.Parse(time.RFC3339Nano, completedAtStr.String); err == nil {
				completedAt = t
			}
		}
		if updatedAtStr.Valid && updatedAtStr.String != "" {
			if t, err := time.Parse(time.RFC3339Nano, updatedAtStr.String); err == nil {
				updatedAt = t
			}
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
		if !scheduledAt.IsZero() {
			task.ScheduledAt = scheduledAt.Format(time.RFC3339Nano)
		}
		if !completedAt.IsZero() {
			task.CompletedAt = completedAt.Format(time.RFC3339Nano)
		}
		if !updatedAt.IsZero() {
			task.UpdatedAt = updatedAt.Format(time.RFC3339Nano)
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
	cols := svc.detectedCols()
	selectCols := "id, title, done, scheduled_date, completed_at, tags, scheduled_time, updated_at, color, recurrence, recurrence_days, recurrence_x"
	if cols["uuid"] {
		selectCols += ", uuid"
	}
	if cols["subtask_parent_uuid"] {
		selectCols += ", subtask_parent_uuid"
	}
	rows, err := svc.db.Query(fmt.Sprintf(`
		SELECT %s
		FROM tasks
		ORDER BY done ASC, CASE WHEN done = 0 THEN scheduled_date ELSE completed_at END ASC
	`, selectCols))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			svc.Deps.MustGetLogger().Warn("tasks: failed to close rows", "error", err)
		}
	}()
	tasks := []TaskRow{}
	tagCounts := make(map[string]int)

	for rows.Next() {
		var task TaskRow
		var scheduledAtStr, completedAtStr, updatedAtStr sql.NullString
		var hasScheduledTime bool
		var recurrenceType string
		var recurrenceDays string
		var recurrenceInterval int

		// Build dynamic scan target list to support optional uuid/subtask_parent_uuid columns
		// Note: SQLite returns datetime as strings, so we scan into NullString and parse
		scanTargets := []interface{}{
			&task.ID, &task.Title, &task.Done, &scheduledAtStr, &completedAtStr,
			&task.Tags, &hasScheduledTime, &updatedAtStr, &task.Color, &recurrenceType, &recurrenceDays, &recurrenceInterval,
		}
		if cols["uuid"] {
			scanTargets = append(scanTargets, &task.UUID)
		}
		if cols["subtask_parent_uuid"] {
			scanTargets = append(scanTargets, &task.ParentUUID)
		}
		if err := rows.Scan(scanTargets...); err != nil {
			continue
		}

		// Parse datetime strings
		var scheduledAt, completedAt, updatedAt time.Time
		if scheduledAtStr.Valid && scheduledAtStr.String != "" {
			if t, err := time.Parse(time.RFC3339Nano, scheduledAtStr.String); err == nil {
				scheduledAt = t
			}
		}
		if completedAtStr.Valid && completedAtStr.String != "" {
			if t, err := time.Parse(time.RFC3339Nano, completedAtStr.String); err == nil {
				completedAt = t
			}
		}
		if updatedAtStr.Valid && updatedAtStr.String != "" {
			if t, err := time.Parse(time.RFC3339Nano, updatedAtStr.String); err == nil {
				updatedAt = t
			}
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
		if !scheduledAt.IsZero() {
			task.ScheduledAt = scheduledAt.Format(time.RFC3339Nano)
		}
		if !completedAt.IsZero() {
			task.CompletedAt = completedAt.Format(time.RFC3339Nano)
		}
		if !updatedAt.IsZero() {
			task.UpdatedAt = updatedAt.Format(time.RFC3339Nano)
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

// annotateSubtaskInfo populates HasSubtasks on tasks by checking for children in the DB.
func (svc *Service) annotateSubtaskInfo(tasks []TaskRow) {
	if svc.db == nil || len(tasks) == 0 {
		return
	}

	// Collect UUIDs to query
	uuids := make(map[string]int)
	for _, t := range tasks {
		if t.UUID != "" {
			uuids[t.UUID] = 0
		}
	}
	if len(uuids) == 0 {
		return
	}

	// Build query with IN clause
	args := []interface{}{}
	placeholders := []string{}
	for u := range uuids {
		placeholders = append(placeholders, "?")
		args = append(args, u)
	}
	placeholdersStr := strings.Repeat("?,", len(placeholders))
	if len(placeholdersStr) > 0 {
		placeholdersStr = strings.TrimSuffix(placeholdersStr, ",")
	}
	// #nosec G202 -- placeholdersStr is constructed from fixed string elements and not from user input
	query := "SELECT subtask_parent_uuid, COUNT(1) FROM tasks WHERE subtask_parent_uuid IN (" + placeholdersStr + ") GROUP BY subtask_parent_uuid"
	rows, err := svc.db.Query(query, args...)
	if err != nil {
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			svc.Deps.MustGetLogger().Warn("tasks: failed to close rows", "error", err)
		}
	}()

	for rows.Next() {
		var parent sql.NullString
		var count int
		if err := rows.Scan(&parent, &count); err == nil && parent.Valid {
			uuids[parent.String] = count
		}
	}

	for i := range tasks {
		if tasks[i].UUID != "" {
			if uuids[tasks[i].UUID] > 0 {
				tasks[i].HasSubtasks = true
			} else {
				tasks[i].HasSubtasks = false
			}
		}
	}
}

// detectedCols inspects the tasks table schema and returns a map of available column names.
func (svc *Service) detectedCols() map[string]bool {
	cols := map[string]bool{}
	if svc.db == nil {
		return cols
	}
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
	}
	return cols
}

// fetchSubtasks returns child tasks for a given parent UUID.
func (svc *Service) fetchSubtasks(parentUUID string) (any, error) {
	if svc.db == nil {
		return nil, fmt.Errorf("tasks database not available")
	}

	svc.Deps.MustGetLogger().Info("tasks: fetchSubtasks called", "parentUUID", parentUUID)

	// First, test basic query to verify database is working
	var testCount int
	testErr := svc.db.QueryRow("SELECT COUNT(*) FROM tasks WHERE subtask_parent_uuid = ?", parentUUID).Scan(&testCount)
	if testErr != nil {
		svc.Deps.MustGetLogger().Warn("tasks: fetchSubtasks test query error", "error", testErr)
	} else {
		svc.Deps.MustGetLogger().Info("tasks: fetchSubtasks test query result", "count", testCount, "parentUUID", parentUUID)
	}

	// Detect optional columns
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
	}
	svc.Deps.MustGetLogger().Info("tasks: fetchSubtasks detected columns", "has_uuid", cols["uuid"], "has_subtask_parent_uuid", cols["subtask_parent_uuid"])

	selectCols := "id, title, done, scheduled_date, completed_at, tags, scheduled_time, updated_at, color, recurrence, recurrence_days, recurrence_x, position"
	if cols["uuid"] {
		selectCols += ", uuid"
	}
	if cols["subtask_parent_uuid"] {
		selectCols += ", subtask_parent_uuid"
	}

	q := "SELECT " + selectCols + " FROM tasks WHERE subtask_parent_uuid = ? ORDER BY done ASC, position ASC"
	svc.Deps.MustGetLogger().Info("tasks: fetchSubtasks query", "query", q, "parentUUID", parentUUID)
	rows, err := svc.db.Query(q, parentUUID)
	if err != nil {
		svc.Deps.MustGetLogger().Warn("tasks: fetchSubtasks query error", "error", err)
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			svc.Deps.MustGetLogger().Warn("tasks: failed to close rows", "error", err)
		}
	}()

	tasks := []TaskRow{}
	tagCounts := make(map[string]int)

	rowCount := 0
	for rows.Next() {
		rowCount++
		svc.Deps.MustGetLogger().Info("tasks: fetchSubtasks processing row", "rowNum", rowCount)
		var task TaskRow
		var scheduledAtStr, completedAtStr, updatedAtStr sql.NullString
		var hasScheduledTime bool
		var recurrenceType string
		var recurrenceDays string
		var recurrenceInterval int

		// Build dynamic scan target list to support optional uuid/subtask_parent_uuid columns
		// Note: SQLite returns datetime as strings, so we scan into NullString and parse
		// Position is always included now, so add it before the optional columns
		scanTargets := []interface{}{
			&task.ID, &task.Title, &task.Done, &scheduledAtStr, &completedAtStr,
			&task.Tags, &hasScheduledTime, &updatedAtStr, &task.Color, &recurrenceType, &recurrenceDays, &recurrenceInterval,
			&task.SortOrder,
		}
		if cols["uuid"] {
			scanTargets = append(scanTargets, &task.UUID)
		}
		if cols["subtask_parent_uuid"] {
			scanTargets = append(scanTargets, &task.ParentUUID)
		}
		if err := rows.Scan(scanTargets...); err != nil {
			svc.Deps.MustGetLogger().Warn("tasks: fetchSubtasks scan error", "error", err)
			continue
		}

		// Parse datetime strings
		var scheduledAt, completedAt, updatedAt time.Time
		if scheduledAtStr.Valid && scheduledAtStr.String != "" {
			if t, err := time.Parse(time.RFC3339Nano, scheduledAtStr.String); err == nil {
				scheduledAt = t
			}
		}
		if completedAtStr.Valid && completedAtStr.String != "" {
			if t, err := time.Parse(time.RFC3339Nano, completedAtStr.String); err == nil {
				completedAt = t
			}
		}
		if updatedAtStr.Valid && updatedAtStr.String != "" {
			if t, err := time.Parse(time.RFC3339Nano, updatedAtStr.String); err == nil {
				updatedAt = t
			}
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
		if !scheduledAt.IsZero() {
			task.ScheduledAt = scheduledAt.Format(time.RFC3339Nano)
		}
		if !completedAt.IsZero() {
			task.CompletedAt = completedAt.Format(time.RFC3339Nano)
		}
		if !updatedAt.IsZero() {
			task.UpdatedAt = updatedAt.Format(time.RFC3339Nano)
		}

		tasks = append(tasks, task)
		svc.Deps.MustGetLogger().Info("tasks: fetchSubtasks appended task", "id", task.ID, "title", task.Title)

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

	svc.Deps.MustGetLogger().Info("tasks: fetchSubtasks returning tasks", "count", len(tasks), "parentUUID", parentUUID)

	// Annotate subtasks
	svc.annotateSubtaskInfo(tasks)

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
	var scheduledDate time.Time
	hasScheduledTimeFlag := 0

	if scheduledAt, ok := params["scheduledAt"].(string); ok && scheduledAt != "" {
		// Use the provided scheduled time
		t, err := time.Parse(time.RFC3339, scheduledAt)
		if err == nil {
			if val, ok := params["hasScheduledTime"].(bool); ok && val {
				hasScheduledTimeFlag = 1
			}
			scheduledDate = t
		} else {
			// Fallback: use logical today
			if svc.logicalCalc != nil {
				scheduledDate = svc.logicalCalc.Today()
			} else {
				scheduledDate = time.Now()
			}
		}
	} else {
		// No specific time provided: use logical today (as an all-day task)
		if svc.logicalCalc != nil {
			scheduledDate = svc.logicalCalc.Today()
			svc.Deps.MustGetLogger().Debug("tasks: using logical today", "logical_date", scheduledDate, "utc_date", scheduledDate.UTC())
		} else {
			scheduledDate = time.Now()
			svc.Deps.MustGetLogger().Warn("tasks: logicalCalc is nil, using time.Now()")
		}
	}

	if val, ok := params["hasScheduledTime"].(bool); ok && val {
		hasScheduledTimeFlag = 1
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Insert the task
	result, err := svc.db.Exec(
		"INSERT INTO tasks (uuid, title, scheduled_date, created_at, updated_at, done, scheduled_time) VALUES (?, ?, ?, ?, ?, 0, ?)",
		uuid.New().String(), title, scheduledDate.UTC().Format(time.RFC3339Nano), now, now, hasScheduledTimeFlag,
	)
	if err != nil {
		return nil, err
	}

	taskID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	// Send SSE message about the new task
	messenger := svc.Deps.MustGetMessenger()
	if messenger != nil {
		msg := core.Message{
			Version:     "1.0",
			Timestamp:   time.Now(),
			ChannelName: "tasks",
			ServiceType: "tasks",
			ServiceName: "tasks",
			Event:       "taskCreated",
			Status:      "created",
		}
		// Add task details to Body as JSON
		taskDetails := map[string]any{
			"taskId": taskID,
			"title":  title,
		}
		if body, err := json.Marshal(taskDetails); err == nil {
			msg.Body = string(body)
		}
		if err := messenger.Send(msg); err != nil {
			svc.Deps.MustGetLogger().Warn("tasks: failed to send SSE message", "error", err)
		}
	}

	return map[string]any{"status": "ok", "taskId": taskID}, nil
}

// deleteTask deletes a task from the database.
func (svc *Service) deleteTask(taskID int64) (any, error) {
	if svc.db == nil {
		return nil, fmt.Errorf("tasks database not available")
	}

	// Delete the task
	result, err := svc.db.Exec("DELETE FROM tasks WHERE id = ?", taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to delete task: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return nil, fmt.Errorf("task not found")
	}

	// Send SSE message about the deletion
	messenger := svc.Deps.MustGetMessenger()
	if messenger != nil {
		msg := core.Message{
			Version:     "1.0",
			Timestamp:   time.Now(),
			ChannelName: "tasks",
			ServiceType: "tasks",
			ServiceName: "tasks",
			Event:       "taskDeleted",
			Status:      "deleted",
		}
		// Add task ID to Body as JSON
		taskDetails := map[string]any{
			"taskId": taskID,
		}
		if body, err := json.Marshal(taskDetails); err == nil {
			msg.Body = string(body)
		}
		if err := messenger.Send(msg); err != nil {
			svc.Deps.MustGetLogger().Warn("tasks: failed to send SSE message", "error", err)
		}
	}

	return map[string]any{"status": "ok"}, nil
}

func (svc *Service) createSubtask(parentUUID string, params map[string]any) (any, error) {
	if svc.db == nil {
		return nil, fmt.Errorf("tasks database not available")
	}

	title, _ := params["title"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("subtask title cannot be empty")
	}

	// Get the parent task to find its scheduled date
	var scheduledDate string
	err := svc.db.QueryRow(
		"SELECT scheduled_date FROM tasks WHERE uuid = ?",
		parentUUID,
	).Scan(&scheduledDate)
	if err != nil {
		return nil, fmt.Errorf("parent task not found: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Get the minimum position (for adding to top)
	var minPosition int64
	err = svc.db.QueryRow(
		"SELECT COALESCE(MIN(position), 0) FROM tasks WHERE subtask_parent_uuid = ?",
		parentUUID,
	).Scan(&minPosition)
	if err != nil {
		minPosition = 0
	} else {
		minPosition = minPosition - 1 // Insert above current minimum
	}

	// Insert the subtask
	result, err := svc.db.Exec(
		"INSERT INTO tasks (uuid, title, scheduled_date, subtask_parent_uuid, created_at, updated_at, done, position) VALUES (?, ?, ?, ?, ?, ?, 0, ?)",
		uuid.New().String(), title, scheduledDate, parentUUID, now, now, minPosition,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create subtask: %w", err)
	}

	taskID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	// Send SSE message about the new subtask
	messenger := svc.Deps.MustGetMessenger()
	if messenger != nil {
		msg := core.Message{
			Version:     "1.0",
			Timestamp:   time.Now(),
			ChannelName: "tasks",
			ServiceType: "tasks",
			ServiceName: "tasks",
			Event:       "subtaskCreated",
			Status:      "created",
		}
		taskDetails := map[string]any{
			"taskId":     taskID,
			"parentUuid": parentUUID,
			"title":      title,
		}
		if body, err := json.Marshal(taskDetails); err == nil {
			msg.Body = string(body)
		}
		if err := messenger.Send(msg); err != nil {
			svc.Deps.MustGetLogger().Warn("tasks: failed to send SSE message", "error", err)
		}
	}

	return map[string]any{
		"status":     "ok",
		"taskId":     taskID,
		"title":      title,
		"parentUuid": parentUUID,
	}, nil
}

// toggleTask marks a task as done or undone.
func (svc *Service) toggleTask(taskID int64) (any, error) {
	if svc.db == nil {
		return nil, fmt.Errorf("tasks database not available")
	}

	// Get current done status and recurrence metadata
	var done bool
	var uuidVal sql.NullString
	var title sql.NullString
	var tags sql.NullString
	var color sql.NullString
	var scheduledDateStr sql.NullString
	var scheduledTime sql.NullBool
	var recurrenceType sql.NullString
	var recurrenceDays sql.NullString
	var recurrenceInterval sql.NullInt64
	var userID int64
	var importance int
	var urgency int
	var parentUUID sql.NullString

	err := svc.db.QueryRow(`
		SELECT done, uuid, title, tags, color, scheduled_date, scheduled_time, recurrence, recurrence_days, recurrence_x, user_id, importance, urgency, parent_uuid
		FROM tasks WHERE id = ?`, taskID).Scan(&done, &uuidVal, &title, &tags, &color, &scheduledDateStr, &scheduledTime, &recurrenceType, &recurrenceDays, &recurrenceInterval, &userID, &importance, &urgency, &parentUUID)
	if err != nil {
		return nil, err
	}

	// If marking as done, check for incomplete subtasks
	if !done && uuidVal.Valid && uuidVal.String != "" {
		var incompleteCount int
		err := svc.db.QueryRow(
			"SELECT COUNT(*) FROM tasks WHERE subtask_parent_uuid = ? AND done = 0",
			uuidVal.String,
		).Scan(&incompleteCount)
		if err == nil && incompleteCount > 0 {
			return map[string]any{"status": "error", "error": "Cannot mark task as done: there are incomplete subtasks"}, nil
		}
	}

	// Parse scheduled_date if it's a string
	var scheduledDate time.Time
	if scheduledDateStr.Valid && scheduledDateStr.String != "" {
		if t, err := time.Parse(time.RFC3339Nano, scheduledDateStr.String); err == nil {
			scheduledDate = t
		}
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
			base := time.Now()
			if !scheduledDate.IsZero() {
				base = scheduledDate
			}

			// Capture the wall clock time from the original scheduled date if it had a specific time.
			// This is important because logical day adjustment may shift base to midnight.
			var origHour, origMin, origSec int
			if scheduledTime.Valid && scheduledTime.Bool {
				origHour, origMin, origSec = base.In(time.Local).Clock()
			}

			// Adjust base to its logical day to ensure we calculate the next instance correctly.
			// This handles cases where a task is completed after midnight but before the logical day rollover (e.g., 4am).
			if svc.logicalCalc != nil {
				base = svc.logicalCalc.GetLogicalDay(base, scheduledTime.Valid && scheduledTime.Bool)
			} else {
				base = base.UTC()
			}

			next := rp.Next(base)
			if !next.IsZero() {
				var newScheduled string
				scheduledFlag := 0
				if scheduledTime.Valid && scheduledTime.Bool {
					// Re-apply original wall clock time to the next calculated day
					loc := time.Local
					if svc.logicalCalc != nil && svc.logicalCalc.Location() != nil {
						loc = svc.logicalCalc.Location()
					}
					nextWithTime := time.Date(next.Year(), next.Month(), next.Day(), origHour, origMin, origSec, 0, loc).UTC()
					newScheduled = nextWithTime.Format(time.RFC3339Nano)
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

				newUUID := uuid.New().String()
				_, err = svc.db.Exec(`
					INSERT INTO tasks (uuid, parent_uuid, title, tags, color, scheduled_date, scheduled_time, recurrence, recurrence_days, recurrence_x, created_at, updated_at, done, user_id, importance, urgency)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)
				`, newUUID, parentUUID.String, title.String, tags.String, color.String, newScheduled, scheduledFlag, recurrenceType.String, recurrenceDays.String, interval, now, now, userID, importance, urgency)
				if err != nil {
					svc.Deps.MustGetLogger().Warn("tasks: failed to create next recurring task", "error", err)
				}
			}
		}
	}

	// Send SSE message about the task update
	messenger := svc.Deps.MustGetMessenger()
	if messenger != nil {
		msg := core.Message{
			Version:     "1.0",
			Timestamp:   time.Now(),
			ChannelName: "tasks",
			ServiceType: "tasks",
			ServiceName: "tasks",
			Event:       "taskUpdated",
			Status:      "updated",
		}
		// Add task details to Body as JSON
		taskDetails := map[string]any{
			"taskId": taskID,
			"done":   newDone,
		}
		if body, err := json.Marshal(taskDetails); err == nil {
			msg.Body = string(body)
		}
		if err := messenger.Send(msg); err != nil {
			svc.Deps.MustGetLogger().Warn("tasks: failed to send SSE message", "error", err)
		}
	}

	return map[string]any{"status": "ok", "done": newDone}, nil
}

// reorderSubtask updates the sort order (position) of a subtask within its parent.
// Handles renumbering other tasks as needed to maintain consistent sort order.
func (svc *Service) reorderSubtask(taskID int64, newPosition int64, parentUUID string) (any, error) {
	if svc.db == nil {
		return nil, fmt.Errorf("tasks database not available")
	}

	// Get the current task to determine its done status
	var currentDone bool
	var currentPosition int64
	err := svc.db.QueryRow("SELECT done, position FROM tasks WHERE id = ?", taskID).Scan(&currentDone, &currentPosition)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}

	// Get all subtasks in this parent, ordered by done and position
	rows, err := svc.db.Query(`
		SELECT id, position, done FROM tasks 
		WHERE subtask_parent_uuid = ? 
		ORDER BY done ASC, position ASC
	`, parentUUID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			svc.Deps.MustGetLogger().Warn("tasks: failed to close rows", "error", err)
		}
	}()

	type subtaskInfo struct {
		id       int64
		position int64
		done     bool
	}

	var subtasks []subtaskInfo
	for rows.Next() {
		var st subtaskInfo
		if err := rows.Scan(&st.id, &st.position, &st.done); err != nil {
			continue
		}
		subtasks = append(subtasks, st)
	}

	// Count incomplete vs completed subtasks
	var incompleteCount, completeCount int
	for _, st := range subtasks {
		if st.done {
			completeCount++
		} else {
			incompleteCount++
		}
	}

	// Determine the boundary between incomplete and complete
	incompleteBoundary := int64(incompleteCount)
	if incompleteBoundary > 0 {
		incompleteBoundary-- // positions are 0-indexed
	}

	// Validate the new position based on current task's done status
	// Incomplete tasks can only move within the incomplete section
	// Completed tasks can only move within the completed section
	if !currentDone && newPosition > incompleteBoundary {
		return nil, fmt.Errorf("cannot drag incomplete task below completed tasks")
	}
	if currentDone && newPosition < int64(incompleteCount) {
		return nil, fmt.Errorf("cannot drag completed task above incomplete tasks")
	}

	// Build new sort order
	// Remove the current task from its old position and insert at new position
	var reorderedTasks []subtaskInfo
	for _, st := range subtasks {
		if st.id != taskID {
			reorderedTasks = append(reorderedTasks, st)
		}
	}

	// Insert at new position
	if newPosition > int64(len(reorderedTasks)) {
		newPosition = int64(len(reorderedTasks))
	}
	if newPosition < 0 {
		newPosition = 0
	}

	newList := make([]subtaskInfo, len(reorderedTasks)+1)
	copy(newList[:newPosition], reorderedTasks[:newPosition])
	newList[newPosition] = subtaskInfo{id: taskID, position: newPosition, done: currentDone}
	copy(newList[newPosition+1:], reorderedTasks[newPosition:])

	// Update all positions in database
	tx, err := svc.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err.Error() != "sql: transaction has already been committed or rolled back" {
			svc.Deps.MustGetLogger().Warn("tasks: failed to rollback transaction", "error", err)
		}
	}()

	for i, st := range newList {
		_, err := tx.Exec("UPDATE tasks SET position = ? WHERE id = ?", i, st.id)
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Return updated subtasks
	return map[string]any{"status": "ok", "subtasks": newList}, nil
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
