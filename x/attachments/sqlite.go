package attachments

import (
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"
)

const attachmentsSchema = `
CREATE TABLE IF NOT EXISTS attachments (
	id               INTEGER PRIMARY KEY AUTOINCREMENT,
	uuid              TEXT    NOT NULL DEFAULT '' UNIQUE,
	original_filename TEXT    NOT NULL,
	stored_filename   TEXT    NOT NULL,
	date_dir          TEXT    NOT NULL,
	mime_type         TEXT    NOT NULL DEFAULT '',
	size              INTEGER NOT NULL DEFAULT 0,
	uploaded_at       DATETIME NOT NULL
);`

// migrateAttachmentsSchema adds columns introduced after the initial schema.
func migrateAttachmentsSchema(db *sql.DB) error {
	// Add uuid column if it doesn't exist (migration for existing databases).
	_, err := db.Exec(`ALTER TABLE attachments ADD COLUMN uuid TEXT NOT NULL DEFAULT ''`)
	if err != nil && err.Error() != "duplicate column name: uuid" {
		// Ignore "duplicate column" — means column already exists.
		if !isDuplicateColumnError(err) {
			return err
		}
	}
	// Back-fill empty UUIDs for any rows that predate this migration.
	rows, err := db.Query(`SELECT id FROM attachments WHERE uuid = '' OR uuid IS NULL`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := db.Exec(`UPDATE attachments SET uuid = ? WHERE id = ?`, uuid.New().String(), id); err != nil {
			return err
		}
	}
	return nil
}

func isDuplicateColumnError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column")
}

// attachment represents one row from the attachments table.
type attachment struct {
	ID               int64
	UUID             string
	OriginalFilename string
	StoredFilename   string
	DateDir          string
	MimeType         string
	Size             int64
	UploadedAt       time.Time
}

// insertAttachment inserts a new attachment record and returns the new ID.
// The UUID field must be set by the caller before calling this function.
func insertAttachment(db *sql.DB, a attachment) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO attachments (uuid, original_filename, stored_filename, date_dir, mime_type, size, uploaded_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.UUID, a.OriginalFilename, a.StoredFilename, a.DateDir, a.MimeType, a.Size, a.UploadedAt,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// listAttachments returns all attachment records, newest first.
func listAttachments(db *sql.DB) ([]attachment, error) {
	rows, err := db.Query(
		`SELECT id, uuid, original_filename, stored_filename, date_dir, mime_type, size, uploaded_at
		 FROM attachments ORDER BY uploaded_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []attachment
	for rows.Next() {
		var a attachment
		if err := rows.Scan(&a.ID, &a.UUID, &a.OriginalFilename, &a.StoredFilename, &a.DateDir, &a.MimeType, &a.Size, &a.UploadedAt); err != nil {
			return nil, err
		}
		results = append(results, a)
	}
	return results, rows.Err()
}

// getAttachmentByUUID returns a single attachment by UUID.
func getAttachmentByUUID(db *sql.DB, id string) (attachment, error) {
	var a attachment
	err := db.QueryRow(
		`SELECT id, uuid, original_filename, stored_filename, date_dir, mime_type, size, uploaded_at
		 FROM attachments WHERE uuid = ?`, id,
	).Scan(&a.ID, &a.UUID, &a.OriginalFilename, &a.StoredFilename, &a.DateDir, &a.MimeType, &a.Size, &a.UploadedAt)
	return a, err
}

// deleteAttachment removes a record from the database by integer ID.
func deleteAttachment(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM attachments WHERE id = ?`, id)
	return err
}

// deleteAttachmentByUUID removes a record from the database by UUID.
func deleteAttachmentByUUID(db *sql.DB, id string) error {
	_, err := db.Exec(`DELETE FROM attachments WHERE uuid = ?`, id)
	return err
}
