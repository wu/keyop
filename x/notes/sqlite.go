package notes

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // SQLite driver for database/sql
)

// buildNotesWhere returns a list of SQL condition fragments (each a hardcoded
// string with ? placeholders) and the corresponding bind arguments. The caller
// assembles them with strings.Join and appends after "WHERE 1=1 AND ...".
//
// Safety guarantee: user input (search, tag) is ONLY placed in the returned
// args slice — never embedded in any of the returned condition strings.
func buildNotesWhere(search string, searchContent bool, tag string) ([]string, []any) {
	var conds []string
	var args []any

	if search != "" {
		p := "%" + search + "%"
		if searchContent {
			conds = append(conds, "(title LIKE ? OR content LIKE ? OR tags LIKE ?)")
			args = append(args, p, p, p)
		} else {
			conds = append(conds, "title LIKE ?")
			args = append(args, p)
		}
	}

	if tag != "" && tag != "all" {
		if tag == "untagged" {
			conds = append(conds, "(tags = '' OR tags IS NULL)")
		} else {
			// Normalise spaces around commas so "foo, bar" matches tag "bar".
			conds = append(conds, "REPLACE(',' || tags || ',', ' ', '') LIKE ?")
			args = append(args, "%,"+tag+",%")
		}
	}

	return conds, args
}

// notesWhere assembles a safe WHERE suffix from buildNotesWhere conditions.
// Only hardcoded condition fragments from buildNotesWhere are joined here;
// no user input enters the returned string.
func notesWhere(conds []string) string { //nolint:gosec
	if len(conds) == 0 {
		return ""
	}
	return " AND " + strings.Join(conds, " AND ")
}

// getNotesList retrieves notes with optional search/tag filter and pagination.
// Returns {notes: [...], total: N} where total is the unfiltered count.
func getNotesList(dbPath string, search string, searchContent bool, tag string, limit, offset int) (any, error) {
	db, err := openNotesDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = db.Close()
	}()

	whereConds, whereArgs := buildNotesWhere(search, searchContent, tag)
	where := notesWhere(whereConds)
	base := "SELECT id, title, tags, created_at, updated_at FROM notes WHERE 1=1" + where

	// Count total matching rows for pagination.
	var total int
	countArgs := make([]any, len(whereArgs))
	copy(countArgs, whereArgs)
	countQ := "SELECT COUNT(*) FROM notes WHERE 1=1" + where
	if err := db.QueryRow(countQ, countArgs...).Scan(&total); err != nil {
		return nil, fmt.Errorf("failed to count notes: %w", err)
	}

	rows, err := db.Query(base+" ORDER BY updated_at DESC LIMIT ? OFFSET ?",
		append(whereArgs, limit, offset)...)
	if err != nil {
		return nil, fmt.Errorf("failed to query notes: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var notes []map[string]any
	for rows.Next() {
		var id int64
		var title, tags, createdAt, updatedAt string
		if err := rows.Scan(&id, &title, &tags, &createdAt, &updatedAt); err != nil {
			continue
		}
		notes = append(notes, map[string]any{
			"id":         id,
			"title":      title,
			"tags":       tags,
			"created_at": createdAt,
			"updated_at": updatedAt,
		})
	}

	return map[string]any{"notes": notes, "total": total}, nil
}

// getTagCounts returns per-tag note counts for all notes matching the search filter.
// The "all" key holds the total matching count; "untagged" counts notes with no tags.
func getTagCounts(dbPath string, search string, searchContent bool) (any, error) {
	db, err := openNotesDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = db.Close()
	}()

	whereConds, whereArgs := buildNotesWhere(search, searchContent, "")
	where := notesWhere(whereConds)
	// q is built only from hardcoded fragments in notesWhere; user input is
	// bound exclusively via whereArgs — no SQL injection risk.
	q := "SELECT tags FROM notes WHERE 1=1" + where //nolint:gosec
	rows, err := db.Query(q, whereArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query tags: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	counts := map[string]int{}
	total := 0
	for rows.Next() {
		var tagsStr string
		if err := rows.Scan(&tagsStr); err != nil {
			continue
		}
		total++
		parts := strings.Split(tagsStr, ",")
		hasTag := false
		for _, p := range parts {
			t := strings.TrimSpace(p)
			if t != "" {
				counts[t]++
				hasTag = true
			}
		}
		if !hasTag {
			counts["untagged"]++
		}
	}
	counts["all"] = total

	return map[string]any{"counts": counts}, nil
}

// getNotesEntry retrieves a single note by ID.
func getNotesEntry(dbPath string, id int64) (any, error) {
	db, err := openNotesDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = db.Close()
	}()

	var title, content, tags, color, createdAt, updatedAt string
	err = db.QueryRow(
		"SELECT title, content, tags, color, created_at, updated_at FROM notes WHERE id = ?",
		id,
	).Scan(&title, &content, &tags, &color, &createdAt, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("note not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query note: %w", err)
	}

	return map[string]any{
		"id":         id,
		"title":      title,
		"content":    content,
		"tags":       tags,
		"color":      color,
		"created_at": createdAt,
		"updated_at": updatedAt,
	}, nil
}

