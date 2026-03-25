package attachments

import (
	"database/sql"
	"fmt"
	"keyop/core"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite" // sqlite driver
)

// Service manages file attachments: upload, storage, and metadata.
type Service struct {
	Deps      core.Dependencies
	Cfg       core.ServiceConfig
	db        *sql.DB
	uploadDir string
	dbPath    string
}

// NewService constructs the attachments service.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	uploadDir := "~/.keyop/attachments"
	if d, ok := cfg.Config["uploadDir"].(string); ok && d != "" {
		uploadDir = d
	}
	dbPath := "~/.keyop/sqlite/attachments.sql"
	if d, ok := cfg.Config["dbPath"].(string); ok && d != "" {
		dbPath = d
	}
	return &Service{
		Deps:      deps,
		Cfg:       cfg,
		uploadDir: uploadDir,
		dbPath:    dbPath,
	}
}

// ValidateConfig performs minimal validation.
func (svc *Service) ValidateConfig() []error {
	return nil
}

// Check performs a periodic health check.
func (svc *Service) Check() error {
	if svc.db == nil {
		return fmt.Errorf("attachments database not initialized")
	}
	return nil
}

// Initialize opens the database and ensures the upload directory exists.
func (svc *Service) Initialize() error {
	uploadDir := expandHome(svc.uploadDir)
	svc.uploadDir = uploadDir
	if err := os.MkdirAll(uploadDir, 0750); err != nil {
		return fmt.Errorf("attachments: failed to create upload dir %s: %w", uploadDir, err)
	}

	dbPath := expandHome(svc.dbPath)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0750); err != nil {
		return fmt.Errorf("attachments: failed to create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal=WAL&_timeout=5000")
	if err != nil {
		return fmt.Errorf("attachments: failed to open database: %w", err)
	}
	svc.db = db

	if _, err := db.Exec(attachmentsSchema); err != nil {
		return fmt.Errorf("attachments: failed to create schema: %w", err)
	}
	if err := migrateAttachmentsSchema(db); err != nil {
		return fmt.Errorf("attachments: failed to migrate schema: %w", err)
	}
	return nil
}

// expandHome expands a leading ~/ to the user's home directory.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// todayDir returns the YYYY-MM-DD sub-directory name for today.
func todayDir() string {
	return time.Now().Format("2006-01-02")
}

var sanitizeRe = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// sanitizeFilename sanitizes the filename base to [a-zA-Z0-9_], preserving the extension.
// Example: "my file (1).pdf" → "my_file__1_.pdf"
func sanitizeFilename(name string) string {
	if name == "" {
		return "attachment"
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	base = sanitizeRe.ReplaceAllString(base, "_")
	if base == "" {
		base = "attachment"
	}
	if ext != "" {
		// Keep only alphanumeric chars in the extension (strip leading dot separately).
		extClean := regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(ext[1:], "")
		if extClean != "" {
			return base + "." + extClean
		}
	}
	return base
}
