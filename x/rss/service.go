package rss

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"keyop/core"
	"keyop/x/search"

	"github.com/mmcdole/gofeed"
)

const (
	defaultMaxAge = 720 * time.Hour // 30 days
	httpTimeout   = 15 * time.Second
	httpUserAgent = "keyop-rss/1.0"
	maxArticles   = 1000
)

// feedConfig holds the parsed configuration for a single RSS/Atom feed.
type feedConfig struct {
	URL   string
	Title string // optional override; feed.Title is used if empty
}

// rssState is the persisted state for the RSS service.
type rssState struct {
	SeenGUIDs map[string]time.Time `json:"seenGuids"`
}

// Service polls RSS/Atom feeds and emits ArticleEvent messages for new items.
type Service struct {
	Deps   core.Dependencies
	Cfg    core.ServiceConfig
	db     **sql.DB
	dbPath string
}

// NewService creates a new RSS service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{Deps: deps, Cfg: cfg}
}

// ValidateConfig validates the service configuration.
// Note: feeds and frequency are optional to allow web-UI-only instances.
func (svc *Service) ValidateConfig() []error {
	var errs []error

	// feeds are optional (allows web UI without polling)
	if raw, ok := svc.Cfg.Config["max_age"].(string); ok && raw != "" {
		if _, err := time.ParseDuration(raw); err != nil {
			errs = append(errs, fmt.Errorf("rss: invalid max_age %q: %w", raw, err))
		}
	}

	return errs
}

// Initialize does not subscribe to any channels; polling is driven by Check().
func (svc *Service) Initialize() error {
	logger := svc.Deps.MustGetLogger()

	// Run migrations for columns added after the initial schema.
	// Each ALTER TABLE is executed separately so a "duplicate column" error
	// on an already-migrated DB doesn't block the others.
	if svc.db != nil && *svc.db != nil {
		db := *svc.db
		for _, stmt := range []string{
			`ALTER TABLE rss_articles ADD COLUMN seen INTEGER DEFAULT 0`,
			`ALTER TABLE rss_articles ADD COLUMN read_later INTEGER DEFAULT 0`,
			`ALTER TABLE rss_articles ADD COLUMN tags TEXT DEFAULT ''`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				// "duplicate column name" is expected on already-migrated databases.
				if !isDuplicateColumnErr(err) {
					logger.Error("rss migration failed", "stmt", stmt, "err", err)
					return fmt.Errorf("rss: migration failed: %w", err)
				}
				logger.Debug("rss migration: column already exists", "stmt", stmt)
			} else {
				logger.Debug("rss migration: column added", "stmt", stmt)
			}
		}
	}
	return nil
}

// isDuplicateColumnErr returns true when SQLite reports a column already exists.
func isDuplicateColumnErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate column") || strings.Contains(msg, "already exists")
}

// Check polls all configured feeds and emits messages for new articles.
func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	maxAge := defaultMaxAge
	if raw, ok := svc.Cfg.Config["max_age"].(string); ok && raw != "" {
		if d, err := time.ParseDuration(raw); err == nil {
			maxAge = d
		}
	}

	feeds, err := parseFeedConfigs(svc.Cfg.Config)
	if err != nil {
		// If feeds can't be parsed, just skip polling (web UI still works)
		logger.Debug("rss: no feeds configured, skipping poll", "error", err)
		return nil
	}

	if len(feeds) == 0 {
		// No feeds configured; web UI mode only
		logger.Debug("rss: no feeds configured, skipping poll")
		return nil
	}

	// Load persisted state.
	stateKey := svc.Cfg.Name + "_seen"
	var state rssState
	if store := svc.Deps.GetStateStore(); store != nil {
		if err := store.Load(stateKey, &state); err != nil {
			// Not-found is expected on first run.
			logger.Debug("rss: no existing state (first run or not found)", "key", stateKey)
		}
	}
	if state.SeenGUIDs == nil {
		state.SeenGUIDs = make(map[string]time.Time)
	}

	httpClient := &http.Client{
		Timeout: httpTimeout,
	}

	totalNew := 0
	cutoff := time.Now().Add(-maxAge)

	for _, fc := range feeds {
		fp := gofeed.NewParser()
		fp.Client = httpClient
		fp.UserAgent = httpUserAgent

		feed, err := fp.ParseURL(fc.URL)
		if err != nil {
			logger.Warn("rss: failed to fetch feed", "url", fc.URL, "error", err)
			continue
		}

		feedTitle := fc.Title
		if feedTitle == "" {
			feedTitle = feed.Title
		}

		for _, item := range feed.Items {
			guid := item.GUID
			if guid == "" {
				guid = item.Link
			}
			if guid == "" {
				continue
			}

			// Skip already-seen articles.
			if _, seen := state.SeenGUIDs[guid]; seen {
				continue
			}

			// Determine published time.
			var published time.Time
			switch {
			case item.PublishedParsed != nil:
				published = *item.PublishedParsed
			case item.UpdatedParsed != nil:
				published = *item.UpdatedParsed
			default:
				published = time.Now()
			}

			// Skip articles older than max_age.
			if published.Before(cutoff) {
				state.SeenGUIDs[guid] = published
				continue
			}

			// Get description from content, description, or media:description (YouTube feeds)
			body := getItemDescription(item)

			event := &ArticleEvent{
				GUID:        guid,
				Title:       item.Title,
				Description: body,
				Link:        item.Link,
				Published:   published,
				FeedTitle:   feedTitle,
				FeedURL:     fc.URL,
			}

			if err := messenger.Send(core.Message{
				ChannelName: svc.Cfg.Name,
				ServiceName: svc.Cfg.Name,
				ServiceType: svc.Cfg.Type,
				Event:       "new_article",
				Text:        item.Title,
				Summary:     feedTitle,
				MetricName:  "rss." + svc.Cfg.Name + ".new_articles",
				Metric:      1,
				Data:        event,
			}); err != nil {
				logger.Error("rss: failed to send article event", "guid", guid, "error", err)
			}

			state.SeenGUIDs[guid] = published
			totalNew++
		}
	}

	// Prune entries older than 2× max_age to prevent unbounded map growth.
	pruneCutoff := time.Now().Add(-2 * maxAge)
	for guid, ts := range state.SeenGUIDs {
		if ts.Before(pruneCutoff) {
			delete(state.SeenGUIDs, guid)
		}
	}

	// Persist updated state.
	if store := svc.Deps.GetStateStore(); store != nil {
		if err := store.Save(stateKey, &state); err != nil {
			logger.Error("rss: failed to save state", "error", err)
		}
	}

	if totalNew > 0 {
		logger.Info("rss: emitted new articles", "count", totalNew, "service", svc.Cfg.Name)
	}

	return nil
}

