// Package search provides full-text search indexing and querying across all services.
package search

import "time"

// SearchableDocument is the indexing unit for the search service.
// Each document is uniquely identified by ID = "<sourceType>:<sourceID>".
type SearchableDocument struct {
	ID         string // globally unique: "<sourceType>:<sourceID>"
	SourceType string // "notes", "tasks", "journal", etc.
	SourceID   string // the native ID within the source service
	Title      string
	Body       string
	Tags       []string
	URL        string // optional deep-link back to the item
	UpdatedAt  time.Time
	Extra      map[string]string // service-specific extra fields
}

// SearchIndexEvent is the payload sent on the "search.index" messenger channel.
// nolint:revive
type SearchIndexEvent struct {
	Op       string             // "upsert" | "delete"
	Document SearchableDocument // populated for "upsert"
	ID       string             // populated for "delete"
}

// IndexProvider is implemented by any service that wants to participate in search.
// The search service calls BulkIndex() once at startup to populate the initial index.
type IndexProvider interface {
	// SearchSourceType returns the source type identifier (e.g., "notes", "tasks").
	SearchSourceType() string

	// BulkIndex returns a channel of SearchableDocument that the search service will index.
	BulkIndex() (<-chan SearchableDocument, error)
}
