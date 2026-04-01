package search

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"

	"keyop/x/webui"

	"github.com/blevesearch/bleve/v2"
)

//go:embed resources
var embeddedAssets embed.FS

// WebUITab implements webui.TabProvider.
func (svc *Service) WebUITab() webui.TabInfo {
	// Inline CSS to ensure it's always loaded
	cssContent := `#search-container {
    padding: 16px;
    height: 100%;
    overflow: hidden;
    background-color: var(--bg, #1e1e1e);
    color: var(--text, #fff);
    display: flex;
    flex-direction: column;
}

#search-header {
    flex-shrink: 0;
}

#search-toolbar {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 16px;
    padding: 8px;
    background-color: var(--item-bg, #2d2d2d);
    border: 1px solid var(--border, #444);
    border-radius: 4px;
    overflow: visible;
}

#search-toolbar button {
    padding: 6px 12px;
    background-color: var(--accent, #007acc);
    color: white;
    border: none;
    border-radius: 3px;
    cursor: pointer;
    font-size: 12px;
    transition: background-color 0.2s;
}

#search-toolbar button:hover {
    background-color: var(--accent-dim, rgba(0, 122, 204, 0.8));
}

#search-stats {
    margin-left: auto;
    font-size: 12px;
    color: var(--text, #fff);
}

#search-input-group {
    margin-bottom: 16px;
    position: relative;
    display: flex;
}

#search-query {
    width: 100%;
    padding: 8px;
    padding-right: 32px;
    border: 1px solid var(--border, #444);
    background-color: var(--item-bg, #2d2d2d);
    color: var(--text, #fff);
    border-radius: 4px;
    font-size: 14px;
    box-sizing: border-box;
}

#search-query:focus {
    outline: none;
    border-color: var(--accent, #007acc);
}

#search-clear-btn {
    position: absolute;
    right: 8px;
    top: 50%;
    transform: translateY(-50%);
    background: none;
    border: none;
    color: var(--text-muted, #999);
    cursor: pointer;
    font-size: 18px;
    padding: 4px;
    display: none;
    transition: color 0.2s;
}

#search-clear-btn:hover {
    color: var(--text, #fff);
}

#search-clear-btn.visible {
    display: block;
}

#search-main {
    display: flex;
    gap: 16px;
    flex: 1;
    overflow: hidden;
}

#search-sidebar {
    width: 200px;
    flex-shrink: 0;
    border-right: 1px solid var(--border, #444);
    padding-right: 16px;
    overflow-y: auto;
}

#search-sidebar-title {
    font-size: 12px;
    font-weight: bold;
    color: var(--text, #fff);
    margin-bottom: 8px;
    text-transform: uppercase;
}

#search-sources {
    display: flex;
    flex-direction: column;
    gap: 4px;
}

.source-filter {
    padding: 6px 8px;
    background-color: var(--item-bg, #2d2d2d);
    border: 1px solid var(--border, #444);
    border-radius: 3px;
    cursor: pointer;
    font-size: 12px;
    color: var(--text, #fff);
    transition: all 0.2s;
    display: flex;
    justify-content: space-between;
    align-items: center;
}

.source-filter:hover {
    background-color: var(--hover-bg, #3d3d3d);
}

.source-filter.active {
    background-color: var(--accent, #007acc);
    border-color: var(--accent, #007acc);
}

.source-count {
    font-size: 11px;
    color: var(--text-muted, #aaa);
}

.source-filter.active .source-count {
    color: white;
}

#search-content {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

#search-results {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 8px;
    overflow-y: auto;
}

.search-result {
    padding: 8px 10px;
    background-color: var(--item-bg, #2d2d2d);
    border: 1px solid var(--border, #444);
    border-radius: 4px;
    cursor: pointer;
    transition: background-color 0.15s;
}

.search-result:hover {
    background-color: var(--hover-bg, #3d3d3d);
}

.search-result-header {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 4px;
}

.search-result-title {
    font-weight: bold;
    font-size: 14px;
    color: var(--text, #fff);
    line-height: 1.2;
    flex: 1;
}

.search-result-title a {
    color: var(--accent, #007acc);
    text-decoration: none;
}

.search-result-title a:hover {
    text-decoration: underline;
}

.search-result-meta {
    display: flex;
    gap: 8px;
    margin-bottom: 4px;
    flex-wrap: wrap;
    align-items: center;
}

.source-badge {
    display: inline-block;
    padding: 3px 8px;
    background-color: var(--accent-dim, rgba(140, 82, 218, 0.2));
    color: var(--accent, #8a52da);
    border-radius: 3px;
    font-size: 10px;
    font-weight: bold;
    white-space: nowrap;
    margin-left: auto;
}

.result-date {
    font-size: 11px;
    color: var(--text-muted, #999);
}

.result-tags {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
    margin-bottom: 4px;
}

.result-tag {
    display: inline-block;
    padding: 2px 6px;
    background-color: #382068;
    color: #8a52da;
    border-radius: 3px;
    font-size: 10px;
    font-weight: 600;
    white-space: nowrap;
}

.search-result-snippet {
    font-size: 13px;
    color: var(--text, #fff);
    line-height: 1.4;
    margin-top: 4px;
    word-wrap: break-word;
    overflow-wrap: break-word;
}

.search-result-snippet strong {
    color: var(--text, #fff);
    font-weight: bold;
}

.search-result-snippet em {
    font-style: italic;
}

.search-result-snippet code {
    background-color: var(--border, #444);
    padding: 2px 4px;
    border-radius: 2px;
    font-family: 'Courier New', monospace;
    font-size: 12px;
    color: var(--text, #fff);
}

.search-result-snippet a {
    color: var(--accent, #007acc);
    text-decoration: none;
}

.search-result-snippet a:hover {
    text-decoration: underline;
}

.search-result-snippet br {
    display: block;
    margin: 4px 0;
}

#search-empty {
    text-align: center;
    padding: 32px 16px;
    color: var(--text, #fff);
}

.search-results-summary {
    padding: 8px 0;
    color: var(--text, #fff);
    font-size: 12px;
    margin-bottom: 12px;
    border-bottom: 1px solid var(--border, #444);
}

.search-pagination {
    padding: 12px 0;
    border-top: 1px solid var(--border, #444);
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 12px;
}

.pagination-info {
    color: var(--text-muted, #aaa);
}

.pagination-buttons {
    display: flex;
    gap: 8px;
}

.pagination-btn {
    padding: 6px 12px;
    background-color: var(--item-bg, #333);
    color: var(--text, #fff);
    border: 1px solid var(--border, #444);
    border-radius: 4px;
    cursor: pointer;
    font-size: 12px;
    transition: background 0.2s;
}

.pagination-btn:hover {
    background-color: var(--hover-bg, #555);
}

#search-help-toggle {
    padding: 6px 12px !important;
    background-color: transparent !important;
    color: var(--accent, #007acc) !important;
    border: 1px solid var(--border, #444) !important;
    cursor: pointer;
}

#search-help-toggle:hover {
    background-color: var(--item-bg, #2d2d2d) !important;
}

#search-help {
    background-color: var(--item-bg, #2d2d2d);
    border: 1px solid var(--border, #444);
    border-radius: 4px;
    padding: 12px;
    margin-bottom: 12px;
    font-size: 12px;
    line-height: 1.5;
}

#search-help h3 {
    margin: 0 0 8px 0;
    font-size: 13px;
    color: var(--accent, #007acc);
}

#search-help ul {
    margin: 0;
    padding-left: 20px;
}

#search-help li {
    margin: 4px 0;
    color: var(--text, #fff);
}

#search-help code {
    background-color: var(--bg, #1e1e1e);
    padding: 2px 4px;
    border-radius: 2px;
    font-family: monospace;
    color: var(--accent, #007acc);
}

#search-stats {
    margin-left: auto;
    font-size: 12px;
    color: var(--text, #fff);
}`

	content := `<div id="search-container">
		<div id="search-header">
			<div id="search-toolbar">
				<button id="reindex-btn">🔄 Reindex</button>
				<button id="search-help-toggle">? Help</button>
				<div id="search-stats" class="loading">Loading...</div>
			</div>
			<div id="search-help" style="display: none;">
				<h3>Search Syntax</h3>
				<ul>
					<li><code>term1 term2</code> - Find documents with both terms (AND)</li>
					<li><code>term1 OR term2</code> - Find documents with either term</li>
					<li><code>"exact phrase"</code> - Search for exact phrases</li>
					<li><code>term1 -term2</code> - Include term1 but exclude term2</li>
					<li><code>+term1 term2</code> - term1 is required, term2 is optional</li>
					<li><code>term*</code> - Wildcard search (e.g., <code>test*</code> matches test, testing, tested)</li>
				</ul>
			</div>
			<div id="search-input-group">
				<input type="text" id="search-query" placeholder="Search..." />
				<button id="search-clear-btn">✕</button>
			</div>
		</div>
		<div id="search-main">
			<div id="search-sidebar">
				<div id="search-sidebar-title">Sources</div>
				<div id="search-sources"></div>
			</div>
			<div id="search-content">
				<div id="search-results"></div>
				<div id="search-empty" style="display: none;">No results found</div>
			</div>
		</div>
	</div>
	<style>` + cssContent + `</style>`

	return webui.TabInfo{
		ID:      "search",
		Title:   "🔍",
		Content: content,
		JSPath:  "/api/assets/search/search-tab.js",
	}
}

