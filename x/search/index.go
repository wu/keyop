package search

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
)

// SearchResult is a single result from a search query.
// nolint:revive
type SearchResult struct {
	ID         string   `json:"id"`
	SourceType string   `json:"sourceType"`
	SourceID   string   `json:"sourceID"`
	Title      string   `json:"title"`
	Snippet    string   `json:"snippet"` // highlighted excerpt
	Tags       []string `json:"tags"`
	URL        string   `json:"url"`
	UpdatedAt  string   `json:"updatedAt"` // RFC3339
	Score      float64  `json:"score"`
}

// IndexDocument is the structure actually stored in Bleve
type IndexDocument struct {
	ID         string   `json:"id"`
	SourceType string   `json:"sourceType"`
	SourceID   string   `json:"sourceID"`
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	Tags       []string `json:"tags"`
	URL        string   `json:"url"`
	UpdatedAt  string   `json:"updatedAt"` // RFC3339
	Extra      string   `json:"extra"`     // JSON-encoded
}

// buildIndexMapping creates a Bleve mapping for documents.
func buildIndexMapping() mapping.IndexMapping {
	idxMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()

	// Disable dynamic mapping so only explicitly mapped fields are used
	docMapping.Dynamic = false

	// Helper to create stored field mapping
	storeField := func(fm *mapping.FieldMapping) *mapping.FieldMapping {
		fm.Store = true
		return fm
	}

	docMapping.AddFieldMappingsAt("id", storeField(bleve.NewTextFieldMapping()))
	docMapping.AddFieldMappingsAt("sourceType", storeField(bleve.NewTextFieldMapping()))
	docMapping.AddFieldMappingsAt("sourceID", storeField(bleve.NewTextFieldMapping()))
	docMapping.AddFieldMappingsAt("title", storeField(bleve.NewTextFieldMapping()))
	docMapping.AddFieldMappingsAt("body", storeField(bleve.NewTextFieldMapping()))

	tagsMapping := bleve.NewTextFieldMapping()
	tagsMapping.Store = true
	docMapping.AddFieldMappingsAt("tags", tagsMapping)

	docMapping.AddFieldMappingsAt("url", storeField(bleve.NewKeywordFieldMapping()))
	docMapping.AddFieldMappingsAt("updatedAt", storeField(bleve.NewDateTimeFieldMapping()))
	docMapping.AddFieldMappingsAt("extra", storeField(bleve.NewTextFieldMapping()))

	idxMapping.AddDocumentMapping("_default", docMapping)
	return idxMapping
}

// openOrCreateIndex opens an existing Bleve index or creates a new one.
// If the index exists but may be outdated, it will be recreated.
func openOrCreateIndexWithRecovery(path string) (bleve.Index, error) {
	// First try to open existing index
	idx, err := bleve.Open(path)
	if err == nil {
		// Verify the index has the correct schema by checking if fields are stored
		// Try a test query to see if we get stored fields
		req := bleve.NewSearchRequest(bleve.NewMatchAllQuery())
		req.Fields = []string{"title"}
		req.Size = 1

		searchResults, err := idx.Search(req)
		if err == nil && len(searchResults.Hits) > 0 {
			// Check if the first hit has fields
			hit := searchResults.Hits[0]
			if hit.Fields != nil && len(hit.Fields) > 0 {
				// Check specifically for title field
				if _, hasTitle := hit.Fields["title"]; hasTitle {
					// Index looks good, fields are being stored
					return idx, nil
				}
			}
		}

		// Index exists but doesn't have stored fields, need to recreate
		_ = idx.Close()
	}

	// Index doesn't exist or is outdated; create a new one
	// Remove old index if it exists
	_ = removeIndexDir(path)

	mapping := buildIndexMapping()
	idx, err = bleve.New(path, mapping)
	if err != nil {
		return nil, fmt.Errorf("failed to create index: %w", err)
	}
	return idx, nil
}

// removeIndexDir safely removes the index directory.
func removeIndexDir(path string) error {
	return os.RemoveAll(path)
}

// openOrCreateIndex is a simple wrapper for tests.
// It creates a fresh index without recovery logic.
func openOrCreateIndex(path string) (bleve.Index, error) {
	// For tests, just create a fresh index
	_ = removeIndexDir(path)
	mapping := buildIndexMapping()
	idx, err := bleve.New(path, mapping)
	if err != nil {
		return nil, fmt.Errorf("failed to create index: %w", err)
	}
	return idx, nil
}

