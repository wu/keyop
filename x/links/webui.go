package links

import (
	"embed"
	"fmt"
	"io/fs"
	"keyop/core"
	"keyop/x/search"
	"keyop/x/webui"
	"net/http"
	"strings"
	"time"
)

//go:embed resources
var embeddedAssets embed.FS

// WebUITab returns the links tab configuration.
func (svc *Service) WebUITab() webui.TabInfo {
	return webui.TabInfo{
		ID:    "links",
		Title: "🔗",
		Content: `<div id="links-container">
<div class="links-layout">
  <div class="links-sidebar">
    <div class="links-sidebar-header">
      <div class="links-tag-filter-wrapper">
        <input type="text" id="links-tag-filter" class="links-tag-filter" placeholder="Filter tags...">
        <button id="links-clear-tag-filter" class="links-clear-tag-filter" style="display:none;" title="Clear filter">✕</button>
      </div>
    </div>
    <div class="tag-list" id="links-tag-list"></div>
  </div>
  <div class="links-content-wrapper">
    <div class="links-toolbar">
      <button id="links-select-all-btn" class="links-btn" onclick="window.linksSelectAll();">Select All</button>
      <button id="links-unselect-all-btn" class="links-btn" onclick="window.linksUnselectAll();">Unselect All</button>
      <button id="links-bulk-tag-btn" class="links-btn" onclick="window.linksOpenBulkTagModal();">Tag</button>
      <div class="links-search-wrapper">
        <input type="text" id="links-search" class="links-search" placeholder="Search links...">
        <button id="links-clear-search" class="links-clear-search" style="display:none;" title="Clear search">✕</button>
      </div>
      <select id="links-sort" class="links-sort">
        <option value="date-desc">Newest</option>
        <option value="date-asc">Oldest</option>
        <option value="domain-asc">Domain A–Z</option>
        <option value="name-asc">Name A–Z</option>
      </select>
      <button id="links-add-btn" class="links-btn links-add-btn">+ Add</button>
    </div>
    <div class="links-content">
      <div id="links-add-form" class="links-add-form" style="display:none;">
        <textarea id="links-url-input" class="links-input" placeholder="URL or paste OneTab format..."></textarea>
        <input type="text" id="links-name-input" class="links-input" placeholder="Name (optional)">
        <textarea id="links-notes-input" class="links-input" placeholder="Notes (optional)"></textarea>
        <input type="text" id="links-tags-input" class="links-input" placeholder="Tags (comma-separated)">
        <input type="datetime-local" id="links-created-at-input" class="links-input" placeholder="Date added (optional)">
        <input type="file" id="links-icon-input" class="links-input" accept=".ico,.png,.jpg,.jpeg,.gif" placeholder="Custom icon (optional)">
        <div class="links-form-actions">
          <button id="links-submit-btn" class="links-btn">Submit</button>
          <button id="links-cancel-btn" class="links-btn">Cancel</button>
        </div>
      </div>
      <div id="links-list">Loading links...</div>
    </div>
  </div>
</div>
</div>
<div id="links-edit-modal" class="links-modal">
  <div class="links-modal-content">
    <div class="links-modal-header">
      Edit Link
      <button class="links-modal-close" onclick="window.linksCloseEditModal();">×</button>
    </div>
    <div class="links-modal-body">
      <div class="links-form-group">
        <label>URL</label>
        <input type="text" id="links-edit-url" placeholder="https://example.com">
      </div>
      <div class="links-form-group">
        <label>Name</label>
        <input type="text" id="links-edit-name" placeholder="Link name (optional)">
      </div>
      <div class="links-form-group">
        <label>Tags</label>
        <input type="text" id="links-edit-tags" placeholder="comma-separated">
      </div>
      <div class="links-form-group">
        <label>Date Added</label>
        <input type="datetime-local" id="links-edit-created-at">
      </div>
      <div class="links-form-group">
        <label>Custom Icon (optional)</label>
        <input type="file" id="links-edit-icon" accept=".ico,.png,.jpg,.jpeg,.gif">
      </div>
    </div>
    <div class="links-modal-actions">
      <button onclick="window.linksCloseEditModal();">Cancel</button>
      <button class="links-btn-primary" onclick="window.linksSaveEdit();">Save</button>
    </div>
  </div>
</div>
<div id="links-note-modal" class="links-modal">
  <div class="links-modal-content">
    <div class="links-modal-header">
      Edit Note
      <button class="links-modal-close" onclick="window.linksCloseNoteModal();">×</button>
    </div>
    <div class="links-modal-body">
      <textarea id="links-note-textarea" placeholder="Enter note..." style="width: 100%; height: 200px; padding: 8px; border: 1px solid var(--border); border-radius: 3px; background: var(--item-bg); color: var(--text); font-family: inherit; resize: vertical;"></textarea>
    </div>
    <div class="links-modal-actions">
      <button onclick="window.linksCloseNoteModal();">Cancel</button>
      <button class="links-btn-primary" onclick="window.linksSaveNote();">Save</button>
    </div>
  </div>
</div>
<div id="links-bulk-tag-modal" class="links-modal">
  <div class="links-modal-content">
    <div class="links-modal-header">
      Manage Tags for Selected Links
      <button class="links-modal-close" onclick="window.linkCloseBulkTagModal();">×</button>
    </div>
    <div class="links-modal-body">
       <div class="links-form-group">
        <label>Action</label>
        <div style="display: flex; gap: 12px; flex-wrap: nowrap; white-space: nowrap;">
          <label style="display: flex; align-items: center; gap: 6px; cursor: pointer; font-weight: normal; margin: 0; flex-shrink: 0;">
            <input type="radio" id="links-tag-action-add" name="tag-action" value="add" checked style="cursor: pointer;">
            Add Tag
          </label>
          <label style="display: flex; align-items: center; gap: 6px; cursor: pointer; font-weight: normal; margin: 0; flex-shrink: 0;">
            <input type="radio" id="links-tag-action-remove" name="tag-action" value="remove" style="cursor: pointer;">
            Remove Tag
          </label>
        </div>
      </div>
      <div class="links-form-group">
        <label>Tag Name</label>
        <input type="text" id="links-bulk-tag-input" placeholder="Tag name" autofocus>
      </div>
      <div style="font-size: 13px; color: var(--text-secondary); margin-top: 10px;">
        Selected: <span id="links-bulk-tag-count">0</span> links
      </div>
    </div>
    <div class="links-modal-actions">
      <button onclick="window.linkCloseBulkTagModal();">Cancel</button>
      <button class="links-btn-primary" id="links-bulk-tag-submit" onclick="window.linksSaveBulkTag();">Add Tag</button>
    </div>
  </div>
</div>
<link href="/api/assets/links/links.css" rel="stylesheet">`,
		JSPath: "/api/assets/links/links.js",
	}
}

