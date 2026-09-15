package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// -------------------------
// SQLite coordination types
// -------------------------

// InsertContext provides metadata about a message being inserted into SQLite.
type InsertContext struct {
	Payload     interface{}
	MessageID   string // UUID from the originating message
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

// SQLiteStatement is a single statement to execute for an incoming message.
type SQLiteStatement struct {
	Query string
	Args  []any
}

// SQLiteMultiInserter is an optional interface for SchemaProviders that need to run more than
// one statement per message, in a defined order and atomically. The sqlite service executes the
// returned statements inside a single transaction, in slice order, and rolls back if any fails.
//
// When a provider implements this, SQLiteInsert is not called for incoming messages. Providers
// must still implement it to satisfy SchemaProvider; returning ("", nil) is fine.
//
// The ordering guarantee matters for providers that maintain a "current value" table alongside a
// change log: the log insert must observe the current-value row before the upsert overwrites it.
type SQLiteMultiInserter interface {
	SQLiteInserts(ctx *InsertContext) []SQLiteStatement
}

// SQLiteAfterInserter is an optional interface for SchemaProviders that need to act on a
// message's rows once they are written, e.g. to read back an autoincrement id. The sqlite
// service calls SQLiteAfterInsert only after all of the provider's statements have succeeded.
// The write is already committed, so the hook cannot fail it and must handle its own errors.
type SQLiteAfterInserter interface {
	SQLiteAfterInsert(ctx *InsertContext, db *sql.DB)
}

// SQLiteMigrator is an optional interface for services that need to run
// imperative data migrations after their schema DDL has been applied.
// The SQLite service calls SQLiteMigrate immediately after executing a
// provider's SQLiteSchema(), while the DB is guaranteed to be open.
type SQLiteMigrator interface {
	SQLiteMigrate(db *sql.DB, logger Logger) error
}

// SQLiteLogProvider is an optional interface for SchemaProviders. When
// implemented, the sqlite service calls SQLiteLogFields and appends the
// returned slog key-value pairs to each insert's structured log entry.
// ctx and args are the same values passed to SQLiteInsert.
type SQLiteLogProvider interface {
	SQLiteLogFields(ctx *InsertContext, args []any) []any
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

// BulkIndexSink receives an IndexProvider's documents during a bulk index.
type BulkIndexSink interface {
	// Add indexes one document.
	Add(doc SearchableDocument)
	// Skip records a source record that could not be turned into a document,
	// e.g. a row that failed to scan; the bulk index carries on. sourceID may
	// be empty when the record's id could not be read either. Records a
	// provider deliberately keeps out of search are not skips.
	Skip(sourceID string, err error)
}

// IndexProvider is implemented by services that participate in full-text search.
type IndexProvider interface {
	SearchSourceType() string
	// BulkIndex passes every document to sink and returns once it has. It
	// returns an error when it cannot finish, e.g. the database cannot be
	// opened or reading stops early (rows.Err); documents already added are
	// kept.
	BulkIndex(ctx context.Context, sink BulkIndexSink) error
}

// SearchCoordinator is implemented by the search service.
// core/runtime uses this to register index providers without importing services/search.
type SearchCoordinator interface {
	RegisterIndexProvider(p IndexProvider)
}

// -------------------------
// MCP coordination types
// -------------------------

// MCPToolInputSchema describes the input parameters for an MCP tool (JSON Schema).
type MCPToolInputSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Required   []string               `json:"required,omitempty"`
}

// MCPTool describes a single tool exposed to the LLM via the Model Context Protocol.
type MCPTool struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	InputSchema MCPToolInputSchema `json:"inputSchema"`

	// SummaryArgs names the arguments a UI should show alongside the tool name when a call is
	// collapsed — the ones that say which call this was ("which article", "which query"), so a
	// transcript can be read without expanding every call. The service that defines the tool
	// chooses them, since only it knows which of its arguments identify a call and which can be
	// arbitrarily large: an argument carrying a whole document is worse than nothing on a summary
	// row. Empty (the default) means the call displays as its name alone.
	//
	// Excluded from JSON: this is a hint for the UI, not part of the MCP tool schema, and
	// tools/list responses are spent out of the model's context window.
	SummaryArgs []string `json:"-"`
}

// MCPToolProvider is implemented by services that expose tools to the LLM.
type MCPToolProvider interface {
	MCPTools() []MCPTool
	HandleMCPToolCall(ctx context.Context, toolName string, args json.RawMessage) (string, error)
}

// MCPCoordinator is implemented by the llm service.
// core/runtime uses this to register tool providers without importing services/llm.
type MCPCoordinator interface {
	RegisterMCPToolProvider(p MCPToolProvider)
}
