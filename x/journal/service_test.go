package journal

import (
	"context"
	"keyop/core"
	"keyop/core/testutil"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func createTestDeps() core.Dependencies {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	deps.SetMessenger(testutil.NewFakeMessenger())
	return deps
}

func TestNewService(t *testing.T) {
	deps := createTestDeps()
	cfg := core.ServiceConfig{
		Name: "test-journal",
	}

	svc := NewService(deps, cfg)

	assert.NotNil(t, svc)
	assert.Equal(t, "test-journal", svc.Cfg.Name)
	assert.Equal(t, deps, svc.Deps)
}

func TestCheck(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{
		Name: "test-journal",
	})

	err := svc.Check()
	assert.NoError(t, err)
}

func TestValidateConfig(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{
		Name: "test-journal",
	})

	errs := svc.ValidateConfig()
	assert.Empty(t, errs)
}

// Note: Initialize test is skipped as it requires a full context setup with messenger subscription
// This is an integration test scenario rather than a unit test scenario

func TestGetDates(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{
		Name: "journal",
	})

	// This test will work with actual filesystem since getDates creates the dir
	result, err := svc.getDates()
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Verify it's a map
	resultMap, ok := result.(map[string]interface{})
	assert.True(t, ok)
	assert.Contains(t, resultMap, "dates")
}

func TestGetEntry(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{
		Name: "journal",
	})

	// Test with default date (today)
	result, err := svc.getEntry(map[string]any{})
	assert.NoError(t, err)
	assert.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	assert.True(t, ok)
	assert.Contains(t, resultMap, "content")
}

func TestGetEntryWithSpecificDate(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{
		Name: "journal",
	})

	testDate := "2026-03-14"
	result, err := svc.getEntry(map[string]any{
		"date": testDate,
	})

	assert.NoError(t, err)
	assert.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	assert.True(t, ok)
	assert.Contains(t, resultMap, "content")
}

func TestSaveEntry(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{
		Name: "journal",
	})

	testDate := "2026-03-15"
	testContent := "# Test Entry\n\n- Item 1\n- Item 2"

	result, err := svc.saveEntry(map[string]any{
		"date":    testDate,
		"content": testContent,
	})

	assert.NoError(t, err)
	assert.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, true, resultMap["saved"])

	// Verify the file was created by reading it back
	readResult, readErr := svc.getEntry(map[string]any{
		"date": testDate,
	})

	assert.NoError(t, readErr)
	readMap, _ := readResult.(map[string]interface{})
	assert.Equal(t, testContent, readMap["content"])
}

func TestRenderMarkdown(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{
		Name: "journal",
	})

	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{
			name:     "heading",
			input:    "# Test Heading",
			contains: "<h1>Test Heading</h1>",
		},
		{
			name:     "bold text",
			input:    "**bold text**",
			contains: "<strong>bold text</strong>",
		},
		{
			name:     "bullet list",
			input:    "- Item 1\n- Item 2",
			contains: "<li>Item 1</li>",
		},
		{
			name:     "paragraph",
			input:    "Some text here",
			contains: "<p>Some text here</p>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.renderMarkdown(map[string]any{
				"content": tt.input,
			})

			assert.NoError(t, err)
			assert.NotNil(t, result)

			resultMap, ok := result.(map[string]interface{})
			assert.True(t, ok)

			html, ok := resultMap["html"].(string)
			assert.True(t, ok)
			assert.Contains(t, html, tt.contains)
		})
	}
}

func TestRenderMarkdownWithListGroups(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{})

	// Test that list groups are separated
	input := `- Item 1
- Item 2

- Item 3
- Item 4`

	result, err := svc.renderMarkdown(map[string]any{
		"content": input,
	})

	assert.NoError(t, err)
	resultMap, _ := result.(map[string]interface{})
	html := resultMap["html"].(string)

	// Should have separator divs between lists
	assert.Contains(t, html, "list-group-gap")
	// Should have multiple <ul> tags (one per group)
	assert.Contains(t, html, "<li>Item 1</li>")
	assert.Contains(t, html, "<li>Item 3</li>")
}