// parseFeedConfigs parses the "feeds" key from a service config map.
// Returns an empty list if feeds is not configured (web UI only mode).
func parseFeedConfigs(cfg map[string]any) ([]feedConfig, error) {
	raw, ok := cfg["feeds"]
	if !ok {
		// Feeds are optional - allows web UI without polling
		return []feedConfig{}, nil
	}

	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("feeds must be a list")
	}

	var feeds []feedConfig
	for i, entry := range list {
		m, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("feeds[%d] must be a map", i)
		}
		url, _ := m["url"].(string)
		if url == "" {
			return nil, fmt.Errorf("feeds[%d] must have a non-empty url", i)
		}
		title, _ := m["title"].(string)
		feeds = append(feeds, feedConfig{URL: url, Title: title})
	}

	return feeds, nil
}

// SearchSourceType implements search.IndexProvider.
func (svc *Service) SearchSourceType() string {
	return "rss"
}

// getItemDescription extracts description from an item, preferring content > description > media:description.
func getItemDescription(item *gofeed.Item) string {
	// Prefer full content over summary description
	if item.Content != "" {
		return item.Content
	}

	if item.Description != "" {
		return item.Description
	}

	// Check for media:description (used by YouTube feeds)
	if item.Extensions != nil {
		if mediaExt, ok := item.Extensions["media"]; ok {
			// mediaExt is map[string][]Extension
			if descriptions, ok := mediaExt["description"]; ok && len(descriptions) > 0 {
				// Extension has a Value field
				if descriptions[0].Value != "" {
					return descriptions[0].Value
				}
			}
		}
	}

	return ""
}

// stripHTML removes HTML tags from text for clean search indexing.
func stripHTML(html string) string {
	var result strings.Builder
	inTag := false
	for _, r := range html {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			result.WriteRune(' ')
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}

	// Clean up whitespace
	text := result.String()
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.Join(strings.Fields(text), " ")
	return text
}

// BulkIndex implements search.IndexProvider and returns all RSS articles for indexing.
func (svc *Service) BulkIndex() (<-chan search.SearchableDocument, error) {
	ch := make(chan search.SearchableDocument)
	go func() {
		defer close(ch)

		if svc.db == nil || *svc.db == nil {
			return
		}
		db := *svc.db

		rows, err := db.Query(`
			SELECT id, title, description, link, published, feed_title, COALESCE(tags,'')
			FROM rss_articles
			ORDER BY published DESC
		`)
		if err != nil {
			// If query fails, might be because tags column doesn't exist
			rows, err = db.Query(`
				SELECT id, title, description, link, published, feed_title, ''
				FROM rss_articles
				ORDER BY published DESC
			`)
			if err != nil {
				return
			}
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var id int64
			var title, description, link, feedTitle, tagsStr string
			var published time.Time
			if err := rows.Scan(&id, &title, &description, &link, &published, &feedTitle, &tagsStr); err != nil {
				continue
			}

			tags := []string{}
			if tagsStr != "" {
				for _, t := range strings.Split(tagsStr, ",") {
					t = strings.TrimSpace(t)
					if t != "" {
						tags = append(tags, t)
					}
				}
			}

			ch <- search.SearchableDocument{
				ID:         fmt.Sprintf("rss:%d", id),
				SourceType: "rss",
				SourceID:   fmt.Sprintf("%d", id),
				Title:      title,
				Body:       stripHTML(description),
				URL:        link,
				UpdatedAt:  published,
				Tags:       tags,
				Extra: map[string]string{
					"feed_title": feedTitle,
				},
			}
		}
	}()
	return ch, nil
}
