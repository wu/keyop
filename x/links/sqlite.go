package links

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite" // SQLite driver for pure Go
)

// openLinksDB opens or creates the links database.
func openLinksDB(dbPath string) (*sql.DB, error) {
	// Expand ~ to home directory
	if strings.HasPrefix(dbPath, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		dbPath = filepath.Join(home, dbPath[1:])
	}

	// Ensure parent directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database with busy timeout and WAL mode to reduce locking
	dsn := fmt.Sprintf("file:%s?cache=shared&mode=rwc&_journal_mode=WAL&_busy_timeout=5000", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool to reduce contention
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(time.Minute)

	// Test connection
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	if err := initLinksDB(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// initLinksDB creates the links table if it doesn't exist.
func initLinksDB(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS links (
id TEXT PRIMARY KEY,
url TEXT NOT NULL,
normalized_url TEXT UNIQUE NOT NULL,
domain TEXT NOT NULL,
name TEXT DEFAULT '',
notes TEXT DEFAULT '',
tags TEXT DEFAULT '',
favicon_path TEXT DEFAULT '',
created_at DATETIME NOT NULL,
updated_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_links_domain ON links(domain);
CREATE INDEX IF NOT EXISTS idx_links_created_at ON links(created_at DESC);
`
	_, err := db.Exec(schema)
	return err
}

// normalizeURL returns a lowercase URL with trailing slashes removed.
func normalizeURL(rawURL string) string {
	u, _ := url.Parse(rawURL)
	if u == nil {
		return strings.ToLower(rawURL)
	}
	// Normalize: lowercase scheme + host, strip trailing slash from path
	norm := strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + u.Path
	if strings.HasSuffix(norm, "/") && u.Path != "/" {
		norm = strings.TrimSuffix(norm, "/")
	}
	if u.RawQuery != "" {
		norm += "?" + u.RawQuery
	}
	return norm
}

// extractDomain extracts domain from a URL.
func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

// Link represents a saved link row.
type Link struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Domain      string `json:"domain"`
	Name        string `json:"name"`
	Notes       string `json:"notes"`
	Tags        string `json:"tags"`
	FaviconPath string `json:"favicon_path"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// addOrUpdateLink adds a new link or updates an existing one (by normalized_url).
// Returns the link ID and any error.
func addOrUpdateLink(dbPath, rawURL, name, notes, tags string) (string, error) {
	db, err := openLinksDB(dbPath)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = db.Close()
	}()

	normURL := normalizeURL(rawURL)
	domain := extractDomain(rawURL)
	now := time.Now().UTC().Format(time.RFC3339)

	// Try to update first
	res, err := db.Exec(
		`UPDATE links SET name=?, notes=?, tags=?, updated_at=? WHERE normalized_url=?`,
		name, notes, tags, now, normURL,
	)
	if err != nil {
		return "", err
	}

	if rows, _ := res.RowsAffected(); rows > 0 {
		// Updated existing row; fetch its ID
		var id string
		err = db.QueryRow(`SELECT id FROM links WHERE normalized_url=?`, normURL).Scan(&id)
		return id, err
	}

	// Insert new row with UUID
	id := uuid.New().String()
	_, err = db.Exec(
		`INSERT INTO links (id, url, normalized_url, domain, name, notes, tags, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		id, rawURL, normURL, domain, name, notes, tags, now, now,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

// updateFaviconPath updates just the favicon path for a link.
func updateFaviconPath(dbPath string, id string, faviconPath string) error {
	db, err := openLinksDB(dbPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`UPDATE links SET favicon_path=?, updated_at=? WHERE id=?`, faviconPath, now, id)
	return err
}

// listLinks returns a paginated list of links with filtering and sorting.
func listLinks(dbPath, search, tag, sort string, limit, offset int) ([]Link, int, error) {
	db, err := openLinksDB(dbPath)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		_ = db.Close()
	}()

	// Build WHERE clause
	var where string
	var args []interface{}

	if search != "" {
		p := "%" + search + "%"
		where = " WHERE (url LIKE ? OR domain LIKE ? OR name LIKE ? OR notes LIKE ?)"
		args = append(args, p, p, p, p)
	}

	if tag != "" {
		tagFilter := "%" + tag + "%"
		if where != "" {
			where += " AND tags LIKE ?"
		} else {
			where = " WHERE tags LIKE ?"
		}
		args = append(args, tagFilter)
	}

	// Get total count
	countQ := "SELECT COUNT(*) FROM links" + where
	var total int
	if err := db.QueryRow(countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Build ORDER clause
	var orderClause string
	switch sort {
	case "date-asc":
		orderClause = " ORDER BY created_at ASC, id ASC"
	case "domain-asc":
		orderClause = " ORDER BY domain ASC, name ASC, id ASC"
	case "name-asc":
		orderClause = " ORDER BY name ASC, domain ASC, id ASC"
	default: // date-desc
		orderClause = " ORDER BY created_at DESC, id DESC"
	}

	// Query links
	// #nosec G202 - where and orderClause are constructed from known parameters, not user input
	q := "SELECT id, url, domain, name, notes, tags, favicon_path, created_at, updated_at FROM links" + where + orderClause + " LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var links []Link
	for rows.Next() {
		var link Link
		if err := rows.Scan(&link.ID, &link.URL, &link.Domain, &link.Name, &link.Notes, &link.Tags, &link.FaviconPath, &link.CreatedAt, &link.UpdatedAt); err != nil {
			continue
		}
		links = append(links, link)
	}
	return links, total, rows.Err()
}

// getTagCounts returns per-tag counts for links matching the search filter.
func getTagCounts(dbPath, search string) (map[string]int, error) {
	db, err := openLinksDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = db.Close()
	}()

	var where string
	var args []interface{}

	if search != "" {
		p := "%" + search + "%"
		where = " WHERE (url LIKE ? OR domain LIKE ? OR name LIKE ? OR notes LIKE ?)"
		args = append(args, p, p, p, p)
	}
	// #nosec G202 - where is constructed from known parameters, not user input
	query := "SELECT tags FROM links" + where
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	counts := make(map[string]int)
	total := 0
	for rows.Next() {
		var tagsStr string
		if err := rows.Scan(&tagsStr); err != nil {
			continue
		}
		total++

		if tagsStr == "" {
			counts["untagged"]++
		} else {
			parts := strings.Split(tagsStr, ",")
			for _, p := range parts {
				tag := strings.TrimSpace(p)
				if tag != "" {
					counts[tag]++
				}
			}
		}
	}

	counts["all"] = total
	return counts, rows.Err()
}

// deleteLink deletes a link by ID.
func deleteLink(dbPath string, id string) error {
	db, err := openLinksDB(dbPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	_, err = db.Exec(`DELETE FROM links WHERE id=?`, id)
	return err
}

// getLink returns a single link by ID.
func getLink(dbPath string, id string) (Link, error) {
	db, err := openLinksDB(dbPath)
	if err != nil {
		return Link{}, err
	}
	defer func() {
		_ = db.Close()
	}()

	var link Link
	err = db.QueryRow(
		`SELECT id, url, domain, name, notes, tags, favicon_path, created_at, updated_at FROM links WHERE id=?`,
		id,
	).Scan(&link.ID, &link.URL, &link.Domain, &link.Name, &link.Notes, &link.Tags, &link.FaviconPath, &link.CreatedAt, &link.UpdatedAt)
	return link, err
}

// addOrUpdateLinkWithDate adds a new link or updates an existing one, with optional created_at override.
// When updating an existing link, uses the oldest date between the new and existing record, and merges tags.
func addOrUpdateLinkWithDate(dbPath, rawURL, name, notes, tags, createdAtStr string) (string, error) {
	db, err := openLinksDB(dbPath)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = db.Close()
	}()

	normURL := normalizeURL(rawURL)
	domain := extractDomain(rawURL)
	now := time.Now().UTC().Format(time.RFC3339)

	// Parse createdAt; use now if not provided or invalid
	createdAt := now
	if createdAtStr != "" {
		if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			createdAt = t.UTC().Format(time.RFC3339)
		}
	}

	// Check if link already exists
	var existingID string
	var existingCreatedAt string
	var existingTags string
	err = db.QueryRow(`SELECT id, created_at, tags FROM links WHERE normalized_url=?`, normURL).Scan(&existingID, &existingCreatedAt, &existingTags)

	if err == sql.ErrNoRows {
		// Insert new row with UUID
		id := uuid.New().String()
		_, err = db.Exec(
			`INSERT INTO links (id, url, normalized_url, domain, name, notes, tags, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
			id, rawURL, normURL, domain, name, notes, tags, createdAt, now,
		)
		if err != nil {
			return "", err
		}
		return id, nil
	}
	if err != nil {
		return "", err
	}

	// Link exists; use the older of the two dates
	finalCreatedAt := createdAt
	if existingCreatedAt != "" {
		t1, _ := time.Parse(time.RFC3339, existingCreatedAt)
		t2, _ := time.Parse(time.RFC3339, createdAt)
		if t1.Before(t2) {
			finalCreatedAt = existingCreatedAt
		}
	}

	// Merge tags: combine new tags with existing tags, avoiding duplicates
	finalTags := mergeTags(existingTags, tags)

	// Update with older date and merged tags
	_, err = db.Exec(
		`UPDATE links SET url=?, normalized_url=?, domain=?, name=?, notes=?, tags=?, created_at=?, updated_at=? WHERE id=?`,
		rawURL, normURL, domain, name, notes, finalTags, finalCreatedAt, now, existingID,
	)
	return existingID, err
}

// mergeTags combines two comma-separated tag strings, avoiding duplicates.
func mergeTags(existing, newTags string) string {
	if existing == "" && newTags == "" {
		return ""
	}
	if existing == "" {
		return newTags
	}
	if newTags == "" {
		return existing
	}

	// Parse both tag lists
	existingList := strings.Split(existing, ",")
	newList := strings.Split(newTags, ",")

	// Create a map to track unique tags
	tagMap := make(map[string]bool)
	var result []string

	// Add existing tags
	for _, tag := range existingList {
		trimmed := strings.TrimSpace(tag)
		if trimmed != "" && !tagMap[trimmed] {
			tagMap[trimmed] = true
			result = append(result, trimmed)
		}
	}

	// Add new tags
	for _, tag := range newList {
		trimmed := strings.TrimSpace(tag)
		if trimmed != "" && !tagMap[trimmed] {
			tagMap[trimmed] = true
			result = append(result, trimmed)
		}
	}

	return strings.Join(result, ",")
}

// updateLinkFull updates all fields of a link including URL and created_at.
func updateLinkFull(dbPath string, id string, url, name, notes, tags, createdAtStr string) error {
	db, err := openLinksDB(dbPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	now := time.Now().UTC().Format(time.RFC3339)

	// Parse createdAt; use now if not provided or invalid
	createdAt := now
	if createdAtStr != "" {
		if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			createdAt = t.UTC().Format(time.RFC3339)
		}
	}

	domain := extractDomain(url)
	normURL := normalizeURL(url)

	_, err = db.Exec(
		`UPDATE links SET url=?, normalized_url=?, domain=?, name=?, notes=?, tags=?, created_at=?, updated_at=? WHERE id=?`,
		url, normURL, domain, name, notes, tags, createdAt, now, id,
	)
	return err
}

// updateNoteOnly updates only the note field for a link.
func updateNoteOnly(dbPath string, id string, note string) error {
	db, err := openLinksDB(dbPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(
		`UPDATE links SET notes=?, updated_at=? WHERE id=?`,
		note, now, id,
	)
	return err
}

// updateTags updates only the tags field for a link.
func updateTags(dbPath string, id string, tags string) error {
	db, err := openLinksDB(dbPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(
		`UPDATE links SET tags=?, updated_at=? WHERE id=?`,
		tags, now, id,
	)
	return err
}

// hashDomain creates a deterministic filename for a domain's favicon.
func hashDomain(domain string) string {
	h := sha256.Sum256([]byte(strings.ToLower(domain)))
	return fmt.Sprintf("%x", h)[:16]
}