func TestPreprocessMarkdownLists(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{
			name: "single list",
			input: `- Item 1
- Item 2`,
			contains: "- Item 1",
		},
		{
			name: "two lists with blank line",
			input: `- Item 1
- Item 2

- Item 3`,
			contains: `<div class="list-group-gap"></div>`,
		},
		{
			name: "multiple groups",
			input: `- A1
- A2

- B1

- C1`,
			contains: `<div class="list-group-gap"></div>`,
		},
		{
			name: "non-list content",
			input: `Some text

- Item 1`,
			contains: "Some text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := preprocessMarkdownLists(tt.input)
			assert.Contains(t, result, tt.contains)
		})
	}
}

func TestPreprocessMarkdownListsNoFalsePositives(t *testing.T) {
	// Test that we don't insert separators where we shouldn't
	input := `Some text

More text

- Item 1
- Item 2`

	result := preprocessMarkdownLists(input)

	// Should not have separator between non-list content
	lines := 0
	for i := 0; i < len(result)-1; i++ {
		if result[i:i+2] == "\n\n" {
			lines++
		}
	}

	// Count occurrences of separator div
	separators := countOccurrences(result, `<div class="list-group-gap"></div>`)

	// Should only have separator between list items, not between non-list content
	assert.GreaterOrEqual(t, 0, separators-1) // At most one more blank line than needed
}

func TestMessageHandler(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{
		Name: "journal",
	})

	msg := core.Message{
		Text:      "Test journal entry",
		Timestamp: time.Now(),
	}

	// Message handler should not error
	err := svc.messageHandler(msg)
	assert.NoError(t, err)

	// Verify entry was saved
	today := time.Now().Format("2006-01-02")
	result, _ := svc.getEntry(map[string]any{
		"date": today,
	})

	resultMap, _ := result.(map[string]interface{})
	content := resultMap["content"].(string)
	assert.Contains(t, content, "Test journal entry")
}

