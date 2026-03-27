package notes

import (
	"context"
	"keyop/core"
	"keyop/core/testutil"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestDeps(_ *testing.T) core.Dependencies {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	deps.SetMessenger(testutil.NewFakeMessenger())
	deps.SetContext(context.Background())
	return deps
}

// newTestSvcWithDB returns a Service backed by a real temp DB with schema
// already initialised.  Use this for tests that exercise CRUD operations.
func newTestSvcWithDB(t *testing.T) *Service {
	t.Helper()
	dbPath := openTestNotesDB(t)
	deps := createTestDeps(t)
	return &Service{
		Cfg:    core.ServiceConfig{Name: "notes"},
		Deps:   deps,
		dbPath: dbPath,
	}
}

// act calls HandleWebUIAction and fails the test on error.
func act(t *testing.T, svc *Service, action string, params map[string]any) any {
	t.Helper()
	res, err := svc.HandleWebUIAction(action, params)
	require.NoError(t, err)
	return res
}

func TestNewService(t *testing.T) {
	deps := createTestDeps(t)
	cfg := core.ServiceConfig{
		Name: "test-notes",
	}

	svc := NewService(deps, cfg)

	assert.NotNil(t, svc)
	assert.Equal(t, "test-notes", svc.Cfg.Name)
}

func TestCheck(t *testing.T) {
	deps := createTestDeps(t)
	svc := NewService(deps, core.ServiceConfig{Name: "test-notes"})

	err := svc.Check()
	assert.NoError(t, err)
}

func TestValidateConfig(t *testing.T) {
	deps := createTestDeps(t)
	svc := NewService(deps, core.ServiceConfig{Name: "test-notes"})

	errs := svc.ValidateConfig()
	assert.Empty(t, errs)
}

func TestInitialize(t *testing.T) {
	deps := createTestDeps(t)
	svc := NewService(deps, core.ServiceConfig{Name: "test-notes"})

	err := svc.Initialize()
	assert.NoError(t, err)
}

func TestHandleWebUIActionGetNotes(t *testing.T) {
	deps := createTestDeps(t)
	svc := NewService(deps, core.ServiceConfig{Name: "notes"})

	result, err := svc.HandleWebUIAction("get-notes", map[string]any{
		"search": "",
		"limit":  100,
	})

	// May fail if database not found, but should not panic
	_ = err
	_ = result
}

func TestHandleWebUIActionRenderMarkdown(t *testing.T) {
	deps := createTestDeps(t)
	svc := NewService(deps, core.ServiceConfig{Name: "notes"})

	result, err := svc.HandleWebUIAction("render-markdown", map[string]any{
		"content": "# Test\n\n**Bold** text",
	})

	assert.NoError(t, err)
	assert.NotNil(t, result)

	resultMap, ok := result.(map[string]any)
	assert.True(t, ok)

	html, htmlOk := resultMap["html"].(string)
	assert.True(t, htmlOk)
	assert.Contains(t, html, "<h1>Test</h1>")
	assert.Contains(t, html, "<strong>Bold</strong>")
}

func TestHandleWebUIActionUnknown(t *testing.T) {
	deps := createTestDeps(t)
	svc := NewService(deps, core.ServiceConfig{Name: "notes"})

	_, err := svc.HandleWebUIAction("unknown-action", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown action")
}

func TestRenderMarkdownWithTable(t *testing.T) {
	deps := createTestDeps(t)
	svc := NewService(deps, core.ServiceConfig{Name: "notes"})

	markdown := `| Header 1 | Header 2 |
|----------|----------|
| Cell 1   | Cell 2   |`

	result, err := svc.renderMarkdown(map[string]any{
		"content": markdown,
	})
	assert.NoError(t, err)

	resultMap, ok := result.(map[string]any)
	assert.True(t, ok)

	html, htmlOk := resultMap["html"].(string)
	assert.True(t, htmlOk)
	assert.Contains(t, html, "<table>")
}

func TestRenderMarkdownMissingContent(t *testing.T) {
	deps := createTestDeps(t)
	svc := NewService(deps, core.ServiceConfig{Name: "notes"})

	_, err := svc.renderMarkdown(map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing content parameter")
}

func TestWebUITab(t *testing.T) {
	deps := createTestDeps(t)
	svc := NewService(deps, core.ServiceConfig{Name: "notes"})

	tab := svc.WebUITab()
	assert.Equal(t, "notes", tab.ID)
	assert.Equal(t, "🗒️", tab.Title)
	assert.Equal(t, "📋", tab.Icon)
	assert.NotEmpty(t, tab.Content)
	assert.NotEmpty(t, tab.JSPath)
}

func TestWebUIAssets(t *testing.T) {
	deps := createTestDeps(t)
	svc := NewService(deps, core.ServiceConfig{Name: "notes"})

	fs := svc.WebUIAssets()
	assert.NotNil(t, fs)
}

func TestCreateNoteMissingTitle(t *testing.T) {
	deps := createTestDeps(t)
	svc := NewService(deps, core.ServiceConfig{Name: "notes"})

	_, err := svc.createNote(map[string]any{
		"content": "Some content",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing title parameter")
}

func TestGetNoteMissingID(t *testing.T) {
	deps := createTestDeps(t)
	svc := NewService(deps, core.ServiceConfig{Name: "notes"})

	_, err := svc.getNote(map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing or invalid id parameter")
}

func TestUpdateNoteMissingID(t *testing.T) {
	deps := createTestDeps(t)
	svc := NewService(deps, core.ServiceConfig{Name: "notes"})

	_, err := svc.updateNote(map[string]any{
		"title": "New title",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing or invalid id parameter")
}

func TestDeleteNoteMissingID(t *testing.T) {
	deps := createTestDeps(t)
	svc := NewService(deps, core.ServiceConfig{Name: "notes"})

	_, err := svc.deleteNote(map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing or invalid id parameter")
}

// ---------------------------------------------------------------------------
// CRUD tests via HandleWebUIAction using a real temp database
// ---------------------------------------------------------------------------

func TestHandleWebUIAction_DBGetNotes(t *testing.T) {
	svc := newTestSvcWithDB(t)
	insertTestNote(t, svc.dbPath, "Note A", "body", "")
	insertTestNote(t, svc.dbPath, "Note B", "body", "")

	res := act(t, svc, "get-notes", map[string]any{"limit": float64(10), "offset": float64(0)})
	m := res.(map[string]any)
	assert.Equal(t, 2, m["total"])
	assert.Len(t, m["notes"], 2)
}

func TestHandleWebUIAction_DBGetNote(t *testing.T) {
	svc := newTestSvcWithDB(t)
	id := insertTestNote(t, svc.dbPath, "My Note", "hello", "tag1")

	res := act(t, svc, "get-note", map[string]any{"id": float64(id)})
	m := res.(map[string]any)
	assert.Equal(t, id, m["id"])
	assert.Equal(t, "My Note", m["title"])
	assert.Equal(t, "hello", m["content"])
}

func TestHandleWebUIAction_DBCreateNote(t *testing.T) {
	svc := newTestSvcWithDB(t)

	res := act(t, svc, "create-note", map[string]any{
		"title":   "Created",
		"content": "Content here",
		"tags":    "new",
	})
	m := res.(map[string]any)
	assert.Equal(t, "Created", m["title"])
	assert.NotNil(t, m["id"])

	list := act(t, svc, "get-notes", map[string]any{"limit": float64(10), "offset": float64(0)})
	assert.Equal(t, 1, list.(map[string]any)["total"])
}

func TestHandleWebUIAction_DBUpdateNote(t *testing.T) {
	svc := newTestSvcWithDB(t)
	id := insertTestNote(t, svc.dbPath, "Before", "old", "")

	res := act(t, svc, "update-note", map[string]any{
		"id":      float64(id),
		"title":   "After",
		"content": "new body",
		"tags":    "edited",
	})
	m := res.(map[string]any)
	assert.Equal(t, "After", m["title"])
	assert.Equal(t, "new body", m["content"])
	assert.Equal(t, "edited", m["tags"])
}

func TestHandleWebUIAction_DBDeleteNote(t *testing.T) {
	svc := newTestSvcWithDB(t)
	id := insertTestNote(t, svc.dbPath, "Doomed", "bye", "")

	res := act(t, svc, "delete-note", map[string]any{"id": float64(id)})
	assert.Equal(t, true, res.(map[string]any)["deleted"])

	_, err := svc.HandleWebUIAction("get-note", map[string]any{"id": float64(id)})
	require.Error(t, err)
}

func TestHandleWebUIAction_DBGetTagCounts(t *testing.T) {
	svc := newTestSvcWithDB(t)
	insertTestNote(t, svc.dbPath, "Note X", "", "go,test")
	insertTestNote(t, svc.dbPath, "Note Y", "", "go")
	insertTestNote(t, svc.dbPath, "Note Z", "", "")

	res := act(t, svc, "get-tag-counts", map[string]any{})
	m := res.(map[string]any)
	counts := m["counts"].(map[string]int)
	assert.Equal(t, 3, counts["all"])
	assert.Equal(t, 2, counts["go"])
	assert.Equal(t, 1, counts["untagged"])
}

func TestHandleWebUIAction_DBGetNoteTitles(t *testing.T) {
	svc := newTestSvcWithDB(t)
	insertTestNote(t, svc.dbPath, "Short", "", "")
	insertTestNote(t, svc.dbPath, "A very long title", "", "")

	res := act(t, svc, "get-note-titles", map[string]any{})
	m := res.(map[string]any)
	assert.NotNil(t, m["titles"])
}

func TestHandleWebUIAction_DBImportNotes(t *testing.T) {
	svc := newTestSvcWithDB(t)

	res := act(t, svc, "import-notes", map[string]any{
		"files": []any{
			map[string]any{"name": "first.md", "content": "# First\nbody"},
			map[string]any{"name": "second.md", "content": "# Second\nbody"},
		},
	})
	m := res.(map[string]any)
	assert.Equal(t, 2, m["count"])

	list := act(t, svc, "get-notes", map[string]any{"limit": float64(10), "offset": float64(0)})
	assert.Equal(t, 2, list.(map[string]any)["total"])
}

func TestHandleWebUIAction_ImportNotesMissingFiles(t *testing.T) {
	svc := newTestSvcWithDB(t)
	_, err := svc.HandleWebUIAction("import-notes", map[string]any{})
	require.Error(t, err)
}