// WebUIAssets implements webui.AssetProvider.
func (svc *Service) WebUIAssets() http.FileSystem {
	sub, _ := fs.Sub(embeddedAssets, "resources")
	return http.FS(sub)
}

// HandleWebUIAction implements webui.ActionProvider.
func (svc *Service) HandleWebUIAction(action string, params map[string]any) (any, error) {
	switch action {
	case "search":
		q, _ := params["q"].(string)
		var types []string
		if t, ok := params["types"].([]interface{}); ok {
			for _, v := range t {
				if s, ok := v.(string); ok {
					types = append(types, s)
				}
			}
		}
		var tags []string
		if t, ok := params["tags"].([]interface{}); ok {
			for _, v := range t {
				if s, ok := v.(string); ok {
					tags = append(tags, s)
				}
			}
		}
		from := 0
		if f, ok := params["from"].(float64); ok {
			from = int(f)
		}
		size := 20
		if s, ok := params["size"].(float64); ok {
			size = int(s)
		}

		results, total, err := queryIndex(svc.index, q, types, tags, from, size)
		if err != nil {
			return nil, err
		}

		return map[string]any{
			"results": results,
			"total":   total,
		}, nil

	case "reindex":
		go func() {
			_ = svc.runBulkIndex()
		}()
		return map[string]any{"status": "reindex started"}, nil

	case "get-sources":
		svc.mu.RLock()
		sourceTypes := make([]string, len(svc.providers))
		for i, p := range svc.providers {
			sourceTypes[i] = p.SearchSourceType()
		}
		svc.mu.RUnlock()
		return map[string]any{"sources": sourceTypes}, nil

	case "get-index-status":
		docCount, err := svc.index.DocCount()
		if err != nil {
			return map[string]any{"docCount": 0, "error": err.Error()}, nil
		}
		svc.mu.RLock()
		providerCount := len(svc.providers)
		svc.mu.RUnlock()

		// Get source type counts
		sourceCounts := make(map[string]uint64)
		if docCount > 0 {
			// Query all documents with match-all query to get source type distribution
			results, _, err := queryIndex(svc.index, "", nil, nil, 0, int(docCount))
			if err == nil {
				for _, r := range results {
					sourceCounts[r.SourceType]++
				}
			}
		}

		return map[string]any{
			"docCount":       docCount,
			"providersCount": providerCount,
			"sourceCounts":   sourceCounts,
		}, nil

	case "debug-providers":
		// Show registered providers
		svc.mu.RLock()
		providers := make([]string, len(svc.providers))
		for i, p := range svc.providers {
			providers[i] = p.SearchSourceType()
		}
		svc.mu.RUnlock()
		return map[string]any{
			"providers": providers,
		}, nil

	case "debug-list-all":
		// Debug action to list all indexed documents
		results, total, err := queryIndex(svc.index, "", nil, nil, 0, 10000)
		if err != nil {
			return nil, err
		}
		type DocInfo struct {
			ID         string   `json:"id"`
			SourceType string   `json:"sourceType"`
			Title      string   `json:"title"`
			Snippet    string   `json:"snippet"`
			Tags       []string `json:"tags"`
		}
		docs := make([]DocInfo, len(results))
		for i, r := range results {
			snippet := r.Snippet
			if len(snippet) > 100 {
				snippet = snippet[:100]
			}
			docs[i] = DocInfo{
				ID:         r.ID,
				SourceType: r.SourceType,
				Title:      r.Title,
				Snippet:    snippet,
				Tags:       r.Tags,
			}
		}
		return map[string]any{
			"total": total,
			"docs":  docs,
		}, nil

	case "debug-query-index":
		// Debug queryIndex directly
		q, _ := params["q"].(string)
		results, total, err := queryIndex(svc.index, q, []string{}, []string{}, 0, 100)
		if err != nil {
			return nil, err
		}

		type DebugResult struct {
			ID         string `json:"id"`
			SourceType string `json:"sourceType"`
			Title      string `json:"title"`
		}

		debugResults := make([]DebugResult, len(results))
		for i, r := range results {
			debugResults[i] = DebugResult{
				ID:         r.ID,
				SourceType: r.SourceType,
				Title:      r.Title,
			}
		}

		return map[string]any{
			"total":   total,
			"count":   len(results),
			"results": debugResults,
		}, nil

	case "debug-bleve-query":
		// Test Bleve query directly
		q := params["q"]
		if q == nil {
			return nil, fmt.Errorf("missing query parameter 'q'")
		}
		queryStr := fmt.Sprintf("%v", q)

		// Run the Bleve query directly
		bleveQuery := bleve.NewMatchQuery(queryStr)
		searchRequest := bleve.NewSearchRequest(bleveQuery)
		searchRequest.Size = 100
		searchRequest.Fields = []string{"id", "sourceType", "sourceID", "title", "body", "tags", "url", "updatedAt"}

		results, err := svc.index.Search(searchRequest)
		if err != nil {
			return nil, fmt.Errorf("bleve search failed: %w", err)
		}

		type BleveResult struct {
			ID     string                 `json:"id"`
			Score  float64                `json:"score"`
			Fields map[string]interface{} `json:"fields"`
		}

		bleveResults := make([]BleveResult, len(results.Hits))
		for i, hit := range results.Hits {
			bleveResults[i] = BleveResult{
				ID:     hit.ID,
				Score:  hit.Score,
				Fields: hit.Fields,
			}
		}

		return map[string]any{
			"total":   results.Total,
			"hits":    len(results.Hits),
			"results": bleveResults,
		}, nil

	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