// WebUIAssets returns the static assets for the links service.
func (svc *Service) WebUIAssets() http.FileSystem {
	sub, _ := fs.Sub(embeddedAssets, "resources")
	return http.FS(sub)
}

// HandleWebUIAction handles actions from the WebUI.
func (svc *Service) HandleWebUIAction(action string, params map[string]any) (any, error) {
	switch action {
	case "list-links":
		return svc.listLinksAction(params)
	case "get-tag-counts":
		return svc.getTagCountsAction(params)
	case "add-link":
		return svc.addLinkAction(params)
	case "bulk-import":
		return svc.bulkImportAction(params)
	case "update-link":
		return svc.updateLinkAction(params)
	case "delete-link":
		return svc.deleteLinkAction(params)
	case "bulk-add-tag":
		return svc.bulkTagAction(params)
	default:
		return nil, ErrUnknownAction
	}
}

// listLinksAction handles the list-links action.
func (svc *Service) listLinksAction(params map[string]any) (any, error) {
	search, _ := params["search"].(string)
	tag, _ := params["tag"].(string)
	sort, _ := params["sort"].(string)
	if sort == "" {
		sort = "date-desc"
	}

	limit := 100
	if l, ok := params["limit"].(float64); ok {
		limit = int(l)
	}
	offset := 0
	if o, ok := params["offset"].(float64); ok {
		offset = int(o)
	}

	links, total, err := listLinks(svc.dbPath, search, tag, sort, limit, offset)
	if err != nil {
		return nil, err
	}

	for i := range links {
		if links[i].FaviconPath != "" {
			links[i].FaviconPath = "/api/links/favicon/" + links[i].ID
		}
	}

	return map[string]any{
		"links": links,
		"total": total,
	}, nil
}

// getTagCountsAction handles the get-tag-counts action.
func (svc *Service) getTagCountsAction(params map[string]any) (any, error) {
	search, _ := params["search"].(string)
	counts, err := getTagCounts(svc.dbPath, search)
	if err != nil {
		return nil, err
	}
	return map[string]any{"counts": counts}, nil
}

