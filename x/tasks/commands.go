package tasks

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"keyop/core"
	"keyop/x/recurrence"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// parseTimeStringLocal parses simple time expressions like "1pm", "13:00" or "1:30pm"
// and returns a time.Time on the provided base date in the provided location.
func parseTimeStringLocal(s string, base time.Time, loc *time.Location) (time.Time, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return time.Time{}, false
	}

	re := regexp.MustCompile(`^(\d{1,2})(?::(\d{2}))?\s*(am|pm)?$`)
	if m := re.FindStringSubmatch(s); m != nil {
		hour, _ := strconv.Atoi(m[1])
		minute := 0
		if m[2] != "" {
			minute, _ = strconv.Atoi(m[2])
		}
		ampm := m[3]
		if ampm != "" {
			if ampm == "am" {
				if hour == 12 {
					hour = 0
				}
			} else { // pm
				if hour != 12 {
					hour += 12
				}
			}
		}
		if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
			return time.Time{}, false
		}
		return time.Date(base.Year(), base.Month(), base.Day(), hour, minute, 0, 0, loc), true
	}

	re2 := regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)
	if m := re2.FindStringSubmatch(s); m != nil {
		hour, _ := strconv.Atoi(m[1])
		minute, _ := strconv.Atoi(m[2])
		if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
			return time.Time{}, false
		}
		return time.Date(base.Year(), base.Month(), base.Day(), hour, minute, 0, 0, loc), true
	}

	return time.Time{}, false
}

