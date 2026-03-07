// Package macosReminders integrates with the macOS Reminders app to fetch and emit reminder events and changes.
package macosReminders

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"keyop/core"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Service polls macOS Reminders (a specific list, default "Inbox") and publishes reminder
// information to the configured "task" pub channel.
// It relies on the Swift helper (EventKit) to fetch reminders as JSON lines.

// Service periodically reads reminders from macOS and publishes create/update/delete events for configured lists.
type Service struct {
	Deps            core.Dependencies
	Cfg             core.ServiceConfig
	inboxName       string
	onlyUncompleted bool
	helperPath      string // configurable path to the reminders_fetcher binary
}

// Task is the structured payload sent in core.Message.Data for each reminder
type Task struct {
	ID             int64     `json:"id"`
	Uuid           string    `json:"uuid"`
	Title          string    `json:"title"`
	Done           bool      `json:"done"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	Importance     int       `json:"importance"`
	Flag           bool      `json:"flag"`
	Color          string    `json:"color"`
	Note           string    `json:"note"`
	DueDate        time.Time `json:"dueDate"`
	ScheduledDate  time.Time `json:"scheduledDate"`
	ScheduledTime  bool      `json:"scheduledTime"`
	Tags           string    `json:"tags"`
	Recurrence     string    `json:"recurrence"`
	RecurrenceDays string    `json:"recurrenceDays"`
	RecurrenceX    int       `json:"recurrenceX"`
	CompletedAt    time.Time `json:"completedAt"`
}

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	var err []error

	// helper_path is optional but if present must be a non-empty string
	if v, ok := svc.Cfg.Config["helper_path"]; ok {
		if s, ok2 := v.(string); !ok2 || strings.TrimSpace(s) == "" {
			e := fmt.Errorf("config 'helper_path' must be a non-empty string if provided")
			logger.Error(e.Error())
			err = append(err, e)
		} else {
			// If helper_path provided, ensure it exists and is executable
			if fi, statErr := os.Stat(s); statErr != nil {
				e := fmt.Errorf("configured helper_path '%s' does not exist or is not accessible: %v", s, statErr)
				logger.Error(e.Error())
				err = append(err, e)
			} else {
				mode := fi.Mode()
				// Check executable bit for owner/group/other
				if mode&0111 == 0 {
					e := fmt.Errorf("configured helper_path '%s' is not executable (missing executable permission)", s)
					logger.Error(e.Error())
					err = append(err, e)
				}
			}
		}
	}

	inbox, _ := svc.Cfg.Config["inbox_name"].(string)
	if inbox == "" {
		logger.Warn("config 'inbox_name' not provided, defaulting to 'Inbox'")
	}

	return err
}

// Initialize performs one-time startup required by the service (resource loading or connectivity checks).
func (svc *Service) Initialize() error {
	// read config defaults
	if v, ok := svc.Cfg.Config["inbox_name"].(string); ok && v != "" {
		svc.inboxName = v
	} else {
		svc.inboxName = "Inbox"
	}

	if v, ok := svc.Cfg.Config["only_uncompleted"].(bool); ok {
		svc.onlyUncompleted = v
	} else {
		svc.onlyUncompleted = true
	}

	if v, ok := svc.Cfg.Config["helper_path"].(string); ok && v != "" {
		svc.helperPath = v
	}

	return nil
}

// Check performs the service's periodic work: collect data, evaluate state, and publish messages/metrics.
func (svc *Service) Check() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("macosReminders service is only supported on macOS")
	}

	svc.fetchAndPublish()
	return nil
}

// Local type stored in state store to track last-seen reminders
type reminderState struct {
	Title     string    `json:"title"`
	Completed bool      `json:"completed"`
	DueRaw    string    `json:"due_raw"`
	SeenAt    time.Time `json:"seen_at"`
}

// helper to run the swift binary reminders_fetcher and return its stdout (or error)
func (svc *Service) runSwiftFetcher() ([]byte, error) {
	logger := svc.Deps.MustGetLogger()
	osProvider := svc.Deps.MustGetOsProvider()

	// If a helper path is configured, prefer it
	if svc.helperPath != "" {
		// quick existence check for explicit path
		if strings.Contains(svc.helperPath, "/") {
			if _, err := os.Stat(svc.helperPath); os.IsNotExist(err) {
				return nil, fmt.Errorf("configured helper_path '%s' does not exist", svc.helperPath)
			}
		}
		cmd := osProvider.Command(svc.helperPath, "--timeout", "10", "--list", svc.inboxName)
		if svc.onlyUncompleted {
			cmd = osProvider.Command(svc.helperPath, "--timeout", "10", "--list", svc.inboxName, "--only-uncompleted")
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			logger.Error("configured swift fetcher failed", "path", svc.helperPath, "error", err, "output", string(out))
			return nil, err
		}
		return out, nil
	}

	// Fallback: search common locations (sibling to running executable, ./reminders_fetcher, PATH)
	candidates := []string{"./reminders_fetcher", "reminders_fetcher"}
	if exePath, err := os.Executable(); err == nil {
		sibling := fmt.Sprintf("%s/reminders_fetcher", pathDir(exePath))
		candidates = append([]string{sibling}, candidates...)
	} else {
		logger.Debug("could not determine executable path", "error", err)
	}

	args := []string{"--timeout", "10", "--list", svc.inboxName}
	if svc.onlyUncompleted {
		args = append(args, "--only-uncompleted")
	}

	for _, cmdPath := range candidates {
		// quick existence check when path looks like a relative path
		if strings.Contains(cmdPath, "/") {
			if _, err := os.Stat(cmdPath); os.IsNotExist(err) {
				continue
			}
		}
		cmd := osProvider.Command(cmdPath, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			logger.Debug("swift fetcher failed", "cmd", cmdPath, "error", err, "output", string(out))
			continue
		}
		return out, nil
	}

	return nil, fmt.Errorf("swift reminders_fetcher not found or failed")
}

// parse JSON lines from reader and return a map of key->reminderState and an ordered slice of created messages
func parseSwiftJSONLines(r io.Reader, onlyUncompleted bool, inboxName string) (map[string]reminderState, []core.Message, error) {
	dec := json.NewDecoder(r)
	curr := make(map[string]reminderState)
	var created []core.Message

	for {
		var obj map[string]interface{}
		if err := dec.Decode(&obj); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, nil, err
		}

		// Read common fields
		idStr := ""
		if v, ok := obj["id"].(string); ok {
			idStr = v
		} else if v := obj["id"]; v != nil {
			idStr = fmt.Sprintf("%v", v)
		}

		title, _ := obj["title"].(string)
		// helper may use "note" key
		note := ""
		if v, ok := obj["note"].(string); ok {
			note = v
		} else if v, ok := obj["notes"].(string); ok {
			note = v
		}
		completed, _ := obj["done"].(bool)
		if !completed {
			// some helpers may use "completed" boolean or string
			if v, ok := obj["completed"].(bool); ok {
				completed = v
			}
		}
		dueStr, _ := obj["due"].(string)

		key := idStr
		if strings.TrimSpace(key) == "" {
			key = fmt.Sprintf("anon:%s:%s", title, dueStr)
		}

		curr[key] = reminderState{Title: title, Completed: completed, DueRaw: dueStr, SeenAt: time.Now()}

		if onlyUncompleted && completed {
			continue
		}

		// Map additional fields into Task
		task := Task{}
		// Try to parse numeric ID
		if idStr != "" {
			if n, err := strconv.ParseInt(idStr, 10, 64); err == nil {
				task.ID = n
			}
		}
		// generate a stable UUID for this message
		task.Uuid = uuid.New().String()
		task.Title = title
		task.Note = note
		task.Done = completed

		// parse timestamps if available
		if createdStr, ok := obj["created"].(string); ok && createdStr != "" {
			task.CreatedAt = parseDate(createdStr)
		} else {
			task.CreatedAt = time.Now()
		}
		if updatedStr, ok := obj["updated"].(string); ok && updatedStr != "" {
			task.UpdatedAt = parseDate(updatedStr)
		} else {
			task.UpdatedAt = task.CreatedAt
		}
		if dueStr != "" {
			task.DueDate = parseDate(dueStr)
		}
		// Set ScheduledDate from the reminder due date (use DueDate) instead of relying on a separate 'scheduled' field
		if dueStr != "" {
			task.ScheduledDate = parseDate(dueStr)
		}
		if compStr, ok := obj["completedAt"].(string); ok && compStr != "" {
			task.CompletedAt = parseDate(compStr)
		}

		// priority/importance
		if v, ok := obj["priority"]; ok && v != nil {
			switch t := v.(type) {
			case float64:
				task.Importance = int(t)
			case int:
				task.Importance = t
			default:
				// try to parse as string
				if s := fmt.Sprintf("%v", v); s != "" {
					if n, err := strconv.Atoi(s); err == nil {
						task.Importance = n
					}
				}
			}
		}

		// recurrence
		if v, ok := obj["recurrence"].(string); ok {
			task.Recurrence = v
		}

		// recurrenceDays and recurrenceX
		if v, ok := obj["recurrenceDays"].(string); ok {
			task.RecurrenceDays = v
		}
		if v, ok := obj["recurrenceX"]; ok {
			switch t := v.(type) {
			case float64:
				task.RecurrenceX = int(t)
			case int:
				task.RecurrenceX = t
			default:
				if s := fmt.Sprintf("%v", v); s != "" {
					if n, err := strconv.Atoi(s); err == nil {
						task.RecurrenceX = n
					}
				}
			}
		}

		// flag
		if v, ok := obj["flag"]; ok {
			if b2, ok3 := v.(bool); ok3 {
				task.Flag = b2
			} else {
				// try parse as string
				if s := fmt.Sprintf("%v", v); s != "" {
					if s == "true" || s == "1" {
						task.Flag = true
					}
				}
			}
		}

		// other optional fields
		if v, ok := obj["tags"].(string); ok {
			task.Tags = v
		}
		if v, ok := obj["color"].(string); ok {
			task.Color = v
		}

		m := core.Message{
			ChannelName: "",
			ServiceName: "",
			ServiceType: "",
			Summary:     title,
			Text:        note,
			Data: map[string]interface{}{
				"id":        idStr,
				"title":     title,
				"note":      note,
				"due_raw":   dueStr,
				"completed": completed,
				"inbox":     inboxName,
				"task":      task,
			},
		}
		created = append(created, m)
	}

	return curr, created, nil
}

// fetchAndPublish uses just the Swift helper and fails if it isn't available
func (svc *Service) fetchAndPublish() {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()
	state := svc.Deps.MustGetStateStore()

	logger.Warn("MACOS REMINDERS: fetchAndPublish started (swift helper only)")

	stateKey := fmt.Sprintf("macosReminders_%s", svc.Cfg.Name)
	var prev map[string]reminderState
	if err := state.Load(stateKey, &prev); err != nil {
		logger.Error("failed to load state", "error", err)
		prev = make(map[string]reminderState)
	}
	if prev == nil {
		prev = make(map[string]reminderState)
	}

	out, err := svc.runSwiftFetcher()
	if err != nil {
		logger.Error("swift reminders_fetcher not found or failed", "error", err)
		return
	}

	curr, createdMsgs, err := parseSwiftJSONLines(strings.NewReader(string(out)), svc.onlyUncompleted, svc.inboxName)
	if err != nil {
		logger.Error("failed to parse swift fetcher output", "error", err)
		return
	}

	// send created messages (dedupe against prev)
	for _, m := range createdMsgs {
		id, _ := m.Data.(map[string]interface{})["id"].(string)
		dueRaw, _ := m.Data.(map[string]interface{})["due_raw"].(string)
		key := id
		if strings.TrimSpace(key) == "" {
			key = fmt.Sprintf("anon:%s:%s", m.Summary, dueRaw)
		}
		if _, seen := prev[key]; !seen {
			m.ChannelName = svc.Cfg.Name
			m.ServiceName = svc.Cfg.Name
			m.ServiceType = svc.Cfg.Type
			m.Event = "reminder_created"
			if data, ok := m.Data.(map[string]interface{}); ok {
				data["event"] = "created"
				// set user_id from config, default to 1
				var userID int64 = 1
				if v, ok2 := svc.Cfg.Config["user_id"]; ok2 {
					switch t := v.(type) {
					case int:
						userID = int64(t)
					case int64:
						userID = t
					case float64:
						userID = int64(t)
					case string:
						if n, err := strconv.ParseInt(t, 10, 64); err == nil {
							userID = n
						}
					}
				}
				data["user_id"] = userID
			}
			if err := messenger.Send(m); err != nil {
				logger.Error("failed to send reminder message", "error", err, "reminder", m.Summary)
			}
		}
	}

	// Detect removals
	for k, prevState := range prev {
		if _, present := curr[k]; !present {
			msg := core.Message{
				ChannelName: svc.Cfg.Name,
				ServiceName: svc.Cfg.Name,
				ServiceType: svc.Cfg.Type,
				Event:       "reminder_removed",
				Summary:     prevState.Title,
				Text:        "(removed)",
				Data: map[string]interface{}{
					"id":    k,
					"title": prevState.Title,
					"event": "removed",
				},
			}
			if err := messenger.Send(msg); err != nil {
				logger.Error("failed to send reminder removal message", "error", err, "reminder", prevState.Title)
			}
		}
	}

	// Save current state
	if err := state.Save(stateKey, curr); err != nil {
		logger.Error("failed to save state", "error", err)
	}
}

// small helper to get directory of path without importing path/filepath in too many places
func pathDir(p string) string {
	if p == "" {
		return ""
	}
	// find last separator
	last := strings.LastIndex(p, string(os.PathSeparator))
	if last == -1 {
		return "."
	}
	return p[:last]
}

// parse date string in various formats and return time.Time
func parseDate(dateStr string) time.Time {
	// Try parsing as RFC3339
	if t, err := time.Parse(time.RFC3339, dateStr); err == nil {
		return t
	}
	// Try parsing as Unix timestamp
	if ts, err := strconv.ParseInt(dateStr, 10, 64); err == nil {
		return time.Unix(ts, 0)
	}
	// Fallback: return zero time
	return time.Time{}
}
