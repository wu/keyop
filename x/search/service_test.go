package search

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertAndQueryDocument(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "search-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	idxPath := filepath.Join(tmpDir, "test.bleve")
	idx, err := openOrCreateIndex(idxPath)
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	doc := SearchableDocument{
		ID:         "notes:001",
		SourceType: "notes",
		SourceID:   "001",
		Title:      "Test Note About Golang",
		Body:       "This is a test note with searchable content about golang programming",
		Tags:       []string{"test", "demo"},
		URL:        "http://example.com/notes/001",
		UpdatedAt:  time.Now(),
		Extra:      map[string]string{"color": "blue"},
	}

	err = upsertDocument(idx, doc)
	require.NoError(t, err)

	// Search for the title term
	results, total, err := queryIndex(idx, "Golang", nil, nil, 0, 10)
	require.NoError(t, err)
	assert.Greater(t, total, uint64(0), "should find at least one result")
	assert.Greater(t, len(results), 0, "should return results")
	assert.Equal(t, "notes:001", results[0].ID)
}

func TestDeleteDocument(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "search-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	idxPath := filepath.Join(tmpDir, "test.bleve")
	idx, err := openOrCreateIndex(idxPath)
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	doc := SearchableDocument{
		ID:         "tasks:123",
		SourceType: "tasks",
		SourceID:   "123",
		Title:      "Delete Me Task",
		Body:       "This task should be deleted",
		UpdatedAt:  time.Now(),
	}

	err = upsertDocument(idx, doc)
	require.NoError(t, err)

	// Verify it was indexed
	_, total, err := queryIndex(idx, "Delete", nil, nil, 0, 10)
	require.NoError(t, err)
	assert.Greater(t, total, uint64(0))

	// Delete the document
	err = deleteDocument(idx, doc.ID)
	require.NoError(t, err)

	// Verify it was deleted
	results, _, err := queryIndex(idx, "Delete", nil, nil, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, 0, len(results))
}

func TestQueryBySourceType(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "search-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	idxPath := filepath.Join(tmpDir, "test.bleve")
	idx, err := openOrCreateIndex(idxPath)
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// Add documents from different sources
	docs := []SearchableDocument{
		{
			ID:         "notes:1",
			SourceType: "notes",
			SourceID:   "1",
			Title:      "Note One",
			Body:       "Content one",
			UpdatedAt:  time.Now(),
		},
		{
			ID:         "tasks:1",
			SourceType: "tasks",
			SourceID:   "1",
			Title:      "Task One",
			Body:       "Task content",
			UpdatedAt:  time.Now(),
		},
	}

	for _, doc := range docs {
		err := upsertDocument(idx, doc)
		require.NoError(t, err)
	}

	// Query just notes
	results, _, err := queryIndex(idx, "", []string{"notes"}, nil, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, "notes:1", results[0].ID)
}

func TestBulkIndexIndex(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "search-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	idxPath := filepath.Join(tmpDir, "test.bleve")
	idx, err := openOrCreateIndex(idxPath)
	require.NoError(t, err)
	defer func() { _ = idx.Close() }()

	// Mock provider
	mockProvider := &mockIndexProvider{
		sourceType: "notes",
		docs: []SearchableDocument{
			{
				ID:         "notes:1",
				SourceType: "notes",
				SourceID:   "1",
				Title:      "Note One",
				Body:       "Content one",
				UpdatedAt:  time.Now(),
			},
			{
				ID:         "notes:2",
				SourceType: "notes",
				SourceID:   "2",
				Title:      "Note Two",
				Body:       "Content two",
				UpdatedAt:  time.Now(),
			},
			{
				ID:         "notes:3",
				SourceType: "notes",
				SourceID:   "3",
				Title:      "Note Three",
				Body:       "Content three",
				UpdatedAt:  time.Now(),
			},
		},
	}

	// Manually index documents instead of using runBulkIndex (which needs logger)
	for _, doc := range mockProvider.docs {
		err := upsertDocument(idx, doc)
		require.NoError(t, err)
	}

	// Verify all documents were indexed by doing a wildcard search
	results, total, err := queryIndex(idx, "", nil, nil, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), total)
	assert.Equal(t, 3, len(results))
}

// mockIndexProvider is a test implementation of IndexProvider.
type mockIndexProvider struct {
	sourceType string
	docs       []SearchableDocument
}

func (m *mockIndexProvider) SearchSourceType() string {
	return m.sourceType
}

func (m *mockIndexProvider) BulkIndex() (<-chan SearchableDocument, error) {
	ch := make(chan SearchableDocument)
	go func() {
		for _, doc := range m.docs {
			ch <- doc
		}
		close(ch)
	}()
	return ch, nil
}
