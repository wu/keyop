package core

import (
	"database/sql"
	"time"
)

// -------------------------
// SQLite coordination types
// -------------------------

// InsertContext provides metadata about a message being inserted into SQLite.
type InsertContext struct {
	Payload     interface{}
	Timestamp   string // ISO 8601 string
	Hostname    string
	ServiceName string
	ServiceType string
}

// SchemaProvider is implemented by services that contribute a SQLite schema.
type SchemaProvider interface {
	SQLiteSchema() string
	SQLiteInsert(ctx *InsertContext) (query string, args []any)
}

// SQLiteConsumer is implemented by services that need a SQLite DB handle.
type SQLiteConsumer interface {
	SetSQLiteDB(db **sql.DB)
}

// SQLiteCoordinator is implemented by the sqlite service.
// core/runtime uses this to wire schema providers and consumers without importing services/sqlite.
type SQLiteCoordinator interface {
	AcceptsPayloadType(payloadType string) bool
	RegisterProvider(payloadType string, provider SchemaProvider)
	GetSQLiteDB() **sql.DB
	SetDBPath(payloadType string, path string)
}

// -------------------------
// WebUI coordination types
// -------------------------

// TabInfo contains metadata and content for a UI tab.
type TabInfo struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Icon           string `json:"icon,omitempty"`
	Content        string `json:"content"`
	JSPath         string `json:"jsPath,omitempty"`
	RenderMarkdown bool   `json:"renderMarkdown,omitzero"`
}

// PanelInfo describes a small widget shown on the dashboard tab.
type PanelInfo struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	JSPath      string `json:"jsPath,omitempty"`
	ServiceType string `json:"serviceType,omitempty"`
	Event       string `json:"event,omitempty"`
}

// TabProvider is implemented by services that contribute a web UI tab.
type TabProvider interface {
	WebUITab() TabInfo
}

// PanelProvider is implemented by services that expose one or more dashboard panels.
type PanelProvider interface {
	WebUIPanels() []PanelInfo
}

// WebUICoordinator is implemented by the webui service.
// core/runtime uses this to wire tab and panel providers without importing services/webui.
type WebUICoordinator interface {
	ServiceType() string // returns Cfg.Type; needed for webui self-registration
	RegisterProvider(serviceType string, provider TabProvider)
	RegisterPanelProvider(serviceType string, provider PanelProvider)
}

// -------------------------
// Search coordination types
// -------------------------

// SearchableDocument is the indexing unit for the search service.
type SearchableDocument struct {
	ID         string // globally unique: "<sourceType>:<sourceID>"
	SourceType string
	SourceID   string
	Title      string
	Body       string
	Tags       []string
	URL        string
	UpdatedAt  time.Time
	Extra      map[string]string
}

// IndexProvider is implemented by services that participate in full-text search.
type IndexProvider interface {
	SearchSourceType() string
	BulkIndex() (<-chan SearchableDocument, error)
}

// SearchCoordinator is implemented by the search service.
// core/runtime uses this to register index providers without importing services/search.
type SearchCoordinator interface {
	RegisterIndexProvider(p IndexProvider)
}
