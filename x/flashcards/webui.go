package flashcards

import (
	"embed"
	"fmt"
	"io/fs"
	"keyop/util"
	"keyop/x/webui"
	"net/http"
)

//go:embed resources
var embeddedAssets embed.FS

// WebUIAssets returns the static assets for the flashcards service.
func (svc *Service) WebUIAssets() http.FileSystem {
	sub, _ := fs.Sub(embeddedAssets, "resources")
	return http.FS(sub)
}

// WebUITab returns the tab configuration for the flashcards service.
func (svc *Service) WebUITab() webui.TabInfo {
	cssContent, err := embeddedAssets.ReadFile("resources/flashcards.css")
	if err != nil {
		cssContent = []byte{}
	}

	html := `<div id="flashcards-container">
<div class="flashcards-layout">
  <div class="filter-sidebar">
    <div class="filter-title">Tags</div>
    <div class="tag-list">
      <div class="service-item active" data-tag="all">
        <span class="tag-label">all</span>
        <span class="service-count" id="fc-count-all">0</span>
      </div>
    </div>
  </div>
  <div class="flashcards-content">
    <div class="flashcards-header">
      <button id="fc-new-btn" class="fc-new-btn">+ New Card</button>
    </div>
    <div id="fc-list">Loading flashcards...</div>
  </div>
</div>

<!-- New card modal -->
<div id="fc-modal" class="fc-modal" style="display:none">
  <div class="fc-modal-box">
    <h3>New Flashcard</h3>
    <label>Question</label>
    <textarea id="fc-question" rows="3" placeholder="Enter question..."></textarea>
    <label>Answer</label>
    <textarea id="fc-answer" rows="4" placeholder="Enter answer..."></textarea>
    <label>Tags <span class="fc-hint">(comma-separated)</span></label>
    <input id="fc-tags" type="text" placeholder="e.g. math, algebra">
    <div class="fc-modal-actions">
      <button id="fc-save-btn" class="fc-btn fc-btn-primary">Save</button>
      <button id="fc-cancel-btn" class="fc-btn">Cancel</button>
    </div>
  </div>
</div>
<!-- Edit card modal -->
<div id="fc-edit-modal" class="fc-modal" style="display:none">
  <div class="fc-modal-box">
    <h3>Edit Flashcard</h3>
    <label>Question</label>
    <textarea id="fc-edit-question" rows="3"></textarea>
    <label>Answer</label>
    <textarea id="fc-edit-answer" rows="4"></textarea>
    <label>Tags <span class="fc-hint">(comma-separated)</span></label>
    <input id="fc-edit-tags" type="text">
    <div class="fc-modal-actions">
      <button id="fc-edit-save-btn" class="fc-btn fc-btn-primary">Save</button>
      <button id="fc-edit-cancel-btn" class="fc-btn">Cancel</button>
    </div>
  </div>
</div>
</div>` + "\n<style>\n" + string(cssContent) + "\n</style>"

	return webui.TabInfo{
		ID:      "flashcards",
		Title:   "Flashcards",
		Content: html,
		JSPath:  "/api/assets/flashcards/flashcards.js",
	}
}

// HandleWebUIAction handles actions from the web UI.
func (svc *Service) HandleWebUIAction(action string, params map[string]any) (any, error) {
	switch action {
	case "list-due":
		tag, _ := params["tag"].(string)
		return svc.listDue(tag)
	case "list-tags":
		return svc.listTags()
	case "create-card":
		question, _ := params["question"].(string)
		answer, _ := params["answer"].(string)
		tags, _ := params["tags"].(string)
		return svc.createCard(question, answer, tags)
	case "review":
		if id, ok := params["id"].(float64); ok {
			if rating, ok := params["rating"].(string); ok {
				return svc.reviewCard(int64(id), rating)
			}
			return nil, fmt.Errorf("missing rating")
		}
		return nil, fmt.Errorf("missing card id")
	case "render-markdown":
		content, _ := params["content"].(string)
		html, err := util.RenderMarkdown(content)
		if err != nil {
			return nil, err
		}
		return map[string]any{"html": html}, nil
	case "update-card":
		if id, ok := params["id"].(float64); ok {
			question, _ := params["question"].(string)
			answer, _ := params["answer"].(string)
			tags, _ := params["tags"].(string)
			return svc.updateCard(int64(id), question, answer, tags)
		}
		return nil, fmt.Errorf("missing card id")
	case "delete-card":
		if id, ok := params["id"].(float64); ok {
			return svc.deleteCard(int64(id))
		}
		return nil, fmt.Errorf("missing card id")
	case "preview-schedule":
		if id, ok := params["id"].(float64); ok {
			return svc.previewSchedule(int64(id))
		}
		return nil, fmt.Errorf("missing card id")
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}