// addLinkAction handles the add-link action.
func (svc *Service) addLinkAction(params map[string]any) (any, error) {
	rawURL, _ := params["url"].(string)
	if rawURL == "" {
		return nil, ErrMissingURL
	}

	name, _ := params["name"].(string)
	notes, _ := params["notes"].(string)
	tags, _ := params["tags"].(string)
	createdAtStr, _ := params["created_at"].(string)
	iconPath, _ := params["icon_path"].(string)

	id, err := addOrUpdateLinkWithDate(svc.dbPath, rawURL, name, notes, tags, createdAtStr)
	if err != nil {
		return nil, err
	}

	// If a custom icon was provided, use it; otherwise fetch favicon asynchronously
	if iconPath != "" {
		_ = updateFaviconPath(svc.dbPath, id, iconPath)
	} else {
		// Fetch favicon asynchronously
		domain := extractDomain(rawURL)
		go func() {
			dataDir := svc.getDataDir()
			if faviconPath, err := FetchAndCacheFavicon(domain, dataDir); err == nil && faviconPath != "" {
				_ = updateFaviconPath(svc.dbPath, id, faviconPath)
			}
		}()
	}

	link, err := getLink(svc.dbPath, id)
	if err != nil {
		return nil, err
	}
	if link.FaviconPath != "" {
		link.FaviconPath = "/api/links/favicon/" + link.ID
	}

	// Emit search index event
	svc.emitSearchIndexEvent("upsert", link.ID, link.URL, link.Name, link.Notes, link.Tags)

	return link, nil
}

// bulkImportAction handles the bulk-import action.
func (svc *Service) bulkImportAction(params map[string]any) (any, error) {
	text, _ := params["text"].(string)
	tags, _ := params["tags"].(string)
	createdAt, _ := params["created_at"].(string)
	parsed := ParseBulkInput(text)

	imported := 0
	failed := 0
	failedItems := []map[string]string{} // Track URL and reason
	dataDir := svc.getDataDir()

	for _, p := range parsed {
		// Apply tags from form to each link if not already present in OneTab
		linkTags := p.Notes
		if linkTags == "" && tags != "" {
			linkTags = tags
		}

		var id string
		var err error
		if createdAt != "" {
			// Use custom date if provided
			id, err = addOrUpdateLinkWithDate(svc.dbPath, p.URL, p.Name, p.Notes, linkTags, createdAt)
		} else {
			id, err = addOrUpdateLink(svc.dbPath, p.URL, p.Name, p.Notes, linkTags)
		}

		if err != nil {
			failed++
			failedItems = append(failedItems, map[string]string{
				"url":    p.URL,
				"reason": err.Error(),
			})
			continue
		}

		// Count as imported (both new inserts and updates count towards success)
		imported++

		// Fetch favicon asynchronously
		domain := extractDomain(p.URL)
		go func(linkID string, d string) {
			if faviconPath, err := FetchAndCacheFavicon(d, dataDir); err == nil && faviconPath != "" {
				_ = updateFaviconPath(svc.dbPath, linkID, faviconPath)
			}
		}(id, domain)
	}

	result := map[string]any{
		"imported": imported,
		"total":    len(parsed),
		"failed":   failed,
	}

	// Include failed items if there were any
	if failed > 0 {
		result["failed_items"] = failedItems
	}

	return result, nil
}

// updateLinkAction handles the update-link action.
func (svc *Service) updateLinkAction(params map[string]any) (any, error) {
	id, ok := params["id"].(string)
	if !ok {
		return nil, ErrMissingID
	}

	// Check if this is just a note update (partial update)
	if note, ok := params["note"].(string); ok && len(params) == 2 { // id + note
		if err := updateNoteOnly(svc.dbPath, id, note); err != nil {
			return nil, err
		}
		link, err := getLink(svc.dbPath, id)
		if err != nil {
			return nil, err
		}
		if link.FaviconPath != "" {
			link.FaviconPath = "/api/links/favicon/" + link.ID
		}
		return link, nil
	}

	// Full update
	url, _ := params["url"].(string)
	name, _ := params["name"].(string)
	notes, _ := params["notes"].(string)
	tags, _ := params["tags"].(string)
	createdAtStr, _ := params["created_at"].(string)
	iconPath, _ := params["icon_path"].(string)

	if err := updateLinkFull(svc.dbPath, id, url, name, notes, tags, createdAtStr); err != nil {
		return nil, err
	}

	// Update icon path if provided
	if iconPath != "" {
		if err := updateFaviconPath(svc.dbPath, id, iconPath); err != nil {
			return nil, err
		}
	}

	link, err := getLink(svc.dbPath, id)
	if err != nil {
		return nil, err
	}
	if link.FaviconPath != "" {
		link.FaviconPath = "/api/links/favicon/" + link.ID
	}

	// Emit search index event
	svc.emitSearchIndexEvent("upsert", link.ID, link.URL, link.Name, link.Notes, link.Tags)

	return link, nil
}

