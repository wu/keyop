//nolint:revive
package util

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPreprocessMarkdownLists(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "single list no gaps",
			input: `- item 1
- item 2
- item 3`,
			expected: `- item 1
- item 2
- item 3`,
		},
		{
			name: "two lists with gap",
			input: `- item 1
- item 2

- item 3
- item 4`,
			expected: `- item 1
- item 2

<div class="list-group-gap"></div>

- item 3
- item 4`,
		},
		{
			name: "multiple gaps",
			input: `- item 1

- item 2

- item 3`,
			expected: `- item 1

<div class="list-group-gap"></div>

- item 2

<div class="list-group-gap"></div>

- item 3`,
		},
		{
			name: "asterisk markers",
			input: `* item 1
* item 2

* item 3`,
			expected: `* item 1
* item 2

<div class="list-group-gap"></div>

* item 3`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PreprocessMarkdownLists(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPreprocessMarkdownListsNoFalsePositives(t *testing.T) {
	input := `Some text
- item 1

Some more text
- item 2`

	result := PreprocessMarkdownLists(input)

	// Should not have gaps because we return to non-list between them
	assert.NotContains(t, result, `<div class="list-group-gap"></div>`)
}

func TestRenderMarkdown(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, html string)
	}{
		{
			name:  "basic markdown",
			input: "# Heading\n\n**bold** and *italic*",
			check: func(t *testing.T, html string) {
				assert.Contains(t, html, "<h1>")
				assert.Contains(t, html, "<strong>")
				assert.Contains(t, html, "<em>")
			},
		},
		{
			name:  "list with gaps",
			input: "- item 1\n- item 2\n\n- item 3",
			check: func(t *testing.T, html string) {
				assert.Contains(t, html, "<ul>")
				assert.Contains(t, html, `<div class="list-group-gap"></div>`)
			},
		},
		{
			name:  "code block",
			input: "```go\nfmt.Println(\"hello\")\n```",
			check: func(t *testing.T, html string) {
				assert.Contains(t, html, "<pre")
				assert.Contains(t, html, "<code")
				assert.Contains(t, html, "hello")
			},
		},
		{
			name:  "table",
			input: "| Col1 | Col2 |\n|------|------|\n| a    | b    |",
			check: func(t *testing.T, html string) {
				assert.Contains(t, html, "<table>")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, err := RenderMarkdown(tt.input)
			assert.NoError(t, err)
			assert.NotEmpty(t, html)
			tt.check(t, html)
		})
	}
}

func TestRenderMarkdownEmpty(t *testing.T) {
	html, err := RenderMarkdown("")
	assert.NoError(t, err)
	// Empty markdown renders to empty or just whitespace
	assert.True(t, html == "" || html == "\n")
}

func TestPreprocessMarkdownListsEdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		shouldHaveGap bool
	}{
		{
			name: "blank line at start",
			input: `
- item 1`,
			shouldHaveGap: false,
		},
		{
			name: "blank line at end",
			input: `- item 1
`,
			shouldHaveGap: false,
		},
		{
			name: "consecutive blank lines",
			input: `- item 1


- item 2`,
			shouldHaveGap: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PreprocessMarkdownLists(tt.input)
			hasGap := strings.Contains(result, `<div class="list-group-gap"></div>`)
			assert.Equal(t, tt.shouldHaveGap, hasGap)
		})
	}
}

func TestPreprocessMarkdownListsWhitespace(t *testing.T) {
	input := `- item 1
- item 2
  
- item 3`

	result := PreprocessMarkdownLists(input)

	// Should detect the blank line (with or without spaces)
	assert.Contains(t, result, `<div class="list-group-gap"></div>`)
}

func TestRenderMarkdownWithLinks(t *testing.T) {
	html, err := RenderMarkdown("[link](https://example.com)")
	assert.NoError(t, err)
	assert.Contains(t, html, "<a href")
}

func TestRenderMarkdownWithImages(t *testing.T) {
	html, err := RenderMarkdown("![alt](image.png)")
	assert.NoError(t, err)
	assert.Contains(t, html, "<img")
}