// titleExists returns true if any note with the given title exists, excluding the note with excludeID
// (use excludeID = 0 to check without exclusion, e.g. for new notes).
func titleExists(db *sql.DB, title string, excludeID int64) (bool, error) {
	var count int
	var err error
	if excludeID > 0 {
		err = db.QueryRow("SELECT COUNT(*) FROM notes WHERE title = ? AND id != ?", title, excludeID).Scan(&count)
	} else {
		err = db.QueryRow("SELECT COUNT(*) FROM notes WHERE title = ?", title).Scan(&count)
	}
	return count > 0, err
}

// createNotesEntry creates a new note.
func createNotesEntry(dbPath string, title, content, tags string) (any, error) {
	db, err := openNotesDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = db.Close()
	}()

	if exists, err := titleExists(db, title, 0); err != nil {
		return nil, fmt.Errorf("failed to check title uniqueness: %w", err)
	} else if exists {
		return nil, fmt.Errorf("a note named %q already exists", title)
	}

	now := time.Now().Format(time.RFC3339Nano)
	result, err := db.Exec(
		"INSERT INTO notes (user_id, title, content, tags, color, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		0, title, content, tags, "", now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create note: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get inserted note id: %w", err)
	}

	return map[string]any{
		"id":         id,
		"title":      title,
		"content":    content,
		"tags":       tags,
		"created_at": now,
		"updated_at": now,
	}, nil
}

// updateNotesEntry updates an existing note.
func updateNotesEntry(dbPath string, id int64, title, content, tags string) (any, error) {
	db, err := openNotesDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = db.Close()
	}()

	if exists, err := titleExists(db, title, id); err != nil {
		return nil, fmt.Errorf("failed to check title uniqueness: %w", err)
	} else if exists {
		return nil, fmt.Errorf("a note named %q already exists", title)
	}

	now := time.Now().Format(time.RFC3339Nano)
	result, err := db.Exec(
		"UPDATE notes SET title = ?, content = ?, tags = ?, updated_at = ? WHERE id = ?",
		title, content, tags, now, id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update note: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return nil, fmt.Errorf("note not found")
	}

	return map[string]any{
		"id":         id,
		"title":      title,
		"content":    content,
		"tags":       tags,
		"updated_at": now,
	}, nil
}

// deleteNotesEntry deletes a note.
func deleteNotesEntry(dbPath string, id int64) (any, error) {
	db, err := openNotesDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = db.Close()
	}()

	result, err := db.Exec("DELETE FROM notes WHERE id = ?", id)
	if err != nil {
		return nil, fmt.Errorf("failed to delete note: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return nil, fmt.Errorf("note not found")
	}

	return map[string]any{"deleted": true}, nil
}

// getNoteTitles returns all note IDs and titles, ordered by title length descending.
// Longest titles first ensures the autolink algorithm matches greedily.
func getNoteTitles(dbPath string) (any, error) {
	db, err := openNotesDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = db.Close()
	}()

	rows, err := db.Query("SELECT id, title FROM notes ORDER BY length(title) DESC, title ASC")
	if err != nil {
		return nil, fmt.Errorf("failed to query note titles: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	type noteTitle struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	}
	var titles []noteTitle
	for rows.Next() {
		var nt noteTitle
		if err := rows.Scan(&nt.ID, &nt.Title); err != nil {
			continue
		}
		titles = append(titles, nt)
	}

	return map[string]any{"titles": titles}, nil
}

// openNotesDB opens the notes database.
func openNotesDB(dbPath string) (*sql.DB, error) {
	// Expand ~ to home directory
	if strings.HasPrefix(dbPath, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		dbPath = filepath.Join(home, dbPath[1:])
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}
