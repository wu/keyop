package notes

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"sync"
)

// Service provides notes management functionality.
type Service struct {
	Cfg       core.ServiceConfig
	Deps      core.Dependencies
	mu        sync.Mutex
	dbPath    string
	tableName string
}

// NewService creates a new notes service.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) *Service {
	return &Service{
		Cfg:       cfg,
		Deps:      deps,
		dbPath:    "~/.keyop/sqlite/notes.sql",
		tableName: "notes",
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
	svc.mu.Lock()
	defer svc.mu.Unlock()

	// Database initialization is handled by sqlite package
	return nil
}

// getNotes retrieves notes with optional search filter.
func (svc *Service) getNotes(params map[string]any) (any, error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	search, ok := params["search"].(string)
	if !ok {
		search = ""
	}

	searchContent := false
	if sc, ok := params["search_content"].(bool); ok {
		searchContent = sc
	} else if scf, ok := params["search_content"].(float64); ok {
		searchContent = scf != 0
	}

	limit := 100
	if l, ok := params["limit"].(float64); ok {
		limit = int(l)
	}

	offset := 0
	if o, ok := params["offset"].(float64); ok {
		offset = int(o)
	}

	return getNotesList(svc.dbPath, search, searchContent, limit, offset)
}

// getNote retrieves a single note by ID.
func (svc *Service) getNote(params map[string]any) (any, error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	id, ok := params["id"].(float64)
	if !ok {
		return nil, fmt.Errorf("missing or invalid id parameter")
	}

	return getNotesEntry(svc.dbPath, int64(id))
}

// createNote creates a new note.
func (svc *Service) createNote(params map[string]any) (any, error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	title, ok := params["title"].(string)
	if !ok {
		return nil, fmt.Errorf("missing title parameter")
	}

	content, ok := params["content"].(string)
	if !ok {
		content = ""
	}

	tags, ok := params["tags"].(string)
	if !ok {
		tags = ""
	}

	return createNotesEntry(svc.dbPath, title, content, tags)
}

// updateNote updates an existing note.
func (svc *Service) updateNote(params map[string]any) (any, error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	id, ok := params["id"].(float64)
	if !ok {
		return nil, fmt.Errorf("missing or invalid id parameter")
	}

	title, ok := params["title"].(string)
	if !ok {
		return nil, fmt.Errorf("missing title parameter")
	}

	content, ok := params["content"].(string)
	if !ok {
		content = ""
	}

	tags, ok := params["tags"].(string)
	if !ok {
		tags = ""
	}

	return updateNotesEntry(svc.dbPath, int64(id), title, content, tags)
}

// deleteNote deletes a note.
func (svc *Service) deleteNote(params map[string]any) (any, error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	id, ok := params["id"].(float64)
	if !ok {
		return nil, fmt.Errorf("missing or invalid id parameter")
	}

	return deleteNotesEntry(svc.dbPath, int64(id))
}

// renderMarkdown converts markdown to HTML.
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

// importNotes imports markdown files as notes.
func (svc *Service) importNotes(params map[string]any) (any, error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	files, ok := params["files"].([]any)
	if !ok {
		return nil, fmt.Errorf("missing files parameter")
	}

	var imported []map[string]any
	for _, fileData := range files {
		fileMap, ok := fileData.(map[string]any)
		if !ok {
			continue
		}

		name, nameOk := fileMap["name"].(string)
		content, contentOk := fileMap["content"].(string)
		if !nameOk || !contentOk {
			continue
		}

		// Extract title from filename (remove .md extension)
		title := name
		if len(title) > 3 && title[len(title)-3:] == ".md" {
			title = title[:len(title)-3]
		}

		result, err := createNotesEntry(svc.dbPath, title, content, "imported")
		if err != nil {
			continue
		}

		imported = append(imported, result.(map[string]any))
	}

	return map[string]any{"imported": imported, "count": len(imported)}, nil
}