// runTaskCommand executes a textual command against a task. It supports at least:
// - "c <color>" or "color <color>" to set task color
// - "r <expr>" or "reschedule <expr>" to change scheduled date/time
// - "skip" to advance a recurring task to its next occurrence
// - "x" to mark the task as done
// The function returns an updated TaskRow (if available) or a small map with status and updated fields so callers (UI/TUI) can refresh.
func (svc *Service) runTaskCommand(taskID int64, command string, view string) (any, error) {
	if svc.db == nil {
		return nil, fmt.Errorf("tasks database not available")
	}
	cmdText := strings.TrimSpace(command)
	if cmdText == "" {
		return nil, fmt.Errorf("empty command")
	}
	parts := strings.Fields(cmdText)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	cmd := strings.ToLower(parts[0])
	args := ""
	if len(parts) > 1 {
		args = strings.Join(parts[1:], " ")
	}

	// helper: try to find updated task by querying fetchTasks for the provided view and fallbacks
	findUpdatedTask := func() (*TaskRow, error) {
		views := []string{}
		if view != "" {
			views = append(views, view)
		}
		// try common useful views
		views = append(views, "today", "recent", "all")
		for _, v := range views {
			res, err := svc.fetchTasks(v)
			if err != nil {
				continue
			}
			if m, ok := res.(map[string]any); ok {
				if tasksIface, ok := m["tasks"]; ok {
					if tasksSlice, ok := tasksIface.([]TaskRow); ok {
						for _, t := range tasksSlice {
							if t.ID == taskID {
								return &t, nil
							}
						}
					}
				}
			}
		}
		return nil, nil
	}

	switch cmd {
	case "x":
		var done bool
		if err := svc.db.QueryRow("SELECT done FROM tasks WHERE id = ?", taskID).Scan(&done); err != nil {
			return nil, err
		}
		if !done {
			res, err := svc.toggleTask(taskID)
			if err != nil {
				return nil, err
			}
			if m, ok := res.(map[string]any); ok {
				if status, _ := m["status"].(string); status == "error" {
					return m, nil
				}
			}
		}
		if t, _ := findUpdatedTask(); t != nil {
			return t, nil
		}
		return map[string]any{"status": "ok", "done": true}, nil

	case "c", "color":
		color := strings.TrimSpace(args)
		if color == "" {
			return nil, fmt.Errorf("color required")
		}
		// Interpret "0" as clear
		var colorParam any = color
		if color == "0" {
			colorParam = ""
		}

		if _, err := svc.updateTask(taskID, map[string]any{"color": colorParam}); err != nil {
			return nil, err
		}

		// Try to return updated TaskRow for UI to merge and resort
		if t, _ := findUpdatedTask(); t != nil {
			// Emit SSE about the change (include color even if empty)
			messenger := svc.Deps.MustGetMessenger()
			if messenger != nil {
				msg := core.Message{Version: "1.0", Timestamp: time.Now(), ChannelName: "tasks", ServiceType: "tasks", ServiceName: "tasks", Event: "taskUpdated", Status: "updated"}
				bodyMap := map[string]any{"taskId": taskID, "color": t.Color}
				if b, err := json.Marshal(bodyMap); err == nil {
					msg.Body = string(b)
				}
				_ = messenger.Send(msg)
			}
			return t, nil
		}

		// Fallback minimal response
		var colorDB sql.NullString
		if err := svc.db.QueryRow("SELECT color FROM tasks WHERE id = ?", taskID).Scan(&colorDB); err != nil {
			return nil, err
		}
		resp := map[string]any{"status": "ok"}
		if colorDB.Valid {
			resp["color"] = colorDB.String
		} else {
			resp["color"] = ""
		}
		// Emit SSE
		messenger := svc.Deps.MustGetMessenger()
		if messenger != nil {
			msg := core.Message{Version: "1.0", Timestamp: time.Now(), ChannelName: "tasks", ServiceType: "tasks", ServiceName: "tasks", Event: "taskUpdated", Status: "updated"}
			bodyMap := map[string]any{"taskId": taskID, "color": nil}
			if colorDB.Valid {
				bodyMap["color"] = colorDB.String
			}
			if b, err := json.Marshal(bodyMap); err == nil {
				msg.Body = string(b)
			}
			_ = messenger.Send(msg)
		}

		return resp, nil

	case "r", "reschedule":
		expr := strings.TrimSpace(args)
		if expr == "" {
			return nil, fmt.Errorf("reschedule expression required")
		}

		// Support clear command: 'r 0' clears scheduled date
		if expr == "0" || strings.ToLower(expr) == "clear" {
			if _, err := svc.updateTask(taskID, map[string]any{"scheduledAt": "", "hasScheduledTime": false}); err != nil {
				return nil, err
			}
			if t, _ := findUpdatedTask(); t != nil {
				messenger := svc.Deps.MustGetMessenger()
				if messenger != nil {
					msg := core.Message{Version: "1.0", Timestamp: time.Now(), ChannelName: "tasks", ServiceType: "tasks", ServiceName: "tasks", Event: "taskUpdated", Status: "updated"}
					bodyMap := map[string]any{"taskId": taskID, "scheduledAt": nil}
					if b, err := json.Marshal(bodyMap); err == nil {
						msg.Body = string(b)
					}
					_ = messenger.Send(msg)
				}
				return t, nil
			}
			return map[string]any{"status": "ok"}, nil
		}

		// Load current scheduled_date and scheduled_time
		var scheduledDate sql.NullString
		var scheduledTime sql.NullBool
		if err := svc.db.QueryRow("SELECT scheduled_date, scheduled_time FROM tasks WHERE id = ?", taskID).Scan(&scheduledDate, &scheduledTime); err != nil {
			return nil, err
		}

		loc := time.Local
		if svc.logicalCalc != nil && svc.logicalCalc.Location() != nil {
			loc = svc.logicalCalc.Location()
		}
		var base time.Time
		var hasTime bool
		if scheduledDate.Valid && scheduledDate.String != "" {
			if t, err := time.Parse(time.RFC3339Nano, scheduledDate.String); err == nil {
				base = t.In(loc)
			} else if t2, err2 := time.Parse(time.RFC3339, scheduledDate.String); err2 == nil {
				base = t2.In(loc)
			} else {
				base = time.Now().In(loc)
			}
			hasTime = scheduledTime.Valid && scheduledTime.Bool
		} else {
			if svc.logicalCalc != nil {
				base = svc.logicalCalc.Today().In(loc)
			} else {
				base = time.Now().In(loc)
			}
			hasTime = false
		}

		lower := strings.ToLower(expr)
		var newTime time.Time
		var resultHasTime bool

		// Relative +/-N[d|h|m|w|y]
		relRe := regexp.MustCompile(`^([+-])\s*(\d+)\s*([dhmwy]?)$`)
		if m := relRe.FindStringSubmatch(lower); m != nil {
			sign := 1
			if m[1] == "-" {
				sign = -1
			}
			n, _ := strconv.Atoi(m[2])
			unit := m[3]
			b := base
			switch unit {
			case "", "d":
				b = b.AddDate(0, 0, sign*n)
				resultHasTime = hasTime
			case "w":
				b = b.AddDate(0, 0, sign*n*7)
				resultHasTime = hasTime
			case "h":
				b = b.Add(time.Duration(sign*n) * time.Hour)
				resultHasTime = true
			case "m":
				b = b.Add(time.Duration(sign*n) * time.Minute)
				resultHasTime = true
			case "y":
				b = b.AddDate(sign*n, 0, 0)
				resultHasTime = hasTime
			default:
				b = base
				resultHasTime = hasTime
			}
			newTime = b
		} else if lower == "now" {
			newTime = time.Now().In(loc)
			resultHasTime = true
		} else if lower == "today" || lower == "tomorrow" || lower == "yesterday" {
			var d time.Time
			if svc.logicalCalc != nil {
				d = svc.logicalCalc.Today().In(loc)
			} else {
				now := time.Now().In(loc)
				d = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
			}
			if lower == "tomorrow" {
				d = d.AddDate(0, 0, 1)
			}
			if lower == "yesterday" {
				d = d.AddDate(0, 0, -1)
			}
			newTime = d
			resultHasTime = false
		} else {
			parts2 := strings.Fields(lower)
			dayNames := map[string]time.Weekday{
				"sun": time.Sunday, "sunday": time.Sunday,
				"mon": time.Monday, "monday": time.Monday,
				"tue": time.Tuesday, "tuesday": time.Tuesday,
				"wed": time.Wednesday, "wednesday": time.Wednesday,
				"thu": time.Thursday, "thursday": time.Thursday,
				"fri": time.Friday, "friday": time.Friday,
				"sat": time.Saturday, "saturday": time.Saturday,
			}
			if len(parts2) >= 1 {
				// Support 'today', 'tomorrow', 'yesterday' with optional time, e.g. 'tomorrow 1pm'
				if parts2[0] == "today" || parts2[0] == "tomorrow" || parts2[0] == "yesterday" {
					var baseMid time.Time
					if svc.logicalCalc != nil {
						baseMid = svc.logicalCalc.Today().In(loc)
					} else {
						now := time.Now().In(loc)
						baseMid = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
					}
					switch parts2[0] {
					case "tomorrow":
						baseMid = baseMid.AddDate(0, 0, 1)
					case "yesterday":
						baseMid = baseMid.AddDate(0, 0, -1)
					case "today":
						// no-op
					}
					if len(parts2) > 1 {
						timePart := strings.Join(parts2[1:], " ")
						if t, ok := parseTimeStringLocal(timePart, baseMid, loc); ok {
							newTime = t
							resultHasTime = true
						} else {
							newTime = baseMid
							resultHasTime = false
						}
					} else {
						newTime = baseMid
						resultHasTime = false
					}
				} else if dwd, ok := dayNames[parts2[0]]; ok {
					var baseMid time.Time
					if svc.logicalCalc != nil {
						baseMid = svc.logicalCalc.Today().In(loc)
					} else {
						now := time.Now().In(loc)
						baseMid = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
					}
					cur := int(baseMid.Weekday())
					target := int(dwd)
					delta := (target - cur + 7) % 7
					// Treat same-day as next week to ensure 'future' weekday (e.g., 'wed' means next Wednesday)
					if delta == 0 {
						delta = 7
					}
					date := baseMid.AddDate(0, 0, delta)
					if len(parts2) > 1 {
						timePart := strings.Join(parts2[1:], " ")
						if t, ok := parseTimeStringLocal(timePart, date, loc); ok {
							newTime = t
							resultHasTime = true
						} else {
							newTime = date
							resultHasTime = false
						}
					} else {
						newTime = date
						resultHasTime = false
					}
				} else {
					// try time-only parse
					if t, ok := parseTimeStringLocal(lower, base, loc); ok {
						newTime = t
						resultHasTime = true
					} else if parsed, err := time.ParseInLocation(time.RFC3339, expr, loc); err == nil {
						newTime = parsed
						resultHasTime = true
					} else if parsed, err := time.ParseInLocation("2006-01-02 15:04", expr, loc); err == nil {
						newTime = parsed
						resultHasTime = true
					} else {
						return nil, fmt.Errorf("unable to parse schedule expression: %s", expr)
					}
				}
			}
		}

		scheduledAtParam := newTime.UTC().Format(time.RFC3339)
		if _, err := svc.updateTask(taskID, map[string]any{"scheduledAt": scheduledAtParam, "hasScheduledTime": resultHasTime}); err != nil {
			return nil, err
		}

		// Return updated TaskRow if possible
		if t, _ := findUpdatedTask(); t != nil {
			messenger := svc.Deps.MustGetMessenger()
			if messenger != nil {
				msg := core.Message{Version: "1.0", Timestamp: time.Now(), ChannelName: "tasks", ServiceType: "tasks", ServiceName: "tasks", Event: "taskUpdated", Status: "updated"}
				bodyMap := map[string]any{"taskId": taskID}
				if t.ScheduledAt != "" {
					bodyMap["scheduledAt"] = t.ScheduledAt
				}
				if b, err := json.Marshal(bodyMap); err == nil {
					msg.Body = string(b)
				}
				_ = messenger.Send(msg)
			}
			return t, nil
		}

		return map[string]any{"status": "ok"}, nil

	case "skip", "s":
		// Advance a recurring task to its next occurrence in-place (do not mark done or create a new task)
		// Load current scheduled_date, scheduled_time and recurrence fields
		var scheduledDate sql.NullString
		var scheduledTime sql.NullBool
		var recurrenceType sql.NullString
		var recurrenceDays sql.NullString
		var recurrenceInterval sql.NullInt64
		if err := svc.db.QueryRow("SELECT scheduled_date, scheduled_time, recurrence, recurrence_days, recurrence_x FROM tasks WHERE id = ?", taskID).Scan(&scheduledDate, &scheduledTime, &recurrenceType, &recurrenceDays, &recurrenceInterval); err != nil {
			return nil, err
		}

		if !recurrenceType.Valid || strings.TrimSpace(recurrenceType.String) == "" {
			return nil, fmt.Errorf("task is not recurring")
		}

		pData := parsePatternData(recurrenceType.String, recurrenceDays.String, int(recurrenceInterval.Int64))
		if pData == nil {
			return nil, fmt.Errorf("invalid recurrence pattern")
		}

		rp := &recurrence.Pattern{
			Type:       pData.Type,
			Interval:   pData.Interval,
			DayOfMonth: pData.DayOfMonth,
			Month:      time.Month(pData.Month),
		}
		for _, d := range pData.DaysOfWeek {
			rp.DaysOfWeek = append(rp.DaysOfWeek, time.Weekday(d))
		}

		loc := time.Local
		if svc.logicalCalc != nil && svc.logicalCalc.Location() != nil {
			loc = svc.logicalCalc.Location()
		}

		var base time.Time
		var hasTime bool
		if scheduledDate.Valid && scheduledDate.String != "" {
			if t, err := time.Parse(time.RFC3339Nano, scheduledDate.String); err == nil {
				base = t.In(loc)
			} else if t2, err2 := time.Parse(time.RFC3339, scheduledDate.String); err2 == nil {
				base = t2.In(loc)
			} else {
				base = time.Now().In(loc)
			}
			hasTime = scheduledTime.Valid && scheduledTime.Bool
		} else {
			if svc.logicalCalc != nil {
				base = svc.logicalCalc.Today().In(loc)
			} else {
				base = time.Now().In(loc)
			}
			hasTime = false
		}

		var origHour, origMin, origSec int
		if hasTime {
			origHour, origMin, origSec = base.In(time.Local).Clock()
		}

		if svc.logicalCalc != nil {
			base = svc.logicalCalc.GetLogicalDay(base, hasTime)
		} else {
			base = base.UTC()
		}

		next := rp.Next(base)
		if next.IsZero() {
			return nil, fmt.Errorf("unable to compute next recurrence")
		}

		var scheduledAtParam string
		var resultHasTime bool
		if hasTime {
			locApply := time.Local
			if svc.logicalCalc != nil && svc.logicalCalc.Location() != nil {
				locApply = svc.logicalCalc.Location()
			}
			nextWithTime := time.Date(next.Year(), next.Month(), next.Day(), origHour, origMin, origSec, 0, locApply).UTC()
			scheduledAtParam = nextWithTime.Format(time.RFC3339)
			resultHasTime = true
		} else {
			locApply := time.Local
			if svc.logicalCalc != nil && svc.logicalCalc.Location() != nil {
				locApply = svc.logicalCalc.Location()
			}
			nextMidnight := time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, locApply).UTC()
			scheduledAtParam = nextMidnight.Format(time.RFC3339)
			resultHasTime = false
		}

		if _, err := svc.updateTask(taskID, map[string]any{"scheduledAt": scheduledAtParam, "hasScheduledTime": resultHasTime}); err != nil {
			return nil, err
		}

		// Return updated TaskRow if possible
		if t, _ := findUpdatedTask(); t != nil {
			messenger := svc.Deps.MustGetMessenger()
			if messenger != nil {
				msg := core.Message{Version: "1.0", Timestamp: time.Now(), ChannelName: "tasks", ServiceType: "tasks", ServiceName: "tasks", Event: "taskUpdated", Status: "updated"}
				bodyMap := map[string]any{"taskId": taskID}
				if t.ScheduledAt != "" {
					bodyMap["scheduledAt"] = t.ScheduledAt
				}
				if b, err := json.Marshal(bodyMap); err == nil {
					msg.Body = string(b)
				}
				_ = messenger.Send(msg)
			}
			return t, nil
		}

		return map[string]any{"status": "ok"}, nil
	case "t", "tag":
		argText := strings.TrimSpace(args)
		if argText == "" {
			return nil, fmt.Errorf("tag required")
		}
		// Parse tokens (support multiple tags and removals like "t -foo")
		tokens := strings.Fields(argText)
		// Load current tags
		var currentTags sql.NullString
		if err := svc.db.QueryRow("SELECT COALESCE(tags, '') FROM tasks WHERE id = ?", taskID).Scan(&currentTags); err != nil {
			return nil, err
		}
		origOrder := []string{}
		if currentTags.Valid && currentTags.String != "" {
			for _, tt := range strings.Split(currentTags.String, ",") {
				tt = strings.TrimSpace(tt)
				if tt != "" {
					origOrder = append(origOrder, tt)
				}
			}
		}
		existing := map[string]bool{}
		for _, t := range origOrder {
			existing[t] = true
		}
		changed := false
		addedOrder := []string{}
		for _, tok := range tokens {
			if tok == "" {
				continue
			}
			if strings.HasPrefix(tok, "-") && len(tok) > 1 {
				name := strings.TrimSpace(tok[1:])
				if name == "" {
					continue
				}
				if existing[name] {
					delete(existing, name)
					changed = true
				}
			} else {
				name := strings.TrimSpace(tok)
				if name == "" {
					continue
				}
				if !existing[name] {
					existing[name] = true
					addedOrder = append(addedOrder, name)
					changed = true
				}
			}
		}
		if !changed {
			if t, _ := findUpdatedTask(); t != nil {
				return t, nil
			}
			return map[string]any{"status": "ok", "tags": currentTags.String}, nil
		}
		// Build final tag list: keep original order for remaining tags, then append newly added tags
		finalTags := []string{}
		seen := map[string]bool{}
		for _, t := range origOrder {
			if existing[t] {
				finalTags = append(finalTags, t)
				seen[t] = true
			}
		}
		for _, t := range addedOrder {
			if !seen[t] && existing[t] {
				finalTags = append(finalTags, t)
				seen[t] = true
			}
		}
		newTagsCSV := strings.Join(finalTags, ",")
		if _, err := svc.updateTask(taskID, map[string]any{"tags": newTagsCSV}); err != nil {
			return nil, err
		}
		if t, _ := findUpdatedTask(); t != nil {
			messenger := svc.Deps.MustGetMessenger()
			if messenger != nil {
				msg := core.Message{Version: "1.0", Timestamp: time.Now(), ChannelName: "tasks", ServiceType: "tasks", ServiceName: "tasks", Event: "taskUpdated", Status: "updated"}
				bodyMap := map[string]any{"taskId": taskID}
				if t.Tags != "" {
					bodyMap["tags"] = t.Tags
				} else {
					bodyMap["tags"] = ""
				}
				if b, err := json.Marshal(bodyMap); err == nil {
					msg.Body = string(b)
				}
				_ = messenger.Send(msg)
			}
			return t, nil
		}
		return map[string]any{"status": "ok", "tags": newTagsCSV}, nil
	default:
		return nil, fmt.Errorf("unknown command: %s", cmd)
	}
}