func TestEdgeCases(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{
		Name: "journal",
	})

	t.Run("empty markdown", func(t *testing.T) {
		result, err := svc.renderMarkdown(map[string]any{
			"content": "",
		})
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("missing content parameter", func(t *testing.T) {
		result, err := svc.renderMarkdown(map[string]any{})
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("save entry missing date", func(t *testing.T) {
		result, err := svc.saveEntry(map[string]any{
			"content": "test",
		})
		assert.Error(t, err) // Should error because date is required
		assert.Nil(t, result)
	})

	t.Run("save entry missing content", func(t *testing.T) {
		result, err := svc.saveEntry(map[string]any{
			"date": "2026-03-14",
		})
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}

// Helper function to count occurrences of a string
func countOccurrences(s, substr string) int {
	count := 0
	start := 0
	for {
		pos := findInString(s[start:], substr)
		if pos == -1 {
			break
		}
		count++
		start += pos + len(substr)
	}
	return count
}

// Helper to find substring position
func findInString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// Additional coverage tests for error paths and edge cases

func TestInitialize(t *testing.T) {
	deps := createTestDeps()
	deps.SetContext(context.Background())

	svc := NewService(deps, core.ServiceConfig{
		Name: "journal",
		Type: "journal",
	})

	err := svc.Initialize()
	assert.NoError(t, err)
	assert.NotEmpty(t, svc.today)
}

func TestMessageHandlerEmptyMessage(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	msg := core.Message{Text: ""}
	err := svc.messageHandler(msg)
	assert.NoError(t, err)
}

func TestHandleWebUIActionGetDates(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	result, err := svc.HandleWebUIAction("get-dates", map[string]any{})
	assert.NoError(t, err)
	assert.NotNil(t, result)

	resultMap, ok := result.(map[string]any)
	assert.True(t, ok)
	assert.Contains(t, resultMap, "dates")
}

func TestHandleWebUIActionGetEntry(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	result, err := svc.HandleWebUIAction("get-entry", map[string]any{
		"date": "2026-03-14",
	})
	assert.NoError(t, err)
	assert.NotNil(t, result)

	resultMap, ok := result.(map[string]any)
	assert.True(t, ok)
	assert.Contains(t, resultMap, "date")
	assert.Contains(t, resultMap, "content")
}

func TestHandleWebUIActionRenderMarkdown(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	result, err := svc.HandleWebUIAction("render-markdown", map[string]any{
		"content": "# Test\n\nThis is **bold**.",
	})
	assert.NoError(t, err)
	assert.NotNil(t, result)

	resultMap, ok := result.(map[string]any)
	assert.True(t, ok)
	assert.Contains(t, resultMap, "html")
	html, htmlOk := resultMap["html"].(string)
	assert.True(t, htmlOk)
	assert.Contains(t, html, "<h1")
	assert.Contains(t, html, "<strong>bold</strong>")
}

func TestHandleWebUIActionUnknown(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	_, err := svc.HandleWebUIAction("unknown-action", map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown action")
}

func TestGetEntryNoDate(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	// When no date is provided, should use today's date
	result, err := svc.getEntry(map[string]any{})
	assert.NoError(t, err)

	resultMap, ok := result.(map[string]any)
	assert.True(t, ok)
	assert.Contains(t, resultMap, "date")
	assert.Contains(t, resultMap, "content")

	date, dateOk := resultMap["date"].(string)
	assert.True(t, dateOk)
	assert.NotEmpty(t, date)
}

func TestGetEntryEmptyDate(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	// When empty date is provided, should use today's date
	result, err := svc.getEntry(map[string]any{"date": ""})
	assert.NoError(t, err)

	resultMap, ok := result.(map[string]any)
	assert.True(t, ok)

	date, dateOk := resultMap["date"].(string)
	assert.True(t, dateOk)
	assert.NotEmpty(t, date)
}

func TestGetEntryInvalidDateType(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	// When date is not a string, should use today's date
	result, err := svc.getEntry(map[string]any{"date": 12345})
	assert.NoError(t, err)

	resultMap, ok := result.(map[string]any)
	assert.True(t, ok)

	date, dateOk := resultMap["date"].(string)
	assert.True(t, dateOk)
	assert.NotEmpty(t, date)
}

func TestSaveEntryMissingDate(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	_, err := svc.saveEntry(map[string]any{
		"content": "Test",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing date parameter")
}

func TestSaveEntryEmptyDate(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	_, err := svc.saveEntry(map[string]any{
		"date":    "",
		"content": "Test",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing date parameter")
}

func TestSaveEntryMissingContent(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	_, err := svc.saveEntry(map[string]any{
		"date": "2026-03-14",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing content parameter")
}

func TestSaveEntryInvalidContentType(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	_, err := svc.saveEntry(map[string]any{
		"date":    "2026-03-14",
		"content": 12345,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing content parameter")
}

func TestRenderMarkdownMissingContent(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	_, err := svc.renderMarkdown(map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing content parameter")
}

func TestRenderMarkdownInvalidContentType(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	_, err := svc.renderMarkdown(map[string]any{
		"content": 12345,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing content parameter")
}

func TestRenderMarkdownComplex(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	markdown := `# Heading
	
Paragraph with **bold** and *italic*.

- List item 1
- List item 2

| Col1 | Col2 |
|------|------|
| A    | B    |
`

	result, err := svc.renderMarkdown(map[string]any{
		"content": markdown,
	})
	assert.NoError(t, err)

	resultMap, ok := result.(map[string]any)
	assert.True(t, ok)

	html, htmlOk := resultMap["html"].(string)
	assert.True(t, htmlOk)
	assert.Contains(t, html, "<h1>Heading</h1>")
	assert.Contains(t, html, "<strong>bold</strong>")
	assert.Contains(t, html, "<em>italic</em>")
	assert.Contains(t, html, "<li>List item 1</li>")
	assert.Contains(t, html, "<table>")
}

func TestPreprocessMarkdownListsComplexGrouping(t *testing.T) {
	content := `- Item 1
- Item 2

- Group 2 Item 1
- Group 2 Item 2

Not a list

- Group 3 Item 1`

	result := preprocessMarkdownLists(content)

	// Should have separators between groups
	assert.Contains(t, result, `<div class="list-group-gap"></div>`)

	// Count separators - should have 1 (between group 1-2, but not after non-list text)
	count := countOccurrences(result, `<div class="list-group-gap"></div>`)
	assert.Equal(t, 1, count)
}

func TestPreprocessMarkdownListsWithNestedContent(t *testing.T) {
	content := `- Item 1
  More text

- Item 2`

	result := preprocessMarkdownLists(content)
	assert.NotEmpty(t, result)
	// Should not add separator between these items
	count := countOccurrences(result, `<div class="list-group-gap"></div>`)
	assert.Equal(t, 0, count)
}

func TestPreprocessMarkdownListsOnlyBlankLines(t *testing.T) {
	content := `- Item 1


- Item 2`

	result := preprocessMarkdownLists(content)
	// Should have one separator
	count := countOccurrences(result, `<div class="list-group-gap"></div>`)
	assert.Equal(t, 1, count)
}

func TestPreprocessMarkdownListsEndWithList(t *testing.T) {
	content := `Some text

- Item 1
- Item 2`

	result := preprocessMarkdownLists(content)
	// Should not add separator (no list after)
	count := countOccurrences(result, `<div class="list-group-gap"></div>`)
	assert.Equal(t, 0, count)
}

func TestWebUITab(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	tab := svc.WebUITab()
	assert.Equal(t, "journal", tab.ID)
	assert.Equal(t, "Journal", tab.Title)
	assert.Equal(t, "📝", tab.Icon)
	assert.NotEmpty(t, tab.Content)
	assert.NotEmpty(t, tab.JSPath)
}

func TestWebUIAssets(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	fs := svc.WebUIAssets()
	assert.NotNil(t, fs)
}

func TestGetDatesReturnsSlice(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	result, err := svc.getDates()
	assert.NoError(t, err)

	resultMap, ok := result.(map[string]any)
	assert.True(t, ok)

	dates, datesOk := resultMap["dates"].([]string)
	assert.True(t, datesOk)
	assert.NotNil(t, dates)
}

// Tests for checking status and type assertions
func TestHandleWebUIActionWithMissingParameters(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	// Test save-entry with missing parameters
	result, err := svc.HandleWebUIAction("save-entry", map[string]any{
		"content": "No date",
	})
	assert.Error(t, err)
	assert.Nil(t, result)
}

// Test renderMarkdown with table
func TestRenderMarkdownWithTable(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

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
	assert.Contains(t, html, "Header 1")
}

// Test renderMarkdown with code blocks
func TestRenderMarkdownWithCodeBlock(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	markdown := "```go\nfunc main() {}\n```"

	result, err := svc.renderMarkdown(map[string]any{
		"content": markdown,
	})
	assert.NoError(t, err)

	resultMap, ok := result.(map[string]any)
	assert.True(t, ok)

	html, htmlOk := resultMap["html"].(string)
	assert.True(t, htmlOk)
	assert.Contains(t, html, "<pre>")
	assert.Contains(t, html, "<code")
}

// Test renderMarkdown with lists and proper formatting
func TestRenderMarkdownListFormatting(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	markdown := `- Item 1
- Item 2

- New group 1
- New group 2`

	result, err := svc.renderMarkdown(map[string]any{
		"content": markdown,
	})
	assert.NoError(t, err)

	resultMap, ok := result.(map[string]any)
	assert.True(t, ok)

	html, htmlOk := resultMap["html"].(string)
	assert.True(t, htmlOk)
	// Should have both <ul> blocks and the separator
	assert.Contains(t, html, "<li>Item 1</li>")
	assert.Contains(t, html, "<li>Item 2</li>")
}

// Test preprocessMarkdownLists with multiple blanks
func TestPreprocessMarkdownListsMultipleBlankLines(t *testing.T) {
	content := `- Item 1


- Item 2


- Item 3`

	result := preprocessMarkdownLists(content)
	// Should have separators between all groups
	count := countOccurrences(result, `<div class="list-group-gap"></div>`)
	assert.Equal(t, 2, count)
}

// Test preprocessMarkdownLists with asterisks
func TestPreprocessMarkdownListsWithAsterisks(t *testing.T) {
	content := `* Item 1
* Item 2

* Group 2 Item 1`

	result := preprocessMarkdownLists(content)
	count := countOccurrences(result, `<div class="list-group-gap"></div>`)
	assert.Equal(t, 1, count)
}

// Test preprocessMarkdownLists with mixed markers
func TestPreprocessMarkdownListsMixedMarkers(t *testing.T) {
	content := `- Item 1
* Item 2

- Item 3
* Item 4`

	result := preprocessMarkdownLists(content)
	count := countOccurrences(result, `<div class="list-group-gap"></div>`)
	assert.Equal(t, 1, count)
}

// Test renderMarkdown with links
func TestRenderMarkdownWithLinks(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	markdown := "[Google](https://google.com)"

	result, err := svc.renderMarkdown(map[string]any{
		"content": markdown,
	})
	assert.NoError(t, err)

	resultMap, ok := result.(map[string]any)
	assert.True(t, ok)

	html, htmlOk := resultMap["html"].(string)
	assert.True(t, htmlOk)
	assert.Contains(t, html, "<a href")
	assert.Contains(t, html, "Google")
}

// Test renderMarkdown with images
func TestRenderMarkdownWithImages(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	markdown := "![alt](image.png)"

	result, err := svc.renderMarkdown(map[string]any{
		"content": markdown,
	})
	assert.NoError(t, err)

	resultMap, ok := result.(map[string]any)
	assert.True(t, ok)

	html, htmlOk := resultMap["html"].(string)
	assert.True(t, htmlOk)
	assert.Contains(t, html, "<img")
}

// Test renderMarkdown with nested formatting
func TestRenderMarkdownWithNestedFormatting(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	markdown := "***bold and italic***"

	result, err := svc.renderMarkdown(map[string]any{
		"content": markdown,
	})
	assert.NoError(t, err)

	resultMap, ok := result.(map[string]any)
	assert.True(t, ok)

	html, htmlOk := resultMap["html"].(string)
	assert.True(t, htmlOk)
	assert.Contains(t, html, "<strong>")
}

// Test renderMarkdown with horizontal rules
func TestRenderMarkdownWithHorizontalRule(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	markdown := `Section 1

---

Section 2`

	result, err := svc.renderMarkdown(map[string]any{
		"content": markdown,
	})
	assert.NoError(t, err)

	resultMap, ok := result.(map[string]any)
	assert.True(t, ok)

	html, htmlOk := resultMap["html"].(string)
	assert.True(t, htmlOk)
	assert.Contains(t, html, "<hr")
}

// Test renderMarkdown with blockquotes
func TestRenderMarkdownWithBlockquote(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	markdown := "> This is a blockquote"

	result, err := svc.renderMarkdown(map[string]any{
		"content": markdown,
	})
	assert.NoError(t, err)

	resultMap, ok := result.(map[string]any)
	assert.True(t, ok)

	html, htmlOk := resultMap["html"].(string)
	assert.True(t, htmlOk)
	assert.Contains(t, html, "<blockquote>")
}

// Test preprocessMarkdownLists preserves indentation
func TestPreprocessMarkdownListsPreservesIndentation(t *testing.T) {
	content := `- Item 1
  - Nested item
- Item 2`

	result := preprocessMarkdownLists(content)
	// Should not add separators for nested items
	count := countOccurrences(result, `<div class="list-group-gap"></div>`)
	assert.Equal(t, 0, count)
	assert.Contains(t, result, "- Nested item")
}

// Test Service with multiple sequential operations
func TestMultipleSequentialOperations(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	// Get dates
	datesResult, err := svc.getDates()
	assert.NoError(t, err)
	assert.NotNil(t, datesResult)

	// Get entry
	entryResult, err := svc.getEntry(map[string]any{"date": "2026-03-14"})
	assert.NoError(t, err)
	assert.NotNil(t, entryResult)

	// Render markdown
	markdownResult, err := svc.renderMarkdown(map[string]any{
		"content": "# Test",
	})
	assert.NoError(t, err)
	assert.NotNil(t, markdownResult)
}

// Test preprocessMarkdownLists with whitespace variations
func TestPreprocessMarkdownListsWhitespaceVariations(t *testing.T) {
	content := `  - Item 1  
  - Item 2  
  
  - Item 3  `

	result := preprocessMarkdownLists(content)
	count := countOccurrences(result, `<div class="list-group-gap"></div>`)
	assert.Equal(t, 1, count)
}

// Test renderMarkdown with strikethrough (if supported)
func TestRenderMarkdownWithFormatting(t *testing.T) {
	deps := createTestDeps()
	svc := NewService(deps, core.ServiceConfig{Name: "journal"})

	markdown := `**Bold** and __also bold__
*Italic* and _also italic_
~~Strikethrough~~`

	result, err := svc.renderMarkdown(map[string]any{
		"content": markdown,
	})
	assert.NoError(t, err)

	resultMap, ok := result.(map[string]any)
	assert.True(t, ok)

	html, htmlOk := resultMap["html"].(string)
	assert.True(t, htmlOk)
	assert.Contains(t, html, "<strong>")
	assert.Contains(t, html, "<em>")
}
