package rss

import (
	"database/sql"
	"encoding/json"
	"keyop/core"
	"time"
)

// SQLiteSchema returns the DDL for the rss_articles table.
func (svc *Service) SQLiteSchema() string {
	return `CREATE TABLE IF NOT EXISTS rss_articles (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp   DATETIME,
		feed_url    TEXT,
		feed_title  TEXT,
		guid        TEXT,
		title       TEXT,
		description TEXT,
		link        TEXT,
		published   DATETIME,
		data        TEXT,
		seen        INTEGER DEFAULT 0,
		read_later  INTEGER DEFAULT 0,
		tags        TEXT DEFAULT ''
	);
	CREATE UNIQUE INDEX IF NOT EXISTS rss_articles_guid ON rss_articles(guid);`
}

// SQLiteInsert returns an INSERT OR IGNORE for new_article events.
// Returns empty query for unrelated events.
func (svc *Service) SQLiteInsert(msg core.Message) (string, []any) {
	if msg.Event != "new_article" {
		return "", nil
	}

	var event *ArticleEvent
	switch v := msg.Data.(type) {
	case *ArticleEvent:
		event = v
	case ArticleEvent:
		event = &v
	default:
		return "", nil
	}

	if event == nil {
		return "", nil
	}

	var dataJSON string
	if b, err := json.Marshal(event); err == nil {
		dataJSON = string(b)
	}

	return `INSERT OR IGNORE INTO rss_articles
		(timestamp, feed_url, feed_title, guid, title, description, link, published, data)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		[]any{
			time.Now().UTC(),
			event.FeedURL,
			event.FeedTitle,
			event.GUID,
			event.Title,
			event.Description,
			event.Link,
			event.Published.UTC(),
			dataJSON,
		}
}

// SetSQLiteDB allows the runtime to provide a pointer to the shared DB instance.
func (svc *Service) SetSQLiteDB(db **sql.DB) {
	svc.db = db
	if db != nil {
		logger := svc.Deps.MustGetLogger()
		logger.Debug("rss: SetSQLiteDB called successfully", "dbPath", svc.dbPath)
	}
}

// SetDBPath allows the runtime to provide the database file path.
func (svc *Service) SetDBPath(path string) {
	svc.dbPath = path
}

// PayloadTypes returns the payload types this provider handles.
func (svc *Service) PayloadTypes() []string {
	return []string{"service.rss.article.v1"}
}
