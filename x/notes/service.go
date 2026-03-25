package notes

import (
	"fmt"
	"time"

	"keyop/core"
	"keyop/util"
	"keyop/x/git"
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

	tag, _ := params["tag"].(string)

	limit := 10
	if l, ok := params["limit"].(float64); ok {
		limit = int(l)
	}

	offset := 0
	if o, ok := params["offset"].(float64); ok {
		offset = int(o)
	}

	return getNotesList(svc.dbPath, search, searchContent, tag, limit, offset)
}

// getTagCounts returns per-tag counts for notes matching the current search filter.
func (svc *Service) getTagCounts(params map[string]any) (any, error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	search, _ := params["search"].(string)

	searchContent := false
	if sc, ok := params["search_content"].(bool); ok {
		searchContent = sc
	} else if scf, ok := params["search_content"].(float64); ok {
		searchContent = scf != 0
	}

	return getTagCounts(svc.dbPath, search, searchContent)
}

// getNoteTitles returns all note IDs and titles for autolink matching.
func (svc *Service) getNoteTitles(_ map[string]any) (any, error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	return getNoteTitles(svc.dbPath)
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

// updateNote updates an existing note and emits a ContentChange event with the old and new content.
// If the title changed, a ContentRename event is emitted first so the git service can mv the file.
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

	// Fetch old content and title for change events.
	var oldContent, oldTitle string
	if oldEntry, err := getNotesEntry(svc.dbPath, int64(id)); err == nil {
		if m, ok := oldEntry.(map[string]any); ok {
			if oc, ok2 := m["content"].(string); ok2 {
				oldContent = oc
			}
			if ot, ok2 := m["title"].(string); ok2 {
				oldTitle = ot
			}
		}
	} else {
		svc.Deps.MustGetLogger().Warn("notes: failed to fetch previous content", "id", id, "error", err)
	}

	res, err := updateNotesEntry(svc.dbPath, int64(id), title, content, tags)
	if err != nil {
		return nil, err
	}

	messenger := svc.Deps.MustGetMessenger()
	logger := svc.Deps.MustGetLogger()

	// If the title changed, emit a content_rename event first so the git service
	// can rename the file before writing new content under the new name.
	if oldTitle != "" && oldTitle != title {
		cr := git.ContentRenameEvent{
			OldName: oldTitle,
			NewName: title,
		}
		if sendErr := messenger.Send(core.Message{
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "content_rename",
			Summary:     title,
			Data:        cr,
		}); sendErr != nil {
			logger.Error("notes: failed to send ContentRename event", "error", sendErr, "id", id)
		}
	}

	// Emit ContentChange event (typed). Messenger will set DataType automatically.
	cc := git.ContentChangeEvent{
		Name:      title,
		Old:       oldContent,
		New:       content,
		UpdatedAt: time.Now().Format(time.RFC3339Nano),
	}

	if sendErr := messenger.Send(core.Message{
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "content_change",
		Summary:     title,
		Data:        cc,
	}); sendErr != nil {
		logger.Error("notes: failed to send ContentChange event", "error", sendErr, "id", id)
	}

	return res, nil
}

// deleteNote deletes a note and emits a ContentRemove event.
func (svc *Service) deleteNote(params map[string]any) (any, error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	id, ok := params["id"].(float64)
	if !ok {
		return nil, fmt.Errorf("missing or invalid id parameter")
	}

	// Fetch title before deletion so we can emit the remove event.
	var title string
	if oldEntry, err := getNotesEntry(svc.dbPath, int64(id)); err == nil {
		if m, ok := oldEntry.(map[string]any); ok {
			if t, ok2 := m["title"].(string); ok2 {
				title = t
			}
		}
	}

	res, err := deleteNotesEntry(svc.dbPath, int64(id))
	if err != nil {
		return nil, err
	}

	if title != "" {
		cr := git.ContentRemoveEvent{Name: title}
		if sendErr := svc.Deps.MustGetMessenger().Send(core.Message{
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "content_remove",
			Summary:     title,
			Data:        cr,
		}); sendErr != nil {
			svc.Deps.MustGetLogger().Error("notes: failed to send ContentRemove event", "error", sendErr, "id", id)
		}
	}

	return res, nil
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