// deleteLinkAction handles the delete-link action.
func (svc *Service) deleteLinkAction(params map[string]any) (any, error) {
	id, ok := params["id"].(string)
	if !ok {
		return nil, ErrMissingID
	}

	if err := deleteLink(svc.dbPath, id); err != nil {
		return nil, err
	}

	// Emit search index delete event if messenger is available
	func() {
		defer func() {
			// Recover from any panics - search events are best-effort
			_ = recover()
		}()

		docID := fmt.Sprintf("links:%s", id)
		evt := search.SearchIndexEvent{
			Op: "delete",
			ID: docID,
		}
		messenger := svc.Deps.MustGetMessenger()
		_ = messenger.Send(core.Message{
			ChannelName: "search.index",
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Data:        evt,
		})
	}()

	return map[string]string{"status": "ok"}, nil
}

// bulkTagAction handles the bulk tag action (add or remove).
func (svc *Service) bulkTagAction(params map[string]any) (any, error) {
	tag, _ := params["tag"].(string)
	if tag == "" {
		return nil, fmt.Errorf("tag is required")
	}

	action, _ := params["action"].(string)
	if action == "" {
		action = "add" // default to add
	}

	idsRaw, ok := params["ids"]
	if !ok {
		return nil, fmt.Errorf("ids are required")
	}

	// Support both []string and []any (from JSON marshaling)
	var ids []string
	switch v := idsRaw.(type) {
	case []string:
		ids = v
	case []any:
		for _, id := range v {
			if s, ok := id.(string); ok {
				ids = append(ids, s)
			}
		}
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("no ids provided")
	}

	// Process each link based on action
	count := 0
	for _, id := range ids {
		link, err := getLink(svc.dbPath, id)
		if err != nil {
			continue
		}

		var newTags string
		if action == "remove" {
			// Remove tag from existing tags
			if link.Tags == "" {
				continue // No tags to remove
			}

			// Split tags and filter out the one to remove
			tagList := strings.Split(link.Tags, ",")
			var filteredTags []string
			found := false
			for _, t := range tagList {
				trimmed := strings.TrimSpace(t)
				if trimmed != tag {
					filteredTags = append(filteredTags, trimmed)
				} else {
					found = true
				}
			}

			if !found {
				continue // Tag not found in this link
			}

			// Rejoin filtered tags
			newTags = strings.Join(filteredTags, ",")
		} else {
			// Add tag to existing tags
			newTags = link.Tags
			if newTags == "" {
				newTags = tag
			} else {
				// Split tags, add new one if not already present
				tagList := strings.Split(newTags, ",")
				found := false
				for _, t := range tagList {
					if strings.TrimSpace(t) == tag {
						found = true
						break
					}
				}
				if !found {
					newTags = newTags + "," + tag
				}
			}
		}

		// Update link with new tags
		if err := updateTags(svc.dbPath, id, newTags); err == nil {
			count++
		}
	}

	return map[string]int{"updated": count}, nil
}

// emitSearchIndexEvent sends a search index event to the search service.
func (svc *Service) emitSearchIndexEvent(op string, id string, url, name, notes, tags string) {
	defer func() {
		// Recover from any panics - search events are best-effort
		_ = recover()
	}()

	var tagList []string
	if tags != "" {
		for _, tag := range strings.Split(tags, ",") {
			if t := strings.TrimSpace(tag); t != "" {
				tagList = append(tagList, t)
			}
		}
	}

	// Use name if available, otherwise extract domain
	title := name
	if title == "" {
		title = extractDomain(url)
	}

	// Build body from notes and URL
	body := notes
	if body == "" {
		body = url
	}

	doc := search.SearchableDocument{
		ID:         fmt.Sprintf("links:%s", id),
		SourceType: "links",
		SourceID:   id,
		Title:      title,
		Body:       body,
		Tags:       tagList,
		URL:        url,
		UpdatedAt:  time.Now(),
	}

	evt := search.SearchIndexEvent{
		Op:       op,
		Document: doc,
		ID:       doc.ID,
	}

	messenger := svc.Deps.MustGetMessenger()
	_ = messenger.Send(core.Message{
		ChannelName: "search.index",
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Data:        evt,
	})
	// Search events are best-effort; ignore errors

}
