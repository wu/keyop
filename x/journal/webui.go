package journal

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
	htmlPath := filepath.Join("x/journal/resources", "journal.html")
	htmlContent, err := os.ReadFile(htmlPath) // #nosec G304: path is fixed at compile time
	if err != nil {
		// Fallback if file not found
		htmlContent = []byte(`<div id="journal-container" class="journal-container">
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
</div>`)
	}

	// Load CSS from resources directory
	cssPath := filepath.Join("x/journal/resources", "journal.css")
	cssContent, err := os.ReadFile(cssPath) // #nosec G304: path is fixed at compile time
	if err != nil {
		cssContent = []byte{}
	}

	// Combine HTML with embedded CSS
	content := string(htmlContent) + "\n<style>\n" + string(cssContent) + "\n</style>"

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

// HandleWebUIAction handles actions from the WebUI.
func (svc *Service) HandleWebUIAction(action string, params map[string]any) (any, error) {
	switch action {
	case "get-dates":
		return svc.getDates()
	case "get-entry":
		return svc.getEntry(params)
	case "save-entry":
		return svc.saveEntry(params)
	case "render-markdown":
		return svc.renderMarkdown(params)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}
