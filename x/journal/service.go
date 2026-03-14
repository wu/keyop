package journal

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Service provides journaling functionality.
type Service struct {
	Cfg   core.ServiceConfig
	Deps  core.Dependencies
	mu    sync.Mutex
	today string // Track the current date to detect day changes
}

// NewService creates a new journal service.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) *Service {
	return &Service{
		Cfg:  cfg,
		Deps: deps,
	}
}

// Check implements core.Service.
func (svc *Service) Check() error {
	return nil
}

// ValidateConfig implements core.Service.
func (svc *Service) ValidateConfig() []error {
	return nil
}

// Initialize implements core.Service.
func (svc *Service) Initialize() error {
	svc.today = time.Now().Format("2006-01-02")

	ctx := svc.Deps.MustGetContext()
	messenger := svc.Deps.MustGetMessenger()

	// Subscribe to journal channel
	if err := messenger.Subscribe(ctx, svc.Cfg.Name, "journal", svc.Cfg.Type, svc.Cfg.Name, 0, svc.messageHandler); err != nil {
		return fmt.Errorf("failed to subscribe to journal channel: %w", err)
	}

	return nil
}

// messageHandler processes incoming messages and appends them to the journal.
func (svc *Service) messageHandler(msg core.Message) error {
	if msg.Text == "" {
		return nil
	}

	svc.mu.Lock()
	defer svc.mu.Unlock()

	// Get the current date
	today := time.Now().Format("2006-01-02")

	// Get journal directory
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	journalDir := filepath.Join(home, ".keyop", "journal")

	// Create directory if it doesn't exist
	if err := os.MkdirAll(journalDir, 0o750); err != nil {
		return fmt.Errorf("failed to create journal directory: %w", err)
	}

	// Get the journal file for today
	journalFile := filepath.Join(journalDir, fmt.Sprintf("%s.md", today))

	// Open and append to the file
	f, err := os.OpenFile(journalFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304: today is current date
	if err != nil {
		return fmt.Errorf("failed to open journal file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			err = fmt.Errorf("failed to close journal file: %w", cerr)
		}
	}()

	// Format the entry with timestamp and message
	timestamp := time.Now().Format("15:04:05")
	entry := fmt.Sprintf("- %s: %s\n", timestamp, msg.Text)

	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("failed to write to journal file: %w", err)
	}

	return nil
}

// getEntry retrieves a journal entry for a specific date.
func (svc *Service) getEntry(params map[string]any) (any, error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	date, ok := params["date"].(string)
	if !ok || date == "" {
		date = time.Now().Format("2006-01-02")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	journalFile := filepath.Join(home, ".keyop", "journal", fmt.Sprintf("%s.md", date))

	// Read the file if it exists
	content, err := os.ReadFile(journalFile) // #nosec G304: date is controlled by user UI selection
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{"date": date, "content": ""}, nil
		}
		return nil, fmt.Errorf("failed to read journal file: %w", err)
	}

	return map[string]any{"date": date, "content": string(content)}, nil
}

// getDates returns a list of available journal dates.
func (svc *Service) getDates() (any, error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	journalDir := filepath.Join(home, ".keyop", "journal")

	// Create directory if it doesn't exist
	if err := os.MkdirAll(journalDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create journal directory: %w", err)
	}

	entries, err := os.ReadDir(journalDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read journal directory: %w", err)
	}

	var dates []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".md" {
			// Extract date from filename (e.g., "2026-03-14.md" -> "2026-03-14")
			date := entry.Name()[:len(entry.Name())-3]
			dates = append(dates, date)
		}
	}

	return map[string]any{"dates": dates}, nil
}

// saveEntry saves edits to a journal entry.
func (svc *Service) saveEntry(params map[string]any) (any, error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	date, ok := params["date"].(string)
	if !ok || date == "" {
		return nil, fmt.Errorf("missing date parameter")
	}

	content, ok := params["content"].(string)
	if !ok {
		return nil, fmt.Errorf("missing content parameter")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	journalFile := filepath.Join(home, ".keyop", "journal", fmt.Sprintf("%s.md", date))

	// Write the file
	if err := os.WriteFile(journalFile, []byte(content), 0o600); err != nil {
		return nil, fmt.Errorf("failed to save journal file: %w", err)
	}

	return map[string]any{"saved": true}, nil
}

// renderMarkdown converts markdown to HTML using goldmark, preserving visual list grouping.
func (svc *Service) renderMarkdown(params map[string]any) (any, error) {
	content, ok := params["content"].(string)
	if !ok {
		return nil, fmt.Errorf("missing content parameter")
	}

	html, err := util.RenderMarkdown(content)
	if err != nil {
		return nil, err
	}

	return map[string]any{"html": html}, nil
}
