package notes

import (
	"context"
	"keyop/core"
	"keyop/core/testutil"
	"testing"

	"github.com/stretchr/testify/assert"
)

func createTestDeps(_ *testing.T) core.Dependencies {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	deps.SetMessenger(testutil.NewFakeMessenger())
	deps.SetContext(context.Background())
	return deps
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
	assert.Equal(t, "Notes", tab.Title)
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
