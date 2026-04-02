package rss

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"time"

	"keyop/core"
	"keyop/x/webui"
)

//go:embed resources
var embeddedAssets embed.FS

// WebUIAssets returns the static assets for the RSS service.
func (svc *Service) WebUIAssets() http.FileSystem {
	sub, _ := fs.Sub(embeddedAssets, "resources")
	return http.FS(sub)
}

// WebUITab implements webui.TabProvider.
func (svc *Service) WebUITab() webui.TabInfo {
	htmlContent, _ := embeddedAssets.ReadFile("resources/rss.html")
	cssContent, _ := embeddedAssets.ReadFile("resources/rss.css")
	content := string(htmlContent) + "\n<style>\n" + string(cssContent) + "\n</style>"
	return webui.TabInfo{
		ID:      "rss",
		Title:   "📰",
		Content: content,
		JSPath:  "/api/assets/rss/rss-tab.js",
	}
}

// WebUIPanels returns nil — the RSS service has no dashboard panels.
func (svc *Service) WebUIPanels() []webui.PanelInfo {
	return nil
}

// HandleWebUIAction handles actions from the WebUI.
func (svc *Service) HandleWebUIAction(action string, params map[string]any) (any, error) {
	result, err := svc.handleWebUIActionInternal(action, params)
	if err != nil {
		// Send error to error channel if configured
		if errorChan, ok := svc.Cfg.Pubs["errors"]; ok {
			messenger := svc.Deps.MustGetMessenger()
			messenger.Send(core.Message{
				ChannelName: errorChan.Name,
				ServiceName: svc.Cfg.Name,
				Event:       action,
				Status:      "error",
				Uuid:        fmt.Sprintf("rss-error-%s", action),
				Timestamp:   time.Now(),
			})
		}
	}
	return result, err
}

