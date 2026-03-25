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

// buildSearchWhere builds the shared WHERE clause and args for search/tag filters.
// baseWhere is "WHERE 1=1"; the returned suffix and args are appended by the caller.
func buildNotesWhere(search string, searchContent bool, tag string) (string, []any) {
	where := ""
	args := []any{}

	if search != "" {
		searchPattern := "%" + search + "%"
		if searchContent {
			where += " AND (title LIKE ? OR content LIKE ? OR tags LIKE ?)"
			args = append(args, searchPattern, searchPattern, searchPattern)
		} else {
			where += " AND title LIKE ?"
			args = append(args, searchPattern)
		}
	}

	if tag != "" && tag != "all" {
		if tag == "untagged" {
			where += " AND (tags = '' OR tags IS NULL)"
		} else {
			// Normalise spaces around commas before matching so "foo, bar" matches tag "bar".
			where += " AND REPLACE(',' || tags || ',', ' ', '') LIKE ?"
			args = append(args, "%,"+tag+",%")
		}
	}

	return where, args
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

	whereClause, whereArgs := buildNotesWhere(search, searchContent, tag)
	base := "SELECT id, title, tags, created_at, updated_at FROM notes WHERE 1=1" + whereClause

	// Count total matching rows for pagination.
	var total int
	countArgs := make([]any, len(whereArgs))
	copy(countArgs, whereArgs)
	if err := db.QueryRow("SELECT COUNT(*) FROM notes WHERE 1=1"+whereClause, countArgs...).Scan(&total); err != nil {
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

	whereClause, whereArgs := buildNotesWhere(search, searchContent, "")
	rows, err := db.Query("SELECT tags FROM notes WHERE 1=1"+whereClause, whereArgs...)
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