// upsertDocument adds or updates a document in the index.
func upsertDocument(idx bleve.Index, doc SearchableDocument) error {
	// Convert to IndexDocument
	idxDoc := IndexDocument{
		ID:         doc.ID,
		SourceType: doc.SourceType,
		SourceID:   doc.SourceID,
		Title:      doc.Title,
		Body:       doc.Body,
		Tags:       doc.Tags,
		URL:        doc.URL,
		UpdatedAt:  doc.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if doc.Extra != nil {
		extraJSON, _ := json.Marshal(doc.Extra)
		idxDoc.Extra = string(extraJSON)
	}

	return idx.Index(doc.ID, idxDoc)
}

// deleteDocument removes a document from the index.
func deleteDocument(idx bleve.Index, id string) error {
	return idx.Delete(id)
}

// queryIndex searches the index with optional filters.
func queryIndex(idx bleve.Index, q string, sourceTypes []string, tags []string, from, size int) ([]SearchResult, uint64, error) {
	queries := []query.Query{}

	// Main query across title, body, and tags
	if q != "" {
		titleQuery := bleve.NewMatchQuery(q)
		titleQuery.SetField("title")
		bodyQuery := bleve.NewMatchQuery(q)
		bodyQuery.SetField("body")
		tagsQuery := bleve.NewMatchQuery(q)
		tagsQuery.SetField("tags")

		// Create a disjunction (OR) of title, body, and tags queries
		disjunction := bleve.NewDisjunctionQuery(titleQuery, bodyQuery, tagsQuery)
		queries = append(queries, disjunction)
	}

	// Filter by source types
	if len(sourceTypes) > 0 {
		typeQueries := make([]query.Query, len(sourceTypes))
		for i, t := range sourceTypes {
			typeQuery := bleve.NewMatchQuery(t)
			typeQuery.SetField("sourceType")
			typeQueries[i] = typeQuery
		}
		queries = append(queries, bleve.NewDisjunctionQuery(typeQueries...))
	}

	// Filter by tags
	if len(tags) > 0 {
		for _, tag := range tags {
			tagQuery := bleve.NewMatchQuery(tag)
			tagQuery.SetField("tags")
			queries = append(queries, tagQuery)
		}
	}

	// Build final query
	var finalQuery query.Query
	if len(queries) == 0 {
		finalQuery = bleve.NewMatchAllQuery()
	} else if len(queries) == 1 {
		finalQuery = queries[0]
	} else {
		finalQuery = bleve.NewConjunctionQuery(queries...)
	}

	// Execute search
	req := bleve.NewSearchRequestOptions(finalQuery, size, from, false)
	req.Highlight = bleve.NewHighlight()
	req.Highlight.AddField("body")
	req.Fields = []string{"id", "sourceType", "sourceID", "title", "body", "tags", "url", "updatedAt", "extra"}

	searchResults, err := idx.Search(req)
	if err != nil {
		return nil, 0, fmt.Errorf("search failed: %w", err)
	}

	// Convert Bleve results to SearchResult
	results := make([]SearchResult, 0, len(searchResults.Hits))
	for _, hit := range searchResults.Hits {
		sourceType := getStringField(hit.Fields, "sourceType")
		sourceID := getStringField(hit.Fields, "sourceID")
		title := getStringField(hit.Fields, "title")
		body := getStringField(hit.Fields, "body")
		url := getStringField(hit.Fields, "url")
		updatedAt := getStringField(hit.Fields, "updatedAt")
		tags := getStringSliceField(hit.Fields, "tags")

		// Skip documents with empty sourceType (shouldn't happen, but be safe)
		if sourceType == "" {
			continue
		}

		// Generate snippet with context
		snippet := generateSnippet(body, hit.Fragments)
		if len(snippet) > 400 {
			snippet = snippet[:400] + "…"
		}

		result := SearchResult{
			ID:         hit.ID,
			SourceType: sourceType,
			SourceID:   sourceID,
			Title:      title,
			Snippet:    snippet,
			Tags:       tags,
			URL:        url,
			UpdatedAt:  updatedAt,
			Score:      hit.Score,
		}
		results = append(results, result)
	}

	return results, searchResults.Total, nil
}

// generateSnippet creates a snippet with context around the matched text
func generateSnippet(body string, fragments map[string][]string) string {
	// If we have fragments from Bleve's highlighting, use them
	if len(fragments) > 0 && len(fragments["body"]) > 0 {
		snippet := strings.Join(fragments["body"], " … ")
		return snippet
	}

	// Fallback: return the beginning of the body with ellipsis if too long
	if len(body) > 0 {
		if len(body) > 300 {
			return body[:300] + "…"
		}
		return body
	}

	return ""
}

// Helper functions to extract fields safely
func getStringField(fields map[string]interface{}, key string) string {
	if v, ok := fields[key]; ok {
		// Try direct string first
		if s, ok := v.(string); ok {
			return s
		}
		// Try slice of interface{}
		if sl, ok := v.([]interface{}); ok && len(sl) > 0 {
			if s, ok := sl[0].(string); ok {
				return s
			}
		}
	}
	return ""
}

func getStringSliceField(fields map[string]interface{}, key string) []string {
	if v, ok := fields[key]; ok {
		// Try slice of interface{} first
		if sl, ok := v.([]interface{}); ok {
			result := make([]string, 0, len(sl))
			for _, item := range sl {
				if s, ok := item.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
		// Try slice of strings directly
		if sl, ok := v.([]string); ok {
			return sl
		}
	}
	return nil
}
