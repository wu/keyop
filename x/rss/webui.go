package rss

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

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
	switch action {
	case "fetch-articles":
		return svc.fetchArticles(params)
	case "fetch-feeds":
		return svc.fetchFeeds()
	case "fetch-article":
		return svc.fetchArticle(params)
	case "mark-seen":
		return svc.markSeen(params)
	case "mark-read-later":
		return svc.markReadLater(params)
	case "mark-done-reading":
		return svc.markDoneReading(params)
	case "fetch-unseen-count":
		return svc.fetchUnseenCount()
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
}

// fetchArticles queries the rss_articles table ordered by published DESC.
// Pass show_seen=true to include already-seen articles.
func (svc *Service) fetchArticles(params map[string]any) (any, error) {
	if svc.db == nil || *svc.db == nil {
		return map[string]any{"articles": []any{}}, nil
	}
	db := *svc.db
	feedURL, _ := params["feed_url"].(string)
	view, _ := params["view"].(string) // 'unseen' | 'read-later' | 'seen' | 'all'
	q, _ := params["q"].(string)
	full, _ := params["full"].(bool)

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

	query := fmt.Sprintf(`SELECT id, timestamp, feed_url, feed_title, guid, title, description, link, published, COALESCE(seen,0), COALESCE(read_later,0)
		 FROM rss_articles WHERE %s ORDER BY published DESC LIMIT 200`, strings.Join(where, " AND "))

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("rss: failed to query articles: %w", err)
	}
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
	err := db.QueryRow(
		`SELECT id, timestamp, feed_url, feed_title, guid, title, description, link, published
		 FROM rss_articles WHERE id = ?`, int64(id),
	).Scan(&r.ID, &timestamp, &r.FeedURL, &r.FeedTitle, &r.GUID, &r.Title, &r.Description, &r.Link, &published)
	if err != nil {
		return nil, fmt.Errorf("rss: article not found: %w", err)
	}
	r.Timestamp = timestamp.Format(time.RFC3339)
	r.Published = published.Format(time.RFC3339)
	return r, nil
}

// fetchFeeds returns configured feeds with per-feed article counts from SQLite.
func (svc *Service) fetchFeeds() (any, error) {
	feeds, err := parseFeedConfigs(svc.Cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("rss: %w", err)
	}

	type feedRow struct {
		URL         string `json:"url"`
		Title       string `json:"title"`
		Count       int    `json:"count"`
		UnseenCount int    `json:"unseenCount"`
	}

	type countRow struct{ total, unseen int }
	counts := map[string]countRow{}
	if svc.db != nil && *svc.db != nil {
		rows, qErr := (*svc.db).Query(
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

// markReadLater sets read_later=1 on the given article id.
func (svc *Service) markReadLater(params map[string]any) (any, error) {
	if svc.db == nil || *svc.db == nil {
		return nil, fmt.Errorf("rss: database not available")
	}
	id, ok := params["id"].(float64)
	if !ok {
		return nil, fmt.Errorf("rss: id param required")
	}
	_, err := (*svc.db).Exec(`UPDATE rss_articles SET read_later=1 WHERE id=?`, int64(id))
	if err != nil {
		return nil, fmt.Errorf("rss: mark-read-later failed: %w", err)
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
