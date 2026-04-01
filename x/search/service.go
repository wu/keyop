package search

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"keyop/core"

	"github.com/blevesearch/bleve/v2"
)

// Service implements the search indexing and querying service.
type Service struct {
	Deps      core.Dependencies
	Cfg       core.ServiceConfig
	index     bleve.Index
	indexPath string
	providers []IndexProvider
	mu        sync.RWMutex
}

// NewService creates a new search service.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) *Service {
	indexPath := "~/.keyop/search/index"
	if custom, ok := cfg.Config["index_path"].(string); ok && custom != "" {
		indexPath = custom
	}

	return &Service{
		Deps:      deps,
		Cfg:       cfg,
		indexPath: indexPath,
		providers: []IndexProvider{},
	}
}

// Check returns nil if the index is open and accessible.
func (svc *Service) Check() error {
	if svc.index == nil {
		return fmt.Errorf("search: index not initialized")
	}
	return nil
}

// ValidateConfig returns nil.
func (svc *Service) ValidateConfig() []error {
	return nil
}

// Initialize opens or creates the search index and sets up subscriptions.
func (svc *Service) Initialize() error {
	logger := svc.Deps.MustGetLogger()
	ctx := svc.Deps.MustGetContext()
	osProvider := svc.Deps.MustGetOsProvider()

	// Expand ~ in indexPath
	homeDir, err := osProvider.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	if len(svc.indexPath) > 0 && svc.indexPath[0] == '~' {
		svc.indexPath = filepath.Join(homeDir, svc.indexPath[1:])
	}

	// Open or create index
	idx, err := openOrCreateIndexWithRecovery(svc.indexPath)
	if err != nil {
		return fmt.Errorf("failed to open search index: %w", err)
	}
	svc.index = idx
	logger.Info("search: index opened", "path", svc.indexPath)

	// Subscribe to search.index channel
	messenger := svc.Deps.MustGetMessenger()
	err = messenger.Subscribe(
		ctx,
		svc.Cfg.Name,
		"search.index",
		svc.Cfg.Type,
		svc.Cfg.Name,
		time.Hour,
		svc.handleIndexEvent,
	)
	if err != nil {
		return fmt.Errorf("failed to subscribe to search.index: %w", err)
	}

	// Reindex on startup if configured, or if index is empty but providers exist
	reindex, _ := svc.Cfg.Config["reindex_on_start"].(bool)

	// Check if index is empty
	docCount, _ := svc.index.DocCount()
	if docCount == 0 && len(svc.providers) > 0 {
		logger.Info("search: index is empty but providers exist, triggering reindex on startup")
		reindex = true
	}

	if reindex {
		go func() {
			if err := svc.runBulkIndex(); err != nil {
				logger.Error("bulk index failed", "error", err)
			}
		}()
	}

	return nil
}

// RegisterIndexProvider registers a service as an index provider.
func (svc *Service) RegisterIndexProvider(p IndexProvider) {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	svc.providers = append(svc.providers, p)
}

// handleIndexEvent processes incoming index events from the messenger.
func (svc *Service) handleIndexEvent(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()

	// Type-assert the payload
	var evt SearchIndexEvent
	switch v := msg.Data.(type) {
	case SearchIndexEvent:
		evt = v
	case *SearchIndexEvent:
		evt = *v
	default:
		return fmt.Errorf("invalid message data type")
	}

	switch evt.Op {
	case "upsert":
		if err := upsertDocument(svc.index, evt.Document); err != nil {
			logger.Error("failed to upsert document", "id", evt.Document.ID, "error", err)
			return err
		}
		logger.Info("indexed document", "id", evt.Document.ID)

	case "delete":
		if err := deleteDocument(svc.index, evt.ID); err != nil {
			logger.Error("failed to delete document", "id", evt.ID, "error", err)
			return err
		}
		logger.Info("deleted document", "id", evt.ID)

	default:
		return fmt.Errorf("unknown index operation: %s", evt.Op)
	}

	return nil
}

// runBulkIndex calls BulkIndex on all registered providers and indexes the results.
func (svc *Service) runBulkIndex() error {
	logger := svc.Deps.MustGetLogger()

	svc.mu.RLock()
	providers := make([]IndexProvider, len(svc.providers))
	copy(providers, svc.providers)
	svc.mu.RUnlock()

	logger.Info("bulk index starting", "providers_count", len(providers))

	totalIndexed := 0
	for _, provider := range providers {
		sourceType := provider.SearchSourceType()
		logger.Info("bulk indexing from provider", "source_type", sourceType)

		docsChan, err := provider.BulkIndex()
		if err != nil {
			logger.Error("bulk index failed for provider", "source_type", sourceType, "error", err)
			continue
		}

		providerCount := 0
		for doc := range docsChan {
			if err := upsertDocument(svc.index, doc); err != nil {
				logger.Error("failed to index document", "id", doc.ID, "error", err)
				continue
			}
			providerCount++
			totalIndexed++
		}

		logger.Info("completed indexing from provider", "source_type", sourceType, "documents_indexed", providerCount)
	}

	logger.Info("bulk index complete", "total_documents_indexed", totalIndexed)
	return nil
}

// RegisterRoutes registers HTTP routes on the webui mux.
func (svc *Service) RegisterRoutes(mux *http.ServeMux) {
	logger := svc.Deps.MustGetLogger()

	mux.HandleFunc("GET /api/search", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid query", http.StatusBadRequest)
			return
		}

		q := r.Form.Get("q")
		sourceTypes := r.Form["type"]
		tags := r.Form["tag"]
		from := 0
		if formFrom := r.Form.Get("from"); formFrom != "" {
			_, _ = fmt.Sscanf(formFrom, "%d", &from)
		}
		size := 20
		if formSize := r.Form.Get("size"); formSize != "" {
			_, _ = fmt.Sscanf(formSize, "%d", &size)
		}

		results, total, err := queryIndex(svc.index, q, sourceTypes, tags, from, size)
		if err != nil {
			logger.Error("search query failed", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": results,
			"total":   total,
		})
	})

	mux.HandleFunc("POST /api/search/reindex", func(w http.ResponseWriter, _ *http.Request) {
		go func() {
			if err := svc.runBulkIndex(); err != nil {
				logger.Error("reindex failed", "error", err)
			}
		}()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"status":"reindex started"}`)
	})

	mux.HandleFunc("GET /api/search/sources", func(w http.ResponseWriter, _ *http.Request) {
		svc.mu.RLock()
		sourceTypes := make([]string, len(svc.providers))
		for i, p := range svc.providers {
			sourceTypes[i] = p.SearchSourceType()
		}
		svc.mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sources": sourceTypes})
	})
}
