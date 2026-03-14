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

func TestPreprocessWikiLinks(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "single wiki link",
			input:    "See [[other page]] for more info",
			expected: `See [other page](#wiki-link "other page") for more info`,
		},
		{
			name:     "multiple wiki links",
			input:    "[[First Page]] and [[Second Page]]",
			expected: `[First Page](#wiki-link "First Page") and [Second Page](#wiki-link "Second Page")`,
		},
		{
			name:     "wiki link with spaces",
			input:    "Check out [[My Important Note]]",
			expected: `Check out [My Important Note](#wiki-link "My Important Note")`,
		},
		{
			name:     "no wiki links",
			input:    "Just regular text [with link](https://example.com)",
			expected: "Just regular text [with link](https://example.com)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PreprocessWikiLinks(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRenderMarkdownWithWikiLinks(t *testing.T) {
	html, err := RenderMarkdown("See [[Other Note]] for more info")
	assert.NoError(t, err)
	assert.Contains(t, html, "#wiki-link")
	assert.Contains(t, html, "Other Note")
}

func TestRenderMarkdownWithExternalLinks(t *testing.T) {
	html, err := RenderMarkdown("Visit [Google](https://www.google.com)")
	assert.NoError(t, err)
	assert.Contains(t, html, "https://www.google.com")
	assert.Contains(t, html, "Google")
}

func TestPreprocessPlainLinks(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "single plain https URL",
			input:    "Check out https://github.com/dustin/go-humanize for details",
			expected: "Check out [https://github.com/dustin/go-humanize](https://github.com/dustin/go-humanize) for details",
		},
		{
			name:     "single plain http URL",
			input:    "Visit http://example.com today",
			expected: "Visit [http://example.com](http://example.com) today",
		},
		{
			name:     "URL with query parameters",
			input:    "See https://www.blu-ray.com/deals/?sortby=time&category=bluray here",
			expected: "See [https://www.blu-ray.com/deals/?sortby=time&category=bluray](https://www.blu-ray.com/deals/?sortby=time&category=bluray) here",
		},
		{
			name:     "multiple plain URLs",
			input:    "Visit https://example.com and https://github.com",
			expected: "Visit [https://example.com](https://example.com) and [https://github.com](https://github.com)",
		},
		{
			name:     "markdown link with URL as text",
			input:    "[https://www.blu-ray.com/deals/?sortby=time&category=bluray](https://www.blu-ray.com/deals/?sortby=time&category=bluray)",
			expected: "[https://www.blu-ray.com/deals/?sortby=time&category=bluray](https://www.blu-ray.com/deals/?sortby=time&category=bluray)",
		},
		{
			name:     "already markdown linked URL",
			input:    "[GitHub](https://github.com)",
			expected: "[GitHub](https://github.com)",
		},
		{
			name:     "mixed plain and markdown links",
			input:    "[GitHub](https://github.com) and https://example.com",
			expected: "[GitHub](https://github.com) and [https://example.com](https://example.com)",
		},
		{
			name:     "no URLs",
			input:    "Just regular text without links",
			expected: "Just regular text without links",
		},
		{
			name:     "markdown link with custom text",
			input:    "[Click here](https://www.blu-ray.com/deals/?sortby=time) for deals",
			expected: "[Click here](https://www.blu-ray.com/deals/?sortby=time) for deals",
		},
		{
			name:     "plain URL followed by punctuation",
			input:    "See https://github.com. It's great!",
			expected: "See [https://github.com](https://github.com). It's great!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PreprocessPlainLinks(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRenderMarkdownWithPlainLinks(t *testing.T) {
	html, err := RenderMarkdown("Check out https://github.com/dustin/go-humanize for details")
	assert.NoError(t, err)
	assert.Contains(t, html, "https://github.com/dustin/go-humanize")
	assert.Contains(t, html, "<a href")
}

func TestPreprocessWikiLinksWithEmoji(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "emoji in wiki link text",
			input:    "See [[Important Note 🎯]] for details",
			expected: `See [Important Note 🎯](#wiki-link "Important Note 🎯") for details`,
		},
		{
			name:     "emoji before wiki link",
			input:    "Check this out 🔗 [[Reference]]",
			expected: `Check this out 🔗 [Reference](#wiki-link "Reference")`,
		},
		{
			name:     "emoji after wiki link",
			input:    "[[Note]] 📝 is important",
			expected: `[Note](#wiki-link "Note") 📝 is important`,
		},
		{
			name:     "emoji mixed with text and wiki link",
			input:    "Status: ✓ See [[My Tasks]] 💪",
			expected: `Status: ✓ See [My Tasks](#wiki-link "My Tasks") 💪`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PreprocessWikiLinks(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRenderMarkdownWithEmoji(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "emoji in plain text",
			input:    "This is a test 🎉 with emoji 😀",
			expected: "🎉",
		},
		{
			name:     "emoji with plain link",
			input:    "Check this 🔗 https://example.com",
			expected: "🔗",
		},
		{
			name:     "emoji with wiki link",
			input:    "Status 📝 [[Notes]] here",
			expected: "📝",
		},
		{
			name:     "complex emoji (multi-byte)",
			input:    "Heart ❤️ and flag 🇺🇸 and more",
			expected: "❤️",
		},
		{
			name:     "skin tone emoji",
			input:    "Wave 👋🏻 hello",
			expected: "👋🏻",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, err := RenderMarkdown(tt.input)
			assert.NoError(t, err)
			// Verify emoji is preserved in the output (not corrupted to replacement chars)
			assert.Contains(t, html, tt.expected, "Emoji should be preserved in rendered output")
		})
	}
}
