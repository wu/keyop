package journal

import (
	"bytes"
	"fmt"
	"keyop/core"
	"keyop/x/webui"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
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

// WebUITab implements webui.TabProvider.
func (svc *Service) WebUITab() webui.TabInfo {
	content := `<div id="journal-container" class="journal-container">
    <div class="journal-sidebar">
        <h3>Dates</h3>
        <div id="journal-date-list" class="journal-date-list"></div>
    </div>
    
    <div class="journal-main">
        <div class="journal-toolbar">
            <button id="journal-edit-btn" class="journal-btn journal-edit-btn">Edit</button>
            <button id="journal-save-btn" class="journal-btn journal-save-btn">Save</button>
            <button id="journal-cancel-btn" class="journal-btn journal-cancel-btn">Cancel</button>
        </div>
        
        <div id="journal-view" class="journal-view"></div>
        <div id="journal-edit" class="journal-edit" style="display: none;">
            <textarea id="journal-textarea" placeholder="Edit your journal entry here..."></textarea>
        </div>
    </div>
</div>

<style>
.journal-container {
    display: flex;
    height: calc(100vh - 180px);
    gap: 0;
    padding: 0;
    background: #1a1a1a;
    overflow: hidden;
}

.journal-sidebar {
    width: 150px;
    flex-shrink: 0;
    border-right: 1px solid #333;
    padding: 1rem 0;
    overflow-y: auto;
    max-height: 100%;
}

.journal-sidebar h3 {
    margin-top: 0;
    font-size: 14px;
    color: #aaa;
    text-transform: uppercase;
    font-weight: bold;
    padding: 0.5rem 1rem;
}

.journal-date-list {
    display: flex;
    flex-direction: column;
    gap: 0;
}

.journal-date-btn {
    padding: 0.75rem 1rem;
    text-align: left;
    background: transparent;
    color: #ccc;
    border: none;
    border-left: 3px solid transparent;
    cursor: pointer;
    font-size: 14px;
    transition: all 0.2s ease;
    margin: 0;
}

.journal-date-btn:hover {
    background: #222;
    color: #fff;
    border-left-color: #666;
}

.journal-date-btn.active {
    background: #222;
    color: #00d4ff;
    border-left-color: #00d4ff;
}

.journal-main {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 0;
    padding: 15px;
    background: #1a1a1a;
    min-height: 0;
    overflow: hidden;
}

.journal-toolbar {
    display: flex;
    gap: 10px;
    flex-shrink: 0;
    margin-bottom: 10px;
}

.journal-btn {
    padding: 8px 16px;
    background: #0088cc;
    color: white;
    border: none;
    border-radius: 4px;
    cursor: pointer;
    font-size: 14px;
    transition: background 0.2s ease;
}

.journal-btn:hover {
    background: #006699;
}

.journal-edit-btn {
    background: #0088cc;
}

.journal-edit-btn:hover {
    background: #006699;
}

.journal-save-btn,
.journal-cancel-btn {
    background: #28a745;
    display: none;
}

.journal-save-btn:hover {
    background: #218838;
}

.journal-cancel-btn {
    background: #dc3545;
}

.journal-cancel-btn:hover {
    background: #c82333;
}

.journal-view {
    flex: 1;
    background: #222;
    border: 1px solid #333;
    border-radius: 4px;
    padding: 15px;
    overflow-y: auto;
    line-height: 1.6;
    color: #ccc;
    min-height: 0;
}

.journal-view h1,
.journal-view h2,
.journal-view h3,
.journal-view h4,
.journal-view h5,
.journal-view h6 {
    margin: 15px 0 10px 0;
    color: #00d4ff;
}

.journal-view h1 { font-size: 24px; }
.journal-view h2 { font-size: 20px; }
.journal-view h3 { font-size: 18px; }
.journal-view h4 { font-size: 16px; }
.journal-view h5 { font-size: 14px; }
.journal-view h6 { font-size: 12px; }

.journal-view p {
    margin: 10px 0;
    color: #ccc;
}

.journal-view ul {
    margin: 10px 0;
    padding-left: 20px;
}

.journal-view li {
    margin: 5px 0;
    color: #ccc;
}

.journal-edit {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
}

.journal-edit textarea {
    flex: 1;
    width: 100%;
    height: 100%;
    min-height: 0;
    padding: 10px;
    border: 1px solid #444;
    border-radius: 4px;
    background: #2a2a2a;
    color: #ccc;
    font-family: 'Monaco', 'Courier New', monospace;
    font-size: 14px;
    resize: none;
    box-sizing: border-box;
}

.journal-edit textarea:focus {
    outline: none;
    border-color: #0088cc;
    box-shadow: 0 0 0 3px rgba(0, 136, 204, 0.25);
}
</style>`

	return webui.TabInfo{
		ID:      "journal",
		Title:   "Journal",
		Icon:    "📝",
		Content: content,
		JSPath:  "/api/assets/journal/journal.js",
	}
}

// WebUIAssets returns the static assets for the journal service.
func (svc *Service) WebUIAssets() http.FileSystem {
	return http.Dir("x/journal/resources")
}

// HandleWebUIAction implements webui.ActionProvider.
func (svc *Service) HandleWebUIAction(action string, params map[string]any) (any, error) {
	switch action {
	case "get-entry":
		return svc.getEntry(params)
	case "get-dates":
		return svc.getDates()
	case "save-entry":
		return svc.saveEntry(params)
	case "render-markdown":
		return svc.renderMarkdown(params)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// getEntry retrieves a journal entry for a specific date.
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

// renderMarkdown converts markdown to HTML using goldmark.
func (svc *Service) renderMarkdown(params map[string]any) (any, error) {
	content, ok := params["content"].(string)
	if !ok {
		return nil, fmt.Errorf("missing content parameter")
	}

	md := goldmark.New(
		goldmark.WithExtensions(extension.Table),
	)

	var buf bytes.Buffer
	if err := md.Convert([]byte(content), &buf); err != nil {
		return nil, fmt.Errorf("failed to render markdown: %w", err)
	}

	return map[string]any{"html": buf.String()}, nil
}
