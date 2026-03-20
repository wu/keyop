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
			created_at   DATETIME NOT NULL,
			updated_at   DATETIME NOT NULL
		)
	`)
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

// reviewCard applies the SM-2 algorithm and updates the card.
// rating must be one of: "show_again", "hard", "correct", "easy"
func (svc *Service) reviewCard(id int64, rating string) (any, error) {
	if svc.db == nil {
		return nil, fmt.Errorf("flashcards database not available")
	}

	var ef float64
	var interval, reps int64
	err := svc.db.QueryRow(
		`SELECT ease_factor, interval, repetitions FROM flashcards WHERE id = ?`, id,
	).Scan(&ef, &interval, &reps)
	if err != nil {
		return nil, fmt.Errorf("flashcards: card %d not found: %w", id, err)
	}

	quality := ratingToQuality(rating)
	ef, interval, reps = sm2(ef, interval, reps, quality)

	// Schedule due date at local midnight N days from now.
	// Using AddDate + time.Date correctly handles DST transitions.
	loc := time.Local
	nowLocal := time.Now().In(loc)
	future := nowLocal.AddDate(0, 0, int(interval))
	dueLocal := time.Date(future.Year(), future.Month(), future.Day(), 0, 0, 0, 0, loc)
	dueDate := dueLocal.UTC().Format(time.RFC3339Nano)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err = svc.db.Exec(
		`UPDATE flashcards SET ease_factor=?, interval=?, repetitions=?, due_date=?, updated_at=? WHERE id=?`,
		ef, interval, reps, dueDate, now, id,
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

// ratingToQuality maps UI rating names to SM-2 quality scores (0–4).
func ratingToQuality(rating string) int {
	switch rating {
	case "show_again":
		return 0
	case "hard":
		return 2
	case "correct":
		return 3
	case "easy":
		return 4
	default:
		return 3
	}
}

// sm2 applies the SM-2 algorithm and returns updated (easeFactor, interval, repetitions).
func sm2(ef float64, interval, reps int64, quality int) (float64, int64, int64) {
	if quality < 2 {
		// Failed: reset
		return ef, 1, 0
	}

	// Update ease factor
	ef = ef + 0.1 - float64(3-quality)*(0.08+float64(3-quality)*0.02)
	if ef < 1.3 {
		ef = 1.3
	}

	// Update interval
	var newInterval int64
	switch reps {
	case 0:
		newInterval = 1
	case 1:
		newInterval = 6
	default:
		newInterval = int64(math.Round(float64(interval) * ef))
	}

	return ef, newInterval, reps + 1
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
