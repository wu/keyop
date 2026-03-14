package notes

import (
	"fmt"
	"keyop/x/webui"
	"net/http"
	"os"
	"path/filepath"
)

// WebUITab implements webui.TabProvider.
func (svc *Service) WebUITab() webui.TabInfo {
	// Load HTML from resources directory
	htmlPath := filepath.Join("x/notes/resources", "notes.html")
	htmlContent, err := os.ReadFile(htmlPath) // #nosec G304: path is fixed at compile time
	if err != nil {
		// Fallback if file not found
		htmlContent = []byte(`<div id="notes-container" class="notes-container">
    <div class="notes-sidebar">
        <div class="notes-toolbar">
            <input type="text" id="notes-search" class="notes-search" placeholder="Search notes...">
            <button id="notes-new-btn" class="notes-btn notes-new-btn">+ New</button>
        </div>
        <div id="notes-list" class="notes-list"></div>
        <div id="notes-import" class="notes-import">
            <div class="notes-import-zone">Drop markdown files here to import</div>
        </div>
    </div>
    <div class="notes-main">
        <div class="notes-toolbar">
            <button id="notes-edit-btn" class="notes-btn notes-edit-btn">Edit</button>
            <button id="notes-save-btn" class="notes-btn notes-save-btn">Save</button>
            <button id="notes-delete-btn" class="notes-btn notes-delete-btn">Delete</button>
            <button id="notes-cancel-btn" class="notes-btn notes-cancel-btn">Cancel</button>
        </div>
        <div id="notes-view" class="notes-view"></div>
        <div id="notes-edit" class="notes-edit" style="display: none;">
            <input type="text" id="notes-title" class="notes-input" placeholder="Note title">
            <textarea id="notes-content" class="notes-textarea" placeholder="Note content (markdown)"></textarea>
            <input type="text" id="notes-tags" class="notes-input" placeholder="Tags (comma-separated)">
        </div>
    </div>
</div>`)
	}

	// Load CSS from resources directory
	cssPath := filepath.Join("x/notes/resources", "notes.css")
	cssContent, err := os.ReadFile(cssPath) // #nosec G304: path is fixed at compile time
	if err != nil {
		cssContent = []byte{}
	}

	// Combine HTML with embedded CSS
	content := string(htmlContent) + "\n<style>\n" + string(cssContent) + "\n</style>"

	return webui.TabInfo{
		ID:      "notes",
		Title:   "Notes",
		Icon:    "📋",
		Content: content,
		JSPath:  "/api/assets/notes/notes.js",
	}
}

// WebUIAssets returns the static assets for the notes service.
func (svc *Service) WebUIAssets() http.FileSystem {
	return http.Dir("x/notes/resources")
}

// HandleWebUIAction handles actions from the WebUI.
func (svc *Service) HandleWebUIAction(action string, params map[string]any) (any, error) {
	switch action {
	case "get-notes":
		return svc.getNotes(params)
	case "get-note":
		return svc.getNote(params)
	case "create-note":
		return svc.createNote(params)
	case "update-note":
		return svc.updateNote(params)
	case "delete-note":
		return svc.deleteNote(params)
	case "import-notes":
		return svc.importNotes(params)
	case "render-markdown":
		return svc.renderMarkdown(params)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}
