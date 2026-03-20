package flashcards

import (
	"database/sql"
	"fmt"
	"keyop/core"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite" // sqlite driver
)

// Service implements the flashcards service.
type Service struct {
	Deps   core.Dependencies
	Cfg    core.ServiceConfig
	db     *sql.DB
	dbPath string
}

// NewService constructs the flashcards service.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	dbPath := "~/.keyop/sqlite/flashcards.sql"
	if p, ok := cfg.Config["db_path"].(string); ok && p != "" {
		dbPath = p
	}
	return &Service{
		Deps:   deps,
		Cfg:    cfg,
		dbPath: dbPath,
	}
}

// Check implements core.Service.
func (svc *Service) Check() error {
	if svc.db == nil {
		return fmt.Errorf("flashcards database not initialized")
	}
	var count int
	if err := svc.db.QueryRow("SELECT COUNT(*) FROM flashcards").Scan(&count); err != nil {
		return fmt.Errorf("flashcards database check failed: %w", err)
	}
	return nil
}

// ValidateConfig implements core.Service.
func (svc *Service) ValidateConfig() []error { return nil }

// Initialize opens the flashcards database and creates the schema.
func (svc *Service) Initialize() error {
	dbPath := svc.dbPath
	if strings.HasPrefix(dbPath, "~/") {
		home, err := svc.Deps.MustGetOsProvider().UserHomeDir()
		if err != nil {
			return fmt.Errorf("flashcards: failed to get home dir: %w", err)
		}
		dbPath = filepath.Join(home, dbPath[2:])
	}

	if err := svc.Deps.MustGetOsProvider().MkdirAll(filepath.Dir(dbPath), 0750); err != nil {
		return fmt.Errorf("flashcards: failed to create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("flashcards: failed to open database: %w", err)
	}
	if err := db.Ping(); err != nil {
		return fmt.Errorf("flashcards: failed to ping database: %w", err)
	}
	svc.db = db

	return svc.initSchema()
}

// OnShutdown closes the database.
func (svc *Service) OnShutdown() error {
	if svc.db != nil {
		return svc.db.Close()
	}
	return nil
}

func (svc *Service) initSchema() error {
	_, err := svc.db.Exec(`
		CREATE TABLE IF NOT EXISTS flashcards (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			uuid         TEXT UNIQUE NOT NULL,
			question     TEXT NOT NULL,
			answer       TEXT NOT NULL,
			tags         TEXT NOT NULL DEFAULT '',
			ease_factor  REAL NOT NULL DEFAULT 2.5,
			interval     INTEGER NOT NULL DEFAULT 0,
			repetitions  INTEGER NOT NULL DEFAULT 0,
			due_date     DATETIME,
			card_state   TEXT NOT NULL DEFAULT 'new',
			current_step INTEGER NOT NULL DEFAULT 0,
			created_at   DATETIME NOT NULL,
			updated_at   DATETIME NOT NULL
		)
	`)
	if err != nil {
		return err
	}
	// Migrate older DBs that predate card_state/current_step columns.
	for _, col := range []string{
		"ALTER TABLE flashcards ADD COLUMN card_state TEXT NOT NULL DEFAULT 'new'",
		"ALTER TABLE flashcards ADD COLUMN current_step INTEGER NOT NULL DEFAULT 0",
	} {
		_, _ = svc.db.Exec(col) // ignore "duplicate column" errors
	}
	// Cards that already have repetitions > 0 but no card_state set are graduated review cards.
	_, err = svc.db.Exec(`UPDATE flashcards SET card_state='review' WHERE card_state='new' AND repetitions > 0`)
	return err
}

// createCard inserts a new flashcard.
func (svc *Service) createCard(question, answer, tags string) (any, error) {
	if svc.db == nil {
		return nil, fmt.Errorf("flashcards database not available")
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("question cannot be empty")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := svc.db.Exec(
		`INSERT INTO flashcards (uuid, question, answer, tags, ease_factor, interval, repetitions, due_date, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 2.5, 0, 0, ?, ?, ?)`,
		uuid.New().String(), question, answer, normalizeTags(tags),
		now, now, now, // due immediately
	)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	return map[string]any{"status": "ok", "id": id}, nil
}

// listDue returns cards due now (or overdue), optionally filtered by tag.
func (svc *Service) listDue(tag string) (any, error) {
	if svc.db == nil {
		return nil, fmt.Errorf("flashcards database not available")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	var rows *sql.Rows
	var err error

	if tag == "" || tag == "all" {
		rows, err = svc.db.Query(`
			SELECT id, uuid, question, answer, tags, ease_factor, interval, repetitions, due_date
			FROM flashcards
			WHERE due_date IS NULL OR due_date <= ?
			ORDER BY COALESCE(due_date, '0001-01-01') ASC
		`, now)
	} else {
		rows, err = svc.db.Query(`
			SELECT id, uuid, question, answer, tags, ease_factor, interval, repetitions, due_date
			FROM flashcards
			WHERE (due_date IS NULL OR due_date <= ?)
			  AND (',' || tags || ',' LIKE '%,' || ? || ',%')
			ORDER BY COALESCE(due_date, '0001-01-01') ASC
		`, now, tag)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	type CardRow struct {
		ID          int64   `json:"id"`
		UUID        string  `json:"uuid"`
		Question    string  `json:"question"`
		Answer      string  `json:"answer"`
		Tags        string  `json:"tags"`
		EaseFactor  float64 `json:"easeFactor"`
		Interval    int64   `json:"interval"`
		Repetitions int64   `json:"repetitions"`
		DueDate     string  `json:"dueDate"`
	}

	var cards []CardRow
	for rows.Next() {
		var c CardRow
		var dueDate sql.NullString
		if err := rows.Scan(&c.ID, &c.UUID, &c.Question, &c.Answer, &c.Tags,
			&c.EaseFactor, &c.Interval, &c.Repetitions, &dueDate); err != nil {
			return nil, err
		}
		if dueDate.Valid {
			c.DueDate = dueDate.String
		}
		cards = append(cards, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if cards == nil {
		cards = []CardRow{}
	}
	return map[string]any{"cards": cards}, nil
}

// listTags returns all unique tags with card counts (due + total).
func (svc *Service) listTags() (any, error) {
	if svc.db == nil {
		return nil, fmt.Errorf("flashcards database not available")
	}

	rows, err := svc.db.Query(`SELECT tags FROM flashcards`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	totals := map[string]int{}
	for rows.Next() {
		var tags string
		if err := rows.Scan(&tags); err != nil {
			return nil, err
		}
		for _, t := range splitTags(tags) {
			totals[t]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Count due per tag
	dueRows, err := svc.db.Query(`SELECT tags FROM flashcards WHERE due_date IS NULL OR due_date <= ?`, now)
	if err != nil {
		return nil, err
	}
	defer func() { _ = dueRows.Close() }()

	due := map[string]int{}
	for dueRows.Next() {
		var tags string
		if err := dueRows.Scan(&tags); err != nil {
			return nil, err
		}
		for _, t := range splitTags(tags) {
			due[t]++
		}
	}
	if err := dueRows.Err(); err != nil {
		return nil, err
	}

	type TagInfo struct {
		Tag   string `json:"tag"`
		Total int    `json:"total"`
		Due   int    `json:"due"`
	}
	var result []TagInfo
	for tag, total := range totals {
		result = append(result, TagInfo{Tag: tag, Total: total, Due: due[tag]})
	}

	// Count all-cards totals directly to avoid double-counting multi-tag cards
	var allTotal, allDue int
	_ = svc.db.QueryRow(`SELECT COUNT(*) FROM flashcards`).Scan(&allTotal)
	_ = svc.db.QueryRow(`SELECT COUNT(*) FROM flashcards WHERE due_date IS NULL OR due_date <= ?`, now).Scan(&allDue)

	return map[string]any{"tags": result, "allTotal": allTotal, "allDue": allDue}, nil
}

// previewSchedule returns the projected due date for each rating option without modifying the card.
func (svc *Service) previewSchedule(id int64) (any, error) {
	if svc.db == nil {
		return nil, fmt.Errorf("flashcards database not available")
	}
	var ef float64
	var interval, reps int64
	var cardState string
	var currentStep int64
	err := svc.db.QueryRow(
		`SELECT ease_factor, interval, repetitions, card_state, current_step FROM flashcards WHERE id = ?`, id,
	).Scan(&ef, &interval, &reps, &cardState, &currentStep)
	if err != nil {
		return nil, fmt.Errorf("flashcards: card %d not found: %w", id, err)
	}
	result := map[string]string{}
	for _, rating := range []string{"show_again", "hard", "correct", "easy"} {
		dueDate, _, _, _, _, _ := scheduleAnki(ef, interval, reps, cardState, currentStep, rating)
		result[rating] = dueDate
	}
	return result, nil
}

// reviewCard applies Anki-style scheduling and updates the card.
// rating must be one of: "show_again", "hard", "correct", "easy"
func (svc *Service) reviewCard(id int64, rating string) (any, error) {
	if svc.db == nil {
		return nil, fmt.Errorf("flashcards database not available")
	}

	var ef float64
	var interval, reps int64
	var cardState string
	var currentStep int64
	err := svc.db.QueryRow(
		`SELECT ease_factor, interval, repetitions, card_state, current_step FROM flashcards WHERE id = ?`, id,
	).Scan(&ef, &interval, &reps, &cardState, &currentStep)
	if err != nil {
		return nil, fmt.Errorf("flashcards: card %d not found: %w", id, err)
	}

	dueDate, ef, interval, reps, cardState, currentStep := scheduleAnki(ef, interval, reps, cardState, currentStep, rating)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = svc.db.Exec(
		`UPDATE flashcards SET ease_factor=?, interval=?, repetitions=?, due_date=?, card_state=?, current_step=?, updated_at=? WHERE id=?`,
		ef, interval, reps, dueDate, cardState, currentStep, now, id,
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok", "nextDue": dueDate, "interval": interval}, nil
}

// deleteCard removes a flashcard by id.
func (svc *Service) deleteCard(id int64) (any, error) {
	if svc.db == nil {
		return nil, fmt.Errorf("flashcards database not available")
	}
	_, err := svc.db.Exec(`DELETE FROM flashcards WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok"}, nil
}

// updateCard updates the question, answer, and tags of an existing card.
func (svc *Service) updateCard(id int64, question, answer, tags string) (any, error) {
	if svc.db == nil {
		return nil, fmt.Errorf("flashcards database not available")
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("question cannot be empty")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := svc.db.Exec(
		`UPDATE flashcards SET question = ?, answer = ?, tags = ?, updated_at = ? WHERE id = ?`,
		question, answer, normalizeTags(tags), now, id,
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok"}, nil
}

// scheduleAnki implements Anki's phase-based scheduling:
//
//	New/Learning phases use minute-based steps: [1min, 10min].
//	Review phase uses day-based intervals aligned to local midnight.
//	Relearning phase (lapsed review card) uses a 10-minute step before returning to review.
//
// Card states: "new" → "learning" → "review" ↔ "relearning"
//
// Returns: dueDate (RFC3339Nano UTC), and updated ef/interval/reps/cardState/currentStep.
func scheduleAnki(ef float64, interval, reps int64, cardState string, currentStep int64, rating string) (
	dueDate string, newEF float64, newInterval, newReps int64, newState string, newStep int64,
) {
	// Learning steps in minutes (standard Anki defaults).
	const (
		learnStep0   = 1 * time.Minute  // first step
		learnStep1   = 10 * time.Minute // second / final step
		relearnStep  = 10 * time.Minute // single relearn step after lapse
		gradInterval = 1                // days after completing learning
		easyInterval = 4                // days for Easy on new/learning card
	)

	now := time.Now()
	newEF, newInterval, newReps, newState, newStep = ef, interval, reps, cardState, currentStep

	midnightInDays := func(days int) string {
		loc := time.Local
		future := now.In(loc).AddDate(0, 0, days)
		due := time.Date(future.Year(), future.Month(), future.Day(), 0, 0, 0, 0, loc)
		return due.UTC().Format(time.RFC3339Nano)
	}
	inMinutes := func(d time.Duration) string {
		return now.Add(d).UTC().Format(time.RFC3339Nano)
	}
	clampEF := func(v float64) float64 {
		if v < 1.3 {
			return 1.3
		}
		return v
	}

	switch cardState {
	case "new", "learning":
		newState = "learning"
		switch rating {
		case "show_again":
			newStep = 0
			dueDate = inMinutes(learnStep0)
		case "hard":
			// Anki hard during learning = average of current step and next step.
			// Step 0: (1min + 10min) / 2 = 5.5min → 5min
			// Step 1 (final): no next step, stay at step 1 duration
			if currentStep == 0 {
				dueDate = inMinutes((learnStep0 + learnStep1) / 2)
			} else {
				dueDate = inMinutes(learnStep1)
			}
		case "correct":
			if currentStep == 0 {
				// Advance to step 1
				newStep = 1
				dueDate = inMinutes(learnStep1)
			} else {
				// Completed all steps → graduate to review
				newState = "review"
				newStep = 0
				newReps = reps + 1
				newInterval = gradInterval
				dueDate = midnightInDays(gradInterval)
			}
		case "easy":
			// Skip all steps, graduate immediately
			newState = "review"
			newStep = 0
			newReps = reps + 1
			newInterval = easyInterval
			dueDate = midnightInDays(easyInterval)
		}

	case "review":
		switch rating {
		case "show_again":
			// Lapse: enter relearning, penalise ease factor, shrink interval
			newEF = clampEF(ef - 0.20)
			newInterval = max64(1, int64(math.Round(float64(interval)*0.0))) // resets to 1 after relearn
			if newInterval < 1 {
				newInterval = 1
			}
			newState = "relearning"
			newStep = 0
			dueDate = inMinutes(relearnStep)
		case "hard":
			newEF = clampEF(ef - 0.15)
			newInterval = max64(interval+1, int64(math.Round(float64(interval)*1.2)))
			dueDate = midnightInDays(int(newInterval))
		case "correct":
			newInterval = max64(interval+1, int64(math.Round(float64(interval)*ef)))
			dueDate = midnightInDays(int(newInterval))
		case "easy":
			newEF = clampEF(ef + 0.15)
			newInterval = max64(interval+1, int64(math.Round(float64(interval)*ef*1.3)))
			dueDate = midnightInDays(int(newInterval))
		}
		newReps = reps + 1

	case "relearning":
		switch rating {
		case "show_again", "hard":
			// Repeat the single relearn step
			newStep = 0
			dueDate = inMinutes(relearnStep)
		case "correct", "easy":
			// Completed relearning → back to review
			newState = "review"
			newStep = 0
			newReps = reps + 1
			// interval was already reduced when the lapse was recorded
			if newInterval < 1 {
				newInterval = 1
			}
			dueDate = midnightInDays(int(newInterval))
		}
	}

	// Fallback safety: ensure due date is set
	if dueDate == "" {
		dueDate = now.UTC().Format(time.RFC3339Nano)
	}
	return
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func normalizeTags(tags string) string {
	parts := splitTags(tags)
	return strings.Join(parts, ",")
}

func splitTags(tags string) []string {
	if strings.TrimSpace(tags) == "" {
		return nil
	}
	var out []string
	for _, t := range strings.Split(tags, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}
