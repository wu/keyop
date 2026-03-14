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

// getNotesList retrieves notes with optional search filter.
func getNotesList(dbPath string, search string, limit, offset int) (any, error) {
	db, err := openNotesDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = db.Close()
	}()

	query := "SELECT id, title, tags, created_at, updated_at FROM notes WHERE 1=1"
	args := []any{}

	if search != "" {
		// Search in title, content, and tags
		searchPattern := "%" + search + "%"
		query += " AND (title LIKE ? OR content LIKE ? OR tags LIKE ?)"
		args = append(args, searchPattern, searchPattern, searchPattern)
	}

	query += " ORDER BY updated_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
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

	return map[string]any{"notes": notes}, nil
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

// createNotesEntry creates a new note.
func createNotesEntry(dbPath string, title, content, tags string) (any, error) {
	db, err := openNotesDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = db.Close()
	}()

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