func (svc *Service) handleWebUIActionInternal(action string, params map[string]any) (any, error) {
	switch action {
	case "fetch-articles":
		return svc.fetchArticles(params)
	case "fetch-feeds":
		return svc.fetchFeeds()
	case "fetch-article":
		return svc.fetchArticle(params)
	case "mark-seen":
		return svc.markSeen(params)
	case "mark-unseen":
		return svc.markUnseen(params)
	case "mark-read-later":
		return svc.markReadLater(params)
	case "unmark-read-later":
		return svc.unmarkReadLater(params)
	case "mark-done-reading":
		return svc.markDoneReading(params)
	case "fetch-unseen-count":
		return svc.fetchUnseenCount()
	case "delete-article":
		return svc.deleteArticle(params)
	case "add-tag":
		return svc.addTag(params)
	case "remove-tag":
		return svc.removeTag(params)
	case "fetch-all-tags":
		return svc.fetchAllTags()
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

type articleRow struct {
	ID          int64  `json:"id"`
	Timestamp   string `json:"timestamp"`
	FeedURL     string `json:"feedUrl"`
	FeedTitle   string `json:"feedTitle"`
	GUID        string `json:"guid"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Link        string `json:"link"`
	Published   string `json:"published"`
	Seen        bool   `json:"seen"`
	ReadLater   bool   `json:"readLater"`
	Tags        string `json:"tags"`
}

// fetchArticles queries the rss_articles table ordered by published DESC.
// Pass show_seen=true to include already-seen articles.
func (svc *Service) fetchArticles(params map[string]any) (any, error) {
	logger := svc.Deps.MustGetLogger()

	if svc.db == nil || *svc.db == nil {
		logger.Error("rss: fetchArticles - database not available")
		return map[string]any{"articles": []any{}, "total": 0}, nil
	}

	logger.Debug("rss: fetchArticles called", "view", params["view"])

	db := *svc.db
	feedURL, _ := params["feed_url"].(string)
	view, _ := params["view"].(string) // 'unseen' | 'read-later' | 'seen' | 'all'
	q, _ := params["q"].(string)
	full, _ := params["full"].(bool)
	offset := 0
	limit := 200
	if o, ok := params["offset"].(float64); ok {
		offset = int(o)
	}
	if l, ok := params["limit"].(float64); ok {
		limit = int(l)
	}

	// If contains-id is provided, find which page the article is on
	var containsID int64
	if cid, ok := params["contains-id"].(float64); ok {
		containsID = int64(cid)
	} else if cidStr, ok := params["contains-id"].(string); ok {
		fmt.Sscanf(cidStr, "%d", &containsID)
	}

	where := []string{"1=1"}
	args := []any{}

	if feedURL != "" {
		where = append(where, "feed_url = ?")
		args = append(args, feedURL)
	}
	switch view {
	case "read-later":
		where = append(where, "COALESCE(read_later,0) = 1")
	case "seen":
		where = append(where, "COALESCE(seen,0) = 1")
		where = append(where, "COALESCE(read_later,0) = 0")
	case "all":
		// no filter
	default: // "unseen"
		where = append(where, "COALESCE(seen,0) = 0")
		where = append(where, "COALESCE(read_later,0) = 0")
	}
	if q != "" {
		like := "%" + q + "%"
		if full {
			where = append(where, "(title LIKE ? OR description LIKE ?)")
			args = append(args, like, like)
		} else {
			where = append(where, "title LIKE ?")
			args = append(args, like)
		}
	}

	whereClause := strings.Join(where, " AND ")

	// If contains-id is set, find the article's position and calculate the offset
	if containsID > 0 {
		// Count how many articles come before this one
		countBeforeQuery := fmt.Sprintf(`SELECT COUNT(*) FROM rss_articles 
			WHERE %s AND published >= (SELECT published FROM rss_articles WHERE id = ?)`, whereClause)
		var countBefore int
		err := db.QueryRow(countBeforeQuery, append(args, containsID)...).Scan(&countBefore)
		if err == nil && countBefore > 0 {
			// Position 0 means the article is first, so offset = 0
			// Position 1 means it's 2nd, so offset = 0 (still on first page)
			// Calculate page: position / pageSize, then offset = page * pageSize
			offset = ((countBefore - 1) / limit) * limit
			logger.Debug("rss: found article at position", "id", containsID, "position", countBefore, "offset", offset)
		}
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM rss_articles WHERE %s", whereClause)
	var total int
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		total = 0
	}

	// #nosec G201 - where is built from constant SQL fragments (safe, no injection)
	// Try to select with tags column; fall back to without if column doesn't exist
	queryWithTags := fmt.Sprintf(`SELECT id, timestamp, feed_url, feed_title, guid, title, description, link, published, COALESCE(seen,0), COALESCE(read_later,0), COALESCE(tags,'')
		 FROM rss_articles WHERE %s ORDER BY published DESC LIMIT ? OFFSET ?`, whereClause)

	rows, err := db.Query(queryWithTags, append(args, limit, offset)...)
	if err != nil {
		// If query fails, it might be because tags column doesn't exist
		// Try without tags column
		queryWithoutTags := fmt.Sprintf(`SELECT id, timestamp, feed_url, feed_title, guid, title, description, link, published, COALESCE(seen,0), COALESCE(read_later,0)
			 FROM rss_articles WHERE %s ORDER BY published DESC LIMIT ? OFFSET ?`, whereClause)
		rows, err = db.Query(queryWithoutTags, append(args, limit, offset)...)
		if err != nil {
			return nil, fmt.Errorf("rss: failed to query articles: %w", err)
		}
		result, _ := fetchArticlesWithoutTags(rows)
		if resultMap, ok := result.(map[string]any); ok {
			resultMap["total"] = total
			resultMap["offset"] = offset
			return resultMap, nil
		}
		return result, nil
	}
	defer func() { _ = rows.Close() }()

	articles := make([]articleRow, 0)
	for rows.Next() {
		var r articleRow
		var timestamp, published time.Time
		var seenInt, readLaterInt int
		if err := rows.Scan(&r.ID, &timestamp, &r.FeedURL, &r.FeedTitle, &r.GUID, &r.Title, &r.Description, &r.Link, &published, &seenInt, &readLaterInt, &r.Tags); err != nil {
			continue
		}
		r.Timestamp = timestamp.Format(time.RFC3339)
		r.Published = published.Format(time.RFC3339)
		r.Seen = seenInt != 0
		r.ReadLater = readLaterInt != 0
		articles = append(articles, r)
	}
	return map[string]any{"articles": articles, "total": total, "offset": offset}, nil
}

// fetchArticlesWithoutTags processes rows from a query without the tags column
func fetchArticlesWithoutTags(rows *sql.Rows) (any, error) {
	defer func() { _ = rows.Close() }()

	articles := make([]articleRow, 0)
	for rows.Next() {
		var r articleRow
		var timestamp, published time.Time
		var seenInt, readLaterInt int
		if err := rows.Scan(&r.ID, &timestamp, &r.FeedURL, &r.FeedTitle, &r.GUID, &r.Title, &r.Description, &r.Link, &published, &seenInt, &readLaterInt); err != nil {
			continue
		}
		r.Timestamp = timestamp.Format(time.RFC3339)
		r.Published = published.Format(time.RFC3339)
		r.Seen = seenInt != 0
		r.ReadLater = readLaterInt != 0
		r.Tags = ""
		articles = append(articles, r)
	}
	return map[string]any{"articles": articles}, nil
}

// fetchArticle returns a single article by id.
func (svc *Service) fetchArticle(params map[string]any) (any, error) {
	if svc.db == nil || *svc.db == nil {
		return nil, fmt.Errorf("rss: database not available")
	}
	id, ok := params["id"].(float64)
	if !ok {
		return nil, fmt.Errorf("rss: id param required")
	}
	db := *svc.db
	var r articleRow
	var timestamp, published time.Time

	// Try with tags column first
	err := db.QueryRow(
		`SELECT id, timestamp, feed_url, feed_title, guid, title, description, link, published, COALESCE(tags,'')
		 FROM rss_articles WHERE id = ?`, int64(id),
	).Scan(&r.ID, &timestamp, &r.FeedURL, &r.FeedTitle, &r.GUID, &r.Title, &r.Description, &r.Link, &published, &r.Tags)

	if err != nil {
		// Try without tags column
		err = db.QueryRow(
			`SELECT id, timestamp, feed_url, feed_title, guid, title, description, link, published
			 FROM rss_articles WHERE id = ?`, int64(id),
		).Scan(&r.ID, &timestamp, &r.FeedURL, &r.FeedTitle, &r.GUID, &r.Title, &r.Description, &r.Link, &published)
		if err != nil {
			return nil, fmt.Errorf("rss: article not found: %w", err)
		}
		r.Tags = ""
	}

	r.Timestamp = timestamp.Format(time.RFC3339)
	r.Published = published.Format(time.RFC3339)
	return r, nil
}

// fetchFeeds returns configured feeds with per-feed article counts from SQLite.
func (svc *Service) fetchFeeds() (any, error) {
	logger := svc.Deps.MustGetLogger()

	// Check if database is available
	if svc.db == nil || *svc.db == nil {
		logger.Error("rss: database not available - sqlite-rss service not enabled or failed to initialize")
		return nil, fmt.Errorf("rss: sqlite database instance not available - ensure sqlite-rss service is enabled")
	}

	logger.Debug("rss: fetchFeeds called, database is available")

	feeds, err := parseFeedConfigs(svc.Cfg.Config)
	if err != nil {
		logger.Warn("rss: failed to parse feed configs", "error", err)
		return nil, fmt.Errorf("rss: %w", err)
	}

	logger.Debug("rss: fetchFeeds - configured feeds", "count", len(feeds))

	type feedRow struct {
		URL         string `json:"url"`
		Title       string `json:"title"`
		Count       int    `json:"count"`
		UnseenCount int    `json:"unseenCount"`
	}

	type countRow struct{ total, unseen int }
	counts := map[string]countRow{}

	db := *svc.db
	rows, qErr := db.Query(
		`SELECT feed_url, COUNT(*), SUM(CASE WHEN COALESCE(seen,0)=0 AND COALESCE(read_later,0)=0 THEN 1 ELSE 0 END)
		 FROM rss_articles GROUP BY feed_url`)
	if qErr == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var u string
			var total, unseen int
			if rows.Scan(&u, &total, &unseen) == nil {
				counts[u] = countRow{total, unseen}
			}
		}
	}

	// If no feeds are configured, extract them from the database
	if len(feeds) == 0 {
		rows, qErr := db.Query(
			`SELECT DISTINCT feed_url, feed_title FROM rss_articles ORDER BY feed_title`)
		if qErr == nil {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var url, title string
				if rows.Scan(&url, &title) == nil {
					feeds = append(feeds, feedConfig{URL: url, Title: title})
				}
			}
		}
	}

	result := make([]feedRow, 0, len(feeds))
	for _, f := range feeds {
		c := counts[f.URL]
		result = append(result, feedRow{URL: f.URL, Title: f.Title, Count: c.total, UnseenCount: c.unseen})
	}
	return map[string]any{"feeds": result}, nil
}

// markSeen sets seen=1 on the given article id.
func (svc *Service) markSeen(params map[string]any) (any, error) {
	if svc.db == nil || *svc.db == nil {
		return nil, fmt.Errorf("rss: database not available")
	}
	id, ok := params["id"].(float64)
	if !ok {
		return nil, fmt.Errorf("rss: id param required")
	}
	_, err := (*svc.db).Exec(`UPDATE rss_articles SET seen=1 WHERE id=?`, int64(id))
	if err != nil {
		return nil, fmt.Errorf("rss: mark-seen failed: %w", err)
	}
	return map[string]any{"ok": true}, nil
}

// markUnseen sets seen=0 on the given article id.
func (svc *Service) markUnseen(params map[string]any) (any, error) {
	if svc.db == nil || *svc.db == nil {
		return nil, fmt.Errorf("rss: database not available")
	}
	id, ok := params["id"].(float64)
	if !ok {
		return nil, fmt.Errorf("rss: id param required")
	}
	_, err := (*svc.db).Exec(`UPDATE rss_articles SET seen=0 WHERE id=?`, int64(id))
	if err != nil {
		return nil, fmt.Errorf("rss: mark-unseen failed: %w", err)
	}
	return map[string]any{"ok": true}, nil
}

// markReadLater sets read_later=1 and seen=1 on the given article id.
// Articles marked as read-later are automatically marked as seen.
func (svc *Service) markReadLater(params map[string]any) (any, error) {
	if svc.db == nil || *svc.db == nil {
		return nil, fmt.Errorf("rss: database not available")
	}
	id, ok := params["id"].(float64)
	if !ok {
		return nil, fmt.Errorf("rss: id param required")
	}
	_, err := (*svc.db).Exec(`UPDATE rss_articles SET read_later=1, seen=1 WHERE id=?`, int64(id))
	if err != nil {
		return nil, fmt.Errorf("rss: mark-read-later failed: %w", err)
	}
	return map[string]any{"ok": true}, nil
}

// unmarkReadLater sets read_later=0 on the given article id.
func (svc *Service) unmarkReadLater(params map[string]any) (any, error) {
	if svc.db == nil || *svc.db == nil {
		return nil, fmt.Errorf("rss: database not available")
	}
	id, ok := params["id"].(float64)
	if !ok {
		return nil, fmt.Errorf("rss: id param required")
	}
	_, err := (*svc.db).Exec(`UPDATE rss_articles SET read_later=0 WHERE id=?`, int64(id))
	if err != nil {
		return nil, fmt.Errorf("rss: unmark-read-later failed: %w", err)
	}
	return map[string]any{"ok": true}, nil
}

// markDoneReading clears read_later and sets seen=1 — used when finishing a read-later article.
func (svc *Service) markDoneReading(params map[string]any) (any, error) {
	if svc.db == nil || *svc.db == nil {
		return nil, fmt.Errorf("rss: database not available")
	}
	id, ok := params["id"].(float64)
	if !ok {
		return nil, fmt.Errorf("rss: id param required")
	}
	_, err := (*svc.db).Exec(`UPDATE rss_articles SET seen=1, read_later=0 WHERE id=?`, int64(id))
	if err != nil {
		return nil, fmt.Errorf("rss: mark-done-reading failed: %w", err)
	}
	return map[string]any{"ok": true}, nil
}
func (svc *Service) fetchUnseenCount() (any, error) {
	if svc.db == nil || *svc.db == nil {
		return map[string]any{"count": 0}, nil
	}
	var count int
	err := (*svc.db).QueryRow(
		`SELECT COUNT(*) FROM rss_articles WHERE COALESCE(seen,0)=0 AND COALESCE(read_later,0)=0`,
	).Scan(&count)
	if err != nil {
		return map[string]any{"count": 0}, nil
	}
	return map[string]any{"count": count}, nil
}

// deleteArticle removes an article from the database.
func (svc *Service) deleteArticle(params map[string]any) (any, error) {
	if svc.db == nil || *svc.db == nil {
		return nil, fmt.Errorf("rss: database not available")
	}
	id, ok := params["id"].(float64)
	if !ok {
		return nil, fmt.Errorf("rss: id param required")
	}
	_, err := (*svc.db).Exec(`DELETE FROM rss_articles WHERE id=?`, int64(id))
	if err != nil {
		return nil, fmt.Errorf("rss: delete-article failed: %w", err)
	}
	return map[string]any{"ok": true}, nil
}

// addTag adds a tag to an article (tags are comma-separated).
func (svc *Service) addTag(params map[string]any) (any, error) {
	logger := svc.Deps.MustGetLogger()

	if svc.db == nil || *svc.db == nil {
		return nil, fmt.Errorf("rss: database not available")
	}
	id, ok := params["id"].(float64)
	if !ok {
		return nil, fmt.Errorf("rss: id param required")
	}
	tag, ok := params["tag"].(string)
	if !ok || tag == "" {
		return nil, fmt.Errorf("rss: tag param required")
	}

	logger.Debug("addTag", "id", int64(id), "tag", tag)

	var currentTags string
	err := (*svc.db).QueryRow(`SELECT COALESCE(tags,'') FROM rss_articles WHERE id=?`, int64(id)).Scan(&currentTags)
	if err != nil {
		return nil, fmt.Errorf("rss: article not found: %w", err)
	}

	logger.Debug("currentTags", "tags", currentTags)

	// Parse existing tags
	var tagList []string
	if currentTags != "" {
		tagList = strings.Split(currentTags, ",")
	}

	// Add tag if not already present
	tagExists := false
	for _, t := range tagList {
		if strings.TrimSpace(t) == tag {
			tagExists = true
			break
		}
	}
	if !tagExists {
		tagList = append(tagList, tag)
	}

	newTags := strings.Join(tagList, ",")
	logger.Debug("newTags", "tags", newTags)

	_, err = (*svc.db).Exec(`UPDATE rss_articles SET tags=? WHERE id=?`, newTags, int64(id))
	if err != nil {
		return nil, fmt.Errorf("rss: add-tag failed: %w", err)
	}

	logger.Debug("tag added successfully")
	return map[string]any{"ok": true, "tags": newTags}, nil
}

// removeTag removes a tag from an article.
func (svc *Service) removeTag(params map[string]any) (any, error) {
	if svc.db == nil || *svc.db == nil {
		return nil, fmt.Errorf("rss: database not available")
	}
	id, ok := params["id"].(float64)
	if !ok {
		return nil, fmt.Errorf("rss: id param required")
	}
	tag, ok := params["tag"].(string)
	if !ok || tag == "" {
		return nil, fmt.Errorf("rss: tag param required")
	}

	var currentTags string
	err := (*svc.db).QueryRow(`SELECT COALESCE(tags,'') FROM rss_articles WHERE id=?`, int64(id)).Scan(&currentTags)
	if err != nil {
		return nil, fmt.Errorf("rss: article not found: %w", err)
	}

	// Parse existing tags
	var tagList []string
	if currentTags != "" {
		tagList = strings.Split(currentTags, ",")
	}

	// Remove the tag
	filtered := make([]string, 0, len(tagList))
	for _, t := range tagList {
		if strings.TrimSpace(t) != tag {
			filtered = append(filtered, t)
		}
	}

	newTags := strings.Join(filtered, ",")
	_, err = (*svc.db).Exec(`UPDATE rss_articles SET tags=? WHERE id=?`, newTags, int64(id))
	if err != nil {
		return nil, fmt.Errorf("rss: remove-tag failed: %w", err)
	}
	return map[string]any{"ok": true, "tags": newTags}, nil
}

// fetchAllTags returns all unique tags used across articles.
func (svc *Service) fetchAllTags() (any, error) {
	if svc.db == nil || *svc.db == nil {
		return map[string]any{"tags": []string{}}, nil
	}

	rows, err := (*svc.db).Query(`SELECT DISTINCT tags FROM rss_articles WHERE tags != ''`)
	if err != nil {
		return map[string]any{"tags": []string{}}, nil
	}
	defer func() { _ = rows.Close() }()

	tagSet := make(map[string]bool)
	for rows.Next() {
		var tagsStr string
		if err := rows.Scan(&tagsStr); err != nil {
			continue
		}
		if tagsStr != "" {
			tags := strings.Split(tagsStr, ",")
			for _, t := range tags {
				t = strings.TrimSpace(t)
				if t != "" {
					tagSet[t] = true
				}
			}
		}
	}

	// Convert to sorted list
	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	sort.Strings(tags)

	return map[string]any{"tags": tags}, nil
}
