//nolint:revive
package util

import (
	"fmt"
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
				assert.Contains(t, html, "<h1") // h1 with id attribute
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
		{
			name:     "URL inside inline code block",
			input:    "Run `curl -v http://192.168.50.4:3000/movielog` now",
			expected: "Run `curl -v http://192.168.50.4:3000/movielog` now",
		},
		{
			name:     "URL inside fenced code block",
			input:    "```\ncurl -v http://192.168.50.4:3000/movielog\n```",
			expected: "```\ncurl -v http://192.168.50.4:3000/movielog\n```",
		},
		{
			name:     "URL inside indented code block",
			input:    "    curl -v http://192.168.50.4:3000/movielog",
			expected: "    curl -v http://192.168.50.4:3000/movielog",
		},
		{
			name:     "URL in text and code block",
			input:    "Visit https://example.com or run `curl http://example.com`",
			expected: "Visit [https://example.com](https://example.com) or run `curl http://example.com`",
		},
		{
			name:     "URL in 4-space indented list item (nested bullet)",
			input:    "- item\n    - http://example.com",
			expected: "- item\n    - [http://example.com](http://example.com)",
		},
		{
			name:     "URL in 6-space indented list item (deeply nested)",
			input:    "- item\n    - nested\n        - http://example.com",
			expected: "- item\n    - nested\n        - [http://example.com](http://example.com)",
		},
		{
			name:     "URL in 4-space indented asterisk list item",
			input:    "* item\n    * http://example.com",
			expected: "* item\n    * [http://example.com](http://example.com)",
		},
		{
			name:     "multiple URLs in nested list items",
			input:    "- parent\n    - https://example.com\n    - https://github.com",
			expected: "- parent\n    - [https://example.com](https://example.com)\n    - [https://github.com](https://github.com)",
		},
		{
			name:     "mixed nested list with URLs and plain text",
			input:    "- parent item\n    - Visit https://example.com for more\n    - plain text",
			expected: "- parent item\n    - Visit [https://example.com](https://example.com) for more\n    - plain text",
		},
		{
			name:     "URL in list item with text before and after",
			input:    "- item\n    - Check out https://github.com here",
			expected: "- item\n    - Check out [https://github.com](https://github.com) here",
		},
		{
			name:     "bare indented code block (non-list) should not convert",
			input:    "    http://example.com",
			expected: "    http://example.com",
		},
		{
			name:     "user reported issue: URLs at multiple indentation levels",
			input:    "* http://www.geekfarm.org/\n  * http://www.geekfarm.org/\n    * http://www.geekfarm.org/",
			expected: "* [http://www.geekfarm.org/](http://www.geekfarm.org/)\n  * [http://www.geekfarm.org/](http://www.geekfarm.org/)\n    * [http://www.geekfarm.org/](http://www.geekfarm.org/)",
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

func TestRenderMarkdownWithLinksInNestedLists(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, html string)
	}{
		{
			name:  "links at multiple indentation levels should all be linkified",
			input: "* http://www.geekfarm.org/\n  * http://www.geekfarm.org/\n    * http://www.geekfarm.org/",
			check: func(t *testing.T, html string) {
				// Should contain exactly 3 anchor tags for the 3 URLs
				count := strings.Count(html, `<a href="http://www.geekfarm.org/"`)
				assert.Equal(t, 3, count, "Should have 3 linkified URLs (one at each indentation level)")

				// Verify all three are proper links, not bare text
				assert.NotContains(t, html, `<li>http://www.geekfarm.org/</li>`, "URL should be inside <a> tag, not bare text")

				// Count closing anchor tags
				closeCount := strings.Count(html, "</a>")
				assert.Equal(t, 3, closeCount, "Should have 3 closing </a> tags")
			},
		},
		{
			name:  "link in 4-space indented list item",
			input: "- parent\n    - http://example.com",
			check: func(t *testing.T, html string) {
				// Should contain an anchor tag with the URL
				assert.Contains(t, html, `<a href="http://example.com"`)
				assert.Contains(t, html, "http://example.com</a>")
				// Should be in a list item
				assert.Contains(t, html, "<li>")
				assert.NotContains(t, html, "<li>http://example.com</li>", "URL should be inside <a> tag, not bare text")
			},
		},
		{
			name:  "link in 6-space indented deeply nested list",
			input: "- parent\n    - nested\n        - https://github.com",
			check: func(t *testing.T, html string) {
				assert.Contains(t, html, `<a href="https://github.com"`)
				assert.Contains(t, html, "https://github.com</a>")
				assert.Contains(t, html, "<li>")
			},
		},
		{
			name:  "multiple URLs in nested list",
			input: "- parent\n    - https://example.com\n    - https://github.com",
			check: func(t *testing.T, html string) {
				assert.Contains(t, html, `<a href="https://example.com"`)
				assert.Contains(t, html, `<a href="https://github.com"`)
				// Verify both are properly closed
				count := strings.Count(html, "</a>")
				assert.GreaterOrEqual(t, count, 2, "Should have at least 2 closing </a> tags")
			},
		},
		{
			name:  "link with text in nested list item",
			input: "- parent\n    - Visit https://example.com for info",
			check: func(t *testing.T, html string) {
				assert.Contains(t, html, `<a href="https://example.com"`)
				assert.Contains(t, html, "https://example.com</a>")
				assert.Contains(t, html, "Visit")
				assert.Contains(t, html, "for info")
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

func TestPreprocessWikiLinksWithSourceId(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple numeric ID",
			input:    "See [[contacts:12]] for info",
			expected: `See [contacts:12](#/contacts/12) for info`,
		},
		{
			name:     "UUID-style ID",
			input:    "Reference [[contacts:550e8400-e29b-41d4-a716-446655440000]]",
			expected: `Reference [contacts:550e8400-e29b-41d4-a716-446655440000](#/contacts/550e8400-e29b-41d4-a716-446655440000)`,
		},
		{
			name:     "source with hyphens",
			input:    "Check [[my-service:42]]",
			expected: `Check [my-service:42](#/my-service/42)`,
		},
		{
			name:     "source with underscores",
			input:    "View [[user_profile:123]]",
			expected: `View [user_profile:123](#/user_profile/123)`,
		},
		{
			name:     "source with mixed case in ID",
			input:    "Reference [[api:abc123DEF]]",
			expected: `Reference [api:abc123DEF](#/api/abc123DEF)`,
		},
		{
			name:     "multiple source:id links",
			input:    "See [[contacts:1]] and [[notes:99]]",
			expected: `See [contacts:1](#/contacts/1) and [notes:99](#/notes/99)`,
		},
		{
			name:     "mixed source:id and wiki-links",
			input:    "Check [[service:123]] and [[My Note]]",
			expected: `Check [service:123](#/service/123) and [My Note](#wiki-link "My Note")`,
		},
		{
			name:     "source:id with alphanumeric ID",
			input:    "Get [[endpoint:v2_prod_1a]]",
			expected: `Get [endpoint:v2_prod_1a](#/endpoint/v2_prod_1a)`,
		},
		{
			name:     "invalid: source:id with spaces in source",
			input:    "Bad [[bad source:123]]",
			expected: `Bad [bad source:123](#wiki-link "bad source:123")`,
		},
		{
			name:     "invalid: uppercase source prefix",
			input:    "Invalid [[Contacts:50]]",
			expected: `Invalid [Contacts:50](#wiki-link "Contacts:50")`,
		},
		{
			name:     "source:id with colons in ID part",
			input:    "Version [[source:id:extra]]",
			expected: `Version [source:id:extra](#/source/id:extra)`,
		},
		{
			name:     "source:id in sentence context",
			input:    "To find the contact, use [[contacts:550e8400-e29b-41d4-a716-446655440000]] and then check status.",
			expected: `To find the contact, use [contacts:550e8400-e29b-41d4-a716-446655440000](#/contacts/550e8400-e29b-41d4-a716-446655440000) and then check status.`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PreprocessWikiLinks(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRenderMarkdownWithSourceIdLinks(t *testing.T) {
	html, err := RenderMarkdown("See [[contacts:123]] for more info")
	assert.NoError(t, err)
	assert.Contains(t, html, "#/contacts/123")
	assert.Contains(t, html, "contacts:123")
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

func TestRenderMarkdownWithTableOfContents(t *testing.T) {
	input := `[[toc]]

## test 1

Some content here.

## test 2

More content here.

## test 3

Even more content.`

	html, err := RenderMarkdown(input)
	assert.NoError(t, err)

	// Print the actual HTML for debugging
	t.Logf("Generated HTML:\n%s\n", html)

	// Expected behavior:
	// 1. Contents should be present with "Contents" heading
	assert.Contains(t, html, "Contents", "Should have TOC heading")

	// 2. Should have links to all three sections
	assert.Contains(t, html, "test 1", "Should link to test 1")
	assert.Contains(t, html, "test 2", "Should link to test 2")
	assert.Contains(t, html, "test 3", "Should link to test 3")

	// 3. Should have the actual section headings with IDs and anchors
	assert.Contains(t, html, "id=\"test-1\"", "Should have h2 ID for test 1")
	assert.Contains(t, html, "id=\"test-2\"", "Should have h2 ID for test 2")
	assert.Contains(t, html, "id=\"test-3\"", "Should have h2 ID for test 3")
	assert.Contains(t, html, "test 1", "Should have text for test 1")
	assert.Contains(t, html, "test 2", "Should have text for test 2")
	assert.Contains(t, html, "test 3", "Should have text for test 3")

	// 4. Verify TOC appears exactly once and before content
	tocPos := strings.Index(html, "Contents")
	test1Pos := strings.Index(html, "id=\"test-1\"")
	assert.Less(t, tocPos, test1Pos, "TOC should appear before content sections")
	assert.Greater(t, tocPos, 0, "TOC should be present")

	// 5. Verify balanced tags
	openLI := strings.Count(html, "<li>")
	closeLI := strings.Count(html, "</li>")
	assert.Equal(t, openLI, closeLI, "All <li> tags should be balanced")

	openUL := strings.Count(html, "<ul>") + strings.Count(html, "<ul ")
	closeUL := strings.Count(html, "</ul>")
	assert.Equal(t, openUL, closeUL, "All <ul> tags should be balanced")

	// 6. Verify no orphaned closing tags at the beginning
	firstCloseLI := strings.Index(html, "</li>")
	firstOpenLI := strings.Index(html, "<li>")
	assert.Less(t, firstOpenLI, firstCloseLI, "Opening <li> should come before closing </li>")

	// 7. Verify content sections are present and in correct order
	assert.Contains(t, html, "Some content here", "Section 1 content should be present")
	assert.Contains(t, html, "More content here", "Section 2 content should be present")
	assert.Contains(t, html, "Even more content", "Section 3 content should be present")

	// 8. Verify no duplicate TOCs
	tocCount := strings.Count(html, "Contents")
	assert.Equal(t, 1, tocCount, "Should have exactly one Contents heading")

	// 9. Verify no stray tags before first heading
	beforeFirstHeading := html[:strings.Index(html, "<h2")]
	assert.NotContains(t, beforeFirstHeading, "</li>", "Should not have closing </li> before first heading")
	assert.NotContains(t, beforeFirstHeading, "</ul>", "Should not have closing </ul> before first heading")
}

func TestRenderMarkdownWithTableOfContentsNested(t *testing.T) {
	input := `[[toc]]

## Section One

Some intro text.

### Subsection One-A

Content for subsection one-a.

### Subsection One-B

Content for subsection one-b.

## Section Two

More content here.

### Subsection Two-A

Content for subsection two-a.`

	html, err := RenderMarkdown(input)
	assert.NoError(t, err)

	// Print the actual HTML for debugging
	t.Logf("Generated HTML:\n%s\n", html)

	// The TOC should appear where [[toc]] was placed
	assert.Contains(t, html, "Contents", "Should have TOC heading")

	// Check that TOC items are present
	assert.Contains(t, html, "Section One", "Should have link to Section One")
	assert.Contains(t, html, "Section Two", "Should have link to Section Two")
	assert.Contains(t, html, "Subsection One-A", "Should have link to Subsection One-A")
	assert.Contains(t, html, "Subsection One-B", "Should have link to Subsection One-B")
	assert.Contains(t, html, "Subsection Two-A", "Should have link to Subsection Two-A")

	// Check that the actual content is there
	assert.Contains(t, html, "id=\"section-one\"", "Should have h2 ID for Section One")
	assert.Contains(t, html, "id=\"section-two\"", "Should have h2 ID for Section Two")
	assert.Contains(t, html, "id=\"subsection-one-a\"", "Should have h3 ID for Subsection One-A")
	assert.Contains(t, html, "id=\"subsection-one-b\"", "Should have h3 ID for Subsection One-B")
	assert.Contains(t, html, "id=\"subsection-two-a\"", "Should have h3 ID for Subsection Two-A")

	// Verify TOC position and structure
	tocPos := strings.Index(html, "Contents")
	section1Pos := strings.Index(html, "id=\"section-one\"")
	assert.Less(t, tocPos, section1Pos, "TOC should appear before Section One")
	assert.Greater(t, tocPos, 0, "TOC should be present")

	// Extract TOC section and verify nesting structure
	tocSection := html[tocPos:section1Pos]

	// Verify nested lists have margin: 0 style
	assert.Contains(t, tocSection, "<ul style=\"margin-top: 0; margin-bottom: 0;\">", "Nested lists should have margin: 0")

	// Verify nesting structure: Section One should have nested list with subsections
	assert.Contains(t, tocSection, "Section One</a><ul style=\"margin-top: 0; margin-bottom: 0;\">", "Section One should have nested list")

	// Verify balanced tags in TOC
	tocOpenUL := strings.Count(tocSection, "<ul")
	tocCloseUL := strings.Count(tocSection, "</ul>")
	assert.Equal(t, tocOpenUL, tocCloseUL, "TOC lists should be balanced")

	tocOpenLI := strings.Count(tocSection, "<li>")
	tocCloseLI := strings.Count(tocSection, "</li>")
	assert.Equal(t, tocOpenLI, tocCloseLI, "TOC list items should be balanced")

	// Verify no orphaned tags
	assert.NotContains(t, tocSection, "<li>\n<ul>", "Should not have empty list item wrapping")

	// Verify all content sections are present after TOC
	assert.Contains(t, html, "Some intro text", "Section One intro should be present")
	assert.Contains(t, html, "Content for subsection one-a", "Subsection One-A content should be present")
	assert.Contains(t, html, "Content for subsection one-b", "Subsection One-B content should be present")
	assert.Contains(t, html, "More content here", "Section Two content should be present")
	assert.Contains(t, html, "Content for subsection two-a", "Subsection Two-A content should be present")
}

func TestRenderMarkdownWithTableOfContentsWithPreamble(t *testing.T) {
	input := `This is an introductory paragraph before the table of contents.

[[toc]]

## Section One

Content for section one.

## Section Two

Content for section two.`

	html, err := RenderMarkdown(input)
	assert.NoError(t, err)

	// Print the actual HTML for debugging
	t.Logf("Generated HTML:\n%s\n", html)

	// Verify intro text is present
	assert.Contains(t, html, "introductory paragraph", "Preamble text should be preserved")

	// Verify TOC is present exactly once
	tocCount := strings.Count(html, "Contents")
	assert.Equal(t, 1, tocCount, "Should have exactly one Contents")

	// Verify correct ordering
	introPos := strings.Index(html, "introductory paragraph")
	tocPos := strings.Index(html, "Contents")
	section1Pos := strings.Index(html, "id=\"section-one\"")
	section2Pos := strings.Index(html, "id=\"section-two\"")

	assert.Less(t, introPos, tocPos, "Intro should appear before TOC")
	assert.Less(t, tocPos, section1Pos, "TOC should appear before Section One")
	assert.Less(t, section1Pos, section2Pos, "Section One should appear before Section Two")

	// Verify content is present
	assert.Contains(t, html, "Content for section one", "Section One content should be present")
	assert.Contains(t, html, "Content for section two", "Section Two content should be present")

	// Verify balanced tags
	openUL := strings.Count(html, "<ul>") + strings.Count(html, "<ul ")
	closeUL := strings.Count(html, "</ul>")
	assert.Equal(t, openUL, closeUL, "All <ul> tags should be balanced")

	// Verify TOC links are correct
	assert.Contains(t, html, "href=\"#section-one\">Section One</a>", "TOC should link to Section One")
	assert.Contains(t, html, "href=\"#section-two\">Section Two</a>", "TOC should link to Section Two")
}

func TestRenderMarkdownWithNestedTableOfContents(t *testing.T) {
	input := `[[toc]]

## Section One

Content here.

### Subsection One-A

Sub content.

### Subsection One-B

More sub content.

## Section Two

More content.`

	html, err := RenderMarkdown(input)
	assert.NoError(t, err)

	// Print the actual HTML for debugging
	t.Logf("Generated HTML:\n%s\n", html)

	// TOC should be present
	assert.Contains(t, html, "Contents", "Should have TOC heading")

	// All items should be in the TOC
	assert.Contains(t, html, "Section One", "Should have Section One")
	assert.Contains(t, html, "Subsection One-A", "Should have Subsection One-A")
	assert.Contains(t, html, "Subsection One-B", "Should have Subsection One-B")
	assert.Contains(t, html, "Section Two", "Should have Section Two")

	// Check for proper nesting - subsections should be in nested <ul> tags
	// Find the TOC section from "Contents" to the end of the list (before actual content sections)
	tocStart := strings.Index(html, "Contents")
	firstContentHeading := strings.Index(html, "id=\"section-one\">Section One")
	tocSection := html[tocStart:firstContentHeading]

	t.Logf("TOC Section:\n%s\n", tocSection)

	// Count nested structure - should have multiple <ul> tags (with or without style attribute)
	ulCount := strings.Count(tocSection, "<ul")
	assert.Greater(t, ulCount, 1, "Should have nested <ul> tags for subsections")
}

func TestRenderMarkdownWithTableOfContentsNoStrayTags(t *testing.T) {
	input := `intro...

[[toc]]

## test 1

Content.

### test 1.1

Sub content.

### test 1.2

More sub.

## test 3

Final content.`

	html, err := RenderMarkdown(input)
	assert.NoError(t, err)

	// Print the actual HTML for debugging
	t.Logf("Generated HTML:\n%s\n", html)

	// Should NOT have stray <li> or </li> tags before the preamble
	introPos := strings.Index(html, "intro...")
	assert.Greater(t, introPos, 0, "Intro should be present")

	// Get the HTML before the intro
	beforeIntro := html[:introPos]

	// Count <li> tags before intro - should be 0
	liCountBefore := strings.Count(beforeIntro, "<li>")
	assert.Equal(t, 0, liCountBefore, "Should have no <li> tags before intro text")

	// Count </li> tags before intro - should be 0
	liCloseCountBefore := strings.Count(beforeIntro, "</li>")
	assert.Equal(t, 0, liCloseCountBefore, "Should have no </li> tags before intro text")

	// TOC should appear after intro
	tocPos := strings.Index(html, "Contents")
	assert.Greater(t, tocPos, introPos, "TOC should appear after intro")

	// Verify all content headings are present
	assert.Contains(t, html, "id=\"test-1\"", "Should have test-1 heading")
	assert.Contains(t, html, "id=\"test-11\"", "Should have test-1.1 heading")
	assert.Contains(t, html, "id=\"test-12\"", "Should have test-1.2 heading")
	assert.Contains(t, html, "id=\"test-3\"", "Should have test-3 heading")
}

func TestRenderMarkdownWithTableOfContentsListGrouping(t *testing.T) {
	input := `[[toc]]

## Section One

Content.

### Subsection One-A

Sub content.

### Subsection One-B

More sub.

## Section Two

Different content.`

	html, err := RenderMarkdown(input)
	assert.NoError(t, err)

	// Print the actual HTML for debugging
	t.Logf("Generated HTML:\n%s\n", html)

	// Extract just the TOC section
	tocStart := strings.Index(html, "Contents")
	sectionOneContentStart := strings.Index(html, "id=\"section-one\">Section One")
	tocSection := html[tocStart:sectionOneContentStart]

	t.Logf("TOC Section:\n%s\n", tocSection)

	// The TOC should NOT have any list-group-gap divs because it's generated
	// without blank lines between items
	assert.NotContains(t, tocSection, "list-group-gap", "TOC should not have list-group-gap since it's generated without blank lines")

	// Verify nested structure is present (count <ul with or without style attribute)
	assert.Greater(t, strings.Count(tocSection, "<ul"), 1, "Should have nested lists")
}

func TestRenderMarkdownWithTableOfContentsMissingContent(t *testing.T) {
	input := `This is the intro text.

[[toc]]

## foo

Content of foo section.

## bar

Content of bar section.

## baz

Content of baz section.`

	html, err := RenderMarkdown(input)
	assert.NoError(t, err)

	// Print the actual HTML for debugging
	t.Logf("Generated HTML:\n%s\n", html)

	// All of these should be present in the output
	assert.Contains(t, html, "intro text", "Intro text should be present")
	assert.Contains(t, html, "Contents", "TOC heading should be present")
	assert.Contains(t, html, "id=\"foo\"", "foo section should be present")
	assert.Contains(t, html, "Content of foo section", "foo content should be present")
	assert.Contains(t, html, "id=\"bar\"", "bar section should be present")
	assert.Contains(t, html, "Content of bar section", "bar content should be present")
	assert.Contains(t, html, "id=\"baz\"", "baz section should be present")
	assert.Contains(t, html, "Content of baz section", "baz content should be present")

	// Verify order: intro should come before TOC
	introPos := strings.Index(html, "intro text")
	tocPos := strings.Index(html, "Contents")
	fooPos := strings.Index(html, "id=\"foo\"")

	assert.Less(t, introPos, tocPos, "Intro should come before TOC")
	assert.Less(t, tocPos, fooPos, "TOC should come before foo section")
}

func TestRenderMarkdownWithTableOfContentsAndLists(t *testing.T) {
	input := `This is the intro text.

[[toc]]

## foo

Content of foo section with a list:

- item 1
- item 2
  - nested item 2a
  - nested item 2b
- item 3

## bar

Content of bar section.

## baz

Content of baz section.`

	html, err := RenderMarkdown(input)
	assert.NoError(t, err)

	// Print the actual HTML for debugging
	t.Logf("Generated HTML:\n%s\n", html)

	// All of these should be present in the output
	assert.Contains(t, html, "intro text", "Intro text should be present")
	assert.Contains(t, html, "Contents", "TOC heading should be present")
	assert.Contains(t, html, "id=\"foo\"", "foo section should be present")
	assert.Contains(t, html, "Content of foo section", "foo content should be present")
	assert.Contains(t, html, "item 1", "List items in foo should be present")
	assert.Contains(t, html, "nested item 2a", "Nested list items should be present")
	assert.Contains(t, html, "id=\"bar\"", "bar section should be present")
	assert.Contains(t, html, "Content of bar section", "bar content should be present")
	assert.Contains(t, html, "id=\"baz\"", "baz section should be present")
	assert.Contains(t, html, "Content of baz section", "baz content should be present")

	// Verify order: intro should come before TOC
	introPos := strings.Index(html, "intro text")
	tocPos := strings.Index(html, "Contents")
	fooPos := strings.Index(html, "id=\"foo\"")

	assert.Less(t, introPos, tocPos, "Intro should come before TOC")
	assert.Less(t, tocPos, fooPos, "TOC should come before foo section")
}

func TestRenderMarkdownListSpacingConsistent(t *testing.T) {
	input := `intro...

[[toc]]

## foo

* foo
  * bar
  * baz
* quux

## test 1

Some content.`

	html, err := RenderMarkdown(input)
	assert.NoError(t, err)

	// Print the actual HTML for debugging
	t.Logf("Generated HTML:\n%s\n", html)

	// Extract just the list part to analyze
	fooStart := strings.Index(html, "id=\"foo\">")
	test1Start := strings.Index(html, "id=\"test-1\">")
	listSection := html[fooStart:test1Start]

	t.Logf("List section:\n%s\n", listSection)

	// The list should NOT have list-group-gap divs since there are no blank lines
	assert.NotContains(t, listSection, "list-group-gap", "No blank lines means no gaps")

	// Verify nested list has margin: 0 for consistent spacing
	assert.Contains(t, listSection, "<ul style=\"margin-top: 0; margin-bottom: 0;\">", "Nested list should have margin: 0")

	// Verify proper structure
	assert.Contains(t, listSection, "<li>foo\n<ul style=\"margin-top: 0; margin-bottom: 0;\">", "foo should have nested list")
	assert.Contains(t, listSection, "<li>bar</li>", "Should have bar")
	assert.Contains(t, listSection, "<li>baz</li>", "Should have baz")
	assert.Contains(t, listSection, "</li>\n<li>quux</li>", "baz and quux should be siblings")

	// Verify balanced tags
	ulCount := strings.Count(listSection, "<ul")
	ulCloseCount := strings.Count(listSection, "</ul>")
	assert.Equal(t, ulCount, ulCloseCount, "All ul tags should be balanced")

	liCount := strings.Count(listSection, "<li>")
	liCloseCount := strings.Count(listSection, "</li>")
	assert.Equal(t, liCount, liCloseCount, "All li tags should be balanced")

	// Verify no orphaned tags
	assert.NotContains(t, listSection, "<li>\n<ul>", "Should not have empty li wrapping ul")
}

func TestRenderMarkdownListSpacingWithBlanks(t *testing.T) {
	input := `intro...

[[toc]]

## foo

* foo

* bar
  * baz

* quux

## test 1

Some content.`

	html, err := RenderMarkdown(input)
	assert.NoError(t, err)

	// Print the actual HTML for debugging
	t.Logf("Generated HTML:\n%s\n", html)

	// Extract just the list part to analyze
	fooStart := strings.Index(html, "id=\"foo\">")
	test1Start := strings.Index(html, "id=\"test-1\">")
	listSection := html[fooStart:test1Start]

	t.Logf("List section:\n%s\n", listSection)

	// The list SHOULD have list-group-gap divs since there are blank lines
	assert.Contains(t, listSection, "list-group-gap", "Blank lines should create gaps")

	// Should have multiple separate lists
	ulCount := strings.Count(listSection, "<ul")
	assert.Greater(t, ulCount, 1, "Should have multiple ul tags for separated groups")

	// Verify nested list has margin: 0
	assert.Contains(t, listSection, "<ul style=\"margin-top: 0; margin-bottom: 0;\">", "Nested list should have margin: 0")

	// Verify gaps separate the groups
	gap1Pos := strings.Index(listSection, "list-group-gap")
	bar1Pos := strings.Index(listSection, "<li>bar")
	assert.Less(t, gap1Pos, bar1Pos, "Gap should appear before bar")

	// Verify balanced tags
	ulCloseCount := strings.Count(listSection, "</ul>")
	assert.Equal(t, ulCount, ulCloseCount, "All ul tags should be balanced")

	liCount := strings.Count(listSection, "<li>")
	liCloseCount := strings.Count(listSection, "</li>")
	assert.Equal(t, liCount, liCloseCount, "All li tags should be balanced")
}

func TestRenderMarkdownTOCPlaceholder(t *testing.T) {
	// Test that verifies the exact behavior when [[toc]] is used
	input := `This is intro text.

[[toc]]

## Section One

Content one.

## Section Two

Content two.`

	html, err := RenderMarkdown(input)
	assert.NoError(t, err)

	// Print for debugging
	t.Logf("Generated HTML:\n%s\n", html)

	// The placeholder comment should NOT appear in output
	assert.NotContains(t, html, "<!-- KEYOP_TOC_PLACEHOLDER -->", "Placeholder should be replaced")

	// "toc" should NOT appear as plain text
	assert.NotContains(t, html, "<p>toc</p>", "toc as text should not appear")

	// Contents heading should be present
	assert.Contains(t, html, "Contents", "TOC should be generated")

	// All content should be present
	assert.Contains(t, html, "intro text", "Intro should be present")
	assert.Contains(t, html, "Content one", "Section one content should be present")
	assert.Contains(t, html, "Content two", "Section two content should be present")
}

func TestRenderMarkdownWithAnchors(t *testing.T) {
	input := `# Main Title

## Heading One

Some content here.

### Subheading

More content.

## Heading Two

Final content.`

	html, err := RenderMarkdown(input)
	assert.NoError(t, err)

	// Print the actual HTML for debugging
	t.Logf("Generated HTML:\n%s\n", html)

	// Expected behavior: headings should have anchor links with IDs
	// The anchor extension generates links like <a class="anchor" href="#main-title">
	assert.Contains(t, html, "class=\"anchor\"", "Should have anchor links on headings")

	// Check that headings have IDs and href attributes with anchors
	assert.Contains(t, html, "id=\"main-title\"", "Should have h1 ID")
	assert.Contains(t, html, "id=\"heading-one\"", "Should have h2 ID")
	assert.Contains(t, html, "id=\"subheading\"", "Should have h3 ID")
	assert.Contains(t, html, "id=\"heading-two\"", "Should have second h2 ID")

	// Check for anchor hrefs
	assert.Contains(t, html, "href=\"#main-title\"", "Should have anchor href for h1")
	assert.Contains(t, html, "href=\"#heading-one\"", "Should have anchor href for h2")
	assert.Contains(t, html, "href=\"#subheading\"", "Should have anchor href for h3")
	assert.Contains(t, html, "href=\"#heading-two\"", "Should have anchor href for second h2")

	// Verify heading text content is present
	assert.Contains(t, html, "Main Title", "Should have h1 text")
	assert.Contains(t, html, "Heading One", "Should have h2 text")
	assert.Contains(t, html, "Subheading", "Should have h3 text")
	assert.Contains(t, html, "Heading Two", "Should have second h2 text")

	// Verify content sections are present
	assert.Contains(t, html, "Some content here", "Should have content for section 1")
	assert.Contains(t, html, "More content", "Should have content for section 2")
	assert.Contains(t, html, "Final content", "Should have content for section 3")
}

func TestRenderMarkdownNoKMCODEInOutput(t *testing.T) {
	// A bash block containing embedded triple backticks (e.g. a script that generates
	// markdown) used to confuse the old fence regex, leaving KMCODExxx...xxxKMCODE
	// placeholders visible in the rendered output.
	input := "## Setup\n\n" +
		"```bash\n" +
		"cat > README.md << 'EOF'\n" +
		"```go\n" +
		"fmt.Println(\"hello\")\n" +
		"```\n" +
		"EOF\n" +
		"```\n\n" +
		"Done.\n"

	html, err := RenderMarkdown(input)
	assert.NoError(t, err)
	assert.NotContains(t, html, "KMCODE")
	assert.NotContains(t, html, "KMATH")
	assert.Contains(t, html, "Done.")
}

func TestRenderMarkdownManyCodeBlocksNoKMCODE(t *testing.T) {
	// Regression test: long documents with many bash blocks (high pCounter values)
	// should never leak KMCODE placeholders into the output.
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&sb, "## Section %d\n\n", i+1)
		sb.WriteString("```bash\n")
		fmt.Fprintf(&sb, "echo 'hello %d'\nls -la\n", i+1)
		sb.WriteString("```\n\n")
		fmt.Fprintf(&sb, "Run `command-%d` to proceed.\n\n", i+1)
	}
	html, err := RenderMarkdown(sb.String())
	assert.NoError(t, err)
	assert.NotContains(t, html, "KMCODE")
	assert.NotContains(t, html, "KMATH")
}

func TestRenderMarkdownStrikethrough(t *testing.T) {
	html, err := RenderMarkdown("This is ~~deleted~~ text.")
	assert.NoError(t, err)
	assert.Contains(t, html, "<del>deleted</del>")
}

func TestRenderMarkdownGitHubAlerts(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantClass   string
		wantTitle   string
		wantContent string
	}{
		{
			name:        "note alert",
			input:       "> [!NOTE]\n> This is a note.",
			wantClass:   "markdown-alert-note",
			wantTitle:   "Note",
			wantContent: "This is a note.",
		},
		{
			name:        "warning alert",
			input:       "> [!WARNING]\n> Be careful here.",
			wantClass:   "markdown-alert-warning",
			wantTitle:   "Warning",
			wantContent: "Be careful here.",
		},
		{
			name:        "tip alert",
			input:       "> [!TIP]\n> Here's a helpful tip.",
			wantClass:   "markdown-alert-tip",
			wantTitle:   "Tip",
			wantContent: "Here's a helpful tip.",
		},
		{
			name:        "important alert",
			input:       "> [!IMPORTANT]\n> This matters.",
			wantClass:   "markdown-alert-important",
			wantTitle:   "Important",
			wantContent: "This matters.",
		},
		{
			name:        "caution alert",
			input:       "> [!CAUTION]\n> Danger zone.",
			wantClass:   "markdown-alert-caution",
			wantTitle:   "Caution",
			wantContent: "Danger zone.",
		},
		{
			name:        "alert with bold content",
			input:       "> [!NOTE]\n> This has **bold** text.",
			wantClass:   "markdown-alert-note",
			wantTitle:   "Note",
			wantContent: "<strong>bold</strong>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, err := RenderMarkdown(tt.input)
			assert.NoError(t, err)
			assert.Contains(t, html, `class="markdown-alert `+tt.wantClass+`"`)
			assert.Contains(t, html, `class="markdown-alert-title"`)
			assert.Contains(t, html, tt.wantTitle)
			assert.Contains(t, html, tt.wantContent)
			assert.NotContains(t, html, "<blockquote>")
		})
	}
}

func TestRenderMarkdownGitHubAlertMultiParagraph(t *testing.T) {
	input := "> [!NOTE]\n>\n> Second paragraph here."
	html, err := RenderMarkdown(input)
	assert.NoError(t, err)
	assert.Contains(t, html, "markdown-alert-note")
	assert.Contains(t, html, "Second paragraph here.")
	assert.NotContains(t, html, "<blockquote>")
}

// ── Basic markdown elements ───────────────────────────────────────────────────

func TestRenderMarkdownTaskList(t *testing.T) {
	html, err := RenderMarkdown("- [ ] unchecked\n- [x] checked\n- [X] also checked\n- plain item")
	assert.NoError(t, err)
	// goldmark TaskList renders checkboxes as disabled inputs
	assert.Contains(t, html, `type="checkbox"`)
	assert.Contains(t, html, `disabled`)
	assert.Contains(t, html, `checked`)
	assert.Contains(t, html, "unchecked")
	assert.Contains(t, html, "plain item")
}

func TestRenderMarkdownOrderedList(t *testing.T) {
	html, err := RenderMarkdown("1. first\n2. second\n3. third")
	assert.NoError(t, err)
	assert.Contains(t, html, "<ol>")
	assert.Contains(t, html, "<li>first</li>")
	assert.Contains(t, html, "<li>second</li>")
	assert.Contains(t, html, "<li>third</li>")
}

func TestRenderMarkdownInlineCode(t *testing.T) {
	html, err := RenderMarkdown("Run `git status` to check.")
	assert.NoError(t, err)
	assert.Contains(t, html, "<code>git status</code>")
}

func TestRenderMarkdownBlockquote(t *testing.T) {
	html, err := RenderMarkdown("> This is a quote.\n> It continues here.")
	assert.NoError(t, err)
	assert.Contains(t, html, "<blockquote>")
	assert.Contains(t, html, "This is a quote.")
	assert.Contains(t, html, "It continues here.")
}

func TestRenderMarkdownHorizontalRule(t *testing.T) {
	for _, input := range []string{"---\n", "***\n", "___\n"} {
		html, err := RenderMarkdown("before\n\n" + input + "\nafter")
		assert.NoError(t, err)
		assert.Contains(t, html, "<hr", "horizontal rule not found for input: "+input)
	}
}

func TestRenderMarkdownHardLineBreak(t *testing.T) {
	// Two trailing spaces force a hard line break in CommonMark
	html, err := RenderMarkdown("line one  \nline two")
	assert.NoError(t, err)
	assert.Contains(t, html, "<br")
	assert.Contains(t, html, "line one")
	assert.Contains(t, html, "line two")
}

func TestRenderMarkdownRawHTMLPassthrough(t *testing.T) {
	// WithUnsafe() is set, so raw HTML should pass through unchanged.
	html, err := RenderMarkdown(`<div class="custom">hello</div>`)
	assert.NoError(t, err)
	assert.Contains(t, html, `<div class="custom">hello</div>`)
}

// ── Tilde code blocks ─────────────────────────────────────────────────────────

func TestRenderMarkdownTildeCodeBlock(t *testing.T) {
	html, err := RenderMarkdown("~~~bash\necho hello\n~~~")
	assert.NoError(t, err)
	assert.Contains(t, html, "<pre")
	// "echo" may be syntax-highlighted into a span; check words individually
	assert.Contains(t, html, "echo")
	assert.Contains(t, html, "hello")
	assert.NotContains(t, html, "KMCODE")
}

func TestRenderMarkdownTildeCodeBlockURLNotLinkified(t *testing.T) {
	html, err := RenderMarkdown("~~~\nhttps://example.com\n~~~")
	assert.NoError(t, err)
	// URL inside a tilde code block must not become an anchor tag
	assert.NotContains(t, html, "<a href")
	assert.Contains(t, html, "https://example.com")
}

func TestRenderMarkdownTildeCodeBlockMathNotProcessed(t *testing.T) {
	html, err := RenderMarkdown("~~~\n$x + y$\n~~~")
	assert.NoError(t, err)
	assert.NotContains(t, html, "math-inline")
	assert.Contains(t, html, "$x + y$")
}

// ── PreprocessMarkdownLists code block awareness ──────────────────────────────

func TestPreprocessMarkdownListsSkipsFencedCodeBlock(t *testing.T) {
	// List items inside a fenced code block must not trigger gap-div insertion.
	input := "```bash\n- opt1\n\n- opt2\n```"
	result := PreprocessMarkdownLists(input)
	assert.Equal(t, input, result, "code block content should be left unchanged")
	assert.NotContains(t, result, "list-group-gap")
}

func TestPreprocessMarkdownListsSkipsTildeCodeBlock(t *testing.T) {
	input := "~~~bash\n- opt1\n\n- opt2\n~~~"
	result := PreprocessMarkdownLists(input)
	assert.Equal(t, input, result)
	assert.NotContains(t, result, "list-group-gap")
}

func TestPreprocessMarkdownListsCodeBlockThenList(t *testing.T) {
	// Lists AFTER a code block should still be processed normally.
	input := "```\n- a\n\n- b\n```\n\n- x\n\n- y"
	result := PreprocessMarkdownLists(input)
	assert.NotContains(t, result, "list-group-gap\n\n- b", "gap should not appear inside code block")
	// The x/y list outside the block should have a gap
	assert.Contains(t, result, "list-group-gap")
	lines := strings.Split(result, "\n")
	// Code block content must be unchanged
	assert.Equal(t, "- a", lines[1])
	assert.Equal(t, "", lines[2])
	assert.Equal(t, "- b", lines[3])
}

func TestRenderMarkdownCodeBlockWithListItemsUnchanged(t *testing.T) {
	// End-to-end: a code block containing list-like content must render
	// without gap-div text appearing in the code block output.
	input := "```bash\n# install flags:\n- --verbose\n\n- --debug\n```"
	html, err := RenderMarkdown(input)
	assert.NoError(t, err)
	assert.NotContains(t, html, "list-group-gap")
	assert.Contains(t, html, "--verbose")
	assert.Contains(t, html, "--debug")
}

// ── GitHub alert edge cases ───────────────────────────────────────────────────

func TestRenderMarkdownMultipleAlerts(t *testing.T) {
	input := "> [!NOTE]\n> First alert.\n\n> [!WARNING]\n> Second alert."
	html, err := RenderMarkdown(input)
	assert.NoError(t, err)
	assert.Contains(t, html, "markdown-alert-note")
	assert.Contains(t, html, "markdown-alert-warning")
	assert.Contains(t, html, "First alert.")
	assert.Contains(t, html, "Second alert.")
}

func TestRenderMarkdownAlertWithInlineCode(t *testing.T) {
	input := "> [!NOTE]\n> Run `make build` first."
	html, err := RenderMarkdown(input)
	assert.NoError(t, err)
	assert.Contains(t, html, "markdown-alert-note")
	assert.Contains(t, html, "<code>make build</code>")
}

func TestRenderMarkdownAlertWithLink(t *testing.T) {
	input := "> [!TIP]\n> See [the docs](https://example.com) for details."
	html, err := RenderMarkdown(input)
	assert.NoError(t, err)
	assert.Contains(t, html, "markdown-alert-tip")
	assert.Contains(t, html, `href="https://example.com"`)
}

func TestRenderMarkdownAlertAtEndOfDocument(t *testing.T) {
	// Alert as the last element, no trailing newline
	input := "Some text.\n\n> [!CAUTION]\n> Be careful."
	html, err := RenderMarkdown(input)
	assert.NoError(t, err)
	assert.Contains(t, html, "markdown-alert-caution")
	assert.Contains(t, html, "Be careful.")
}

func TestRenderMarkdownRegularBlockquoteNotConvertedToAlert(t *testing.T) {
	// A plain blockquote must not be mistaken for an alert.
	input := "> This is just a regular quote."
	html, err := RenderMarkdown(input)
	assert.NoError(t, err)
	assert.Contains(t, html, "<blockquote>")
	assert.NotContains(t, html, "markdown-alert")
}

func TestRenderMarkdownAlertLowercaseNotConverted(t *testing.T) {
	// Lowercase [!note] is not a valid GitHub alert — must stay as blockquote.
	input := "> [!note]\n> lowercase type"
	html, err := RenderMarkdown(input)
	assert.NoError(t, err)
	assert.Contains(t, html, "<blockquote>")
	assert.NotContains(t, html, "markdown-alert")
}

// ── Math rendering ────────────────────────────────────────────────────────────

func TestRenderMarkdown_MathInline(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		absent   []string
	}{
		{
			name:     "arrow command",
			input:    "$\\rightarrow$",
			contains: []string{`class="math-inline"`, "→"},
			absent:   []string{"$\\rightarrow$"},
		},
		{
			name:     "arithmetic expression",
			input:    "$1 + 1 + 1 + 1 + 1 + 1 + 1 + 1 = ?$",
			contains: []string{`class="math-inline"`, "1 + 1", "= ?"},
			absent:   []string{"$1 + 1"},
		},
		{
			name:     "expression starting with operator",
			input:    "$+ 1$",
			contains: []string{`class="math-inline"`, "+ 1"},
			absent:   []string{"$+ 1$"},
		},
		{
			name:     "sequence with dots command",
			input:    "$0, 1, 1, 2, 3, 5, 8, 13 \\dots$",
			contains: []string{`class="math-inline"`, "…"},
			absent:   []string{"$0, 1"},
		},
		{
			name:     "single variable",
			input:    "Let $x$ be a variable",
			contains: []string{`class="math-inline"`, ">x<"},
		},
		{
			name:     "superscript",
			input:    "$x^{2}$",
			contains: []string{"<sup>2</sup>"},
		},
		{
			name:     "subscript",
			input:    "$a_{i}$",
			contains: []string{"<sub>i</sub>"},
		},
		{
			name:     "greek letter",
			input:    "$\\alpha$",
			contains: []string{"α"},
		},
		{
			name:     "fraction",
			input:    "$\\frac{a}{b}$",
			contains: []string{"<sup>a</sup>", "<sub>b</sub>"},
		},
		{
			name:     "mathbb R",
			input:    "$\\mathbb{R}$",
			contains: []string{"ℝ"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, err := RenderMarkdown(tt.input)
			assert.NoError(t, err)
			for _, want := range tt.contains {
				assert.Contains(t, html, want)
			}
			for _, unwanted := range tt.absent {
				assert.NotContains(t, html, unwanted)
			}
		})
	}
}

func TestRenderMarkdown_MathDisplay(t *testing.T) {
	html, err := RenderMarkdown("$$E = mc^2$$")
	assert.NoError(t, err)
	assert.Contains(t, html, `class="math-display"`)
	assert.Contains(t, html, "E = mc")
	assert.NotContains(t, html, "$$")
}

func TestRenderMarkdown_MathNotInCode(t *testing.T) {
	// $...$ inside a fenced code block must not be processed.
	html, err := RenderMarkdown("```\n$\\rightarrow$\n```")
	assert.NoError(t, err)
	assert.NotContains(t, html, `class="math-inline"`)
	assert.Contains(t, html, `\rightarrow`)
}

func TestRenderMarkdown_MathNotInInlineCode(t *testing.T) {
	// $...$ inside backtick code must not be processed.
	html, err := RenderMarkdown("Use `$x$` in code")
	assert.NoError(t, err)
	assert.NotContains(t, html, `class="math-inline"`)
}

func TestRenderMathExpression(t *testing.T) {
	tests := []struct {
		latex    string
		display  bool
		contains []string
	}{
		{"\\rightarrow", false, []string{"→", `class="math-inline"`}},
		{"\\dots", false, []string{"…"}},
		{"\\alpha + \\beta", false, []string{"α", "β"}},
		{"x^{2}", false, []string{"<sup>2</sup>"}},
		{"a_{n}", false, []string{"<sub>n</sub>"}},
		{"\\frac{1}{2}", false, []string{"<sup>1</sup>", "<sub>2</sub>"}},
		{"\\mathbb{R}", false, []string{"ℝ"}},
		{"E = mc^2", true, []string{`class="math-display"`, "E = mc"}},
		{"a < b", false, []string{"&lt;"}}, // HTML-escaped
	}

	for _, tt := range tests {
		t.Run(tt.latex, func(t *testing.T) {
			result := renderMathExpression(tt.latex, tt.display)
			for _, want := range tt.contains {
				assert.Contains(t, result, want)
			}
		})
	}
}

func TestRenderMarkdown_IndentedFencedCodeInList(t *testing.T) {
	// Regression test for issue where indented fenced code blocks in lists
	// were not properly protected, causing PROTECTED_CODE placeholders to appear in output.
	// This occurs in lists where the code block is indented (e.g., nested list items).
	markdown := "1.  **Initialize Modules:** Make sure you have a `go.mod` file.\n" +
		"    ```bash\n" +
		"    go mod tidy\n" +
		"    ```\n" +
		"2.  **Next Step:** Run the build.\n" +
		"    ```bash\n" +
		"    go build\n" +
		"    ```\n"

	html, err := RenderMarkdown(markdown)
	assert.NoError(t, err)

	// Should not contain any placeholder tokens
	assert.NotContains(t, html, "PROTECTED_CODE", "Placeholder tokens should not appear in rendered HTML")
	assert.NotContains(t, html, "__PROTECTED", "Protected placeholders should not leak through")
	assert.NotContains(t, html, "KMCODExxx", "Math code placeholders should not appear")
	assert.NotContains(t, html, "KMATHxxx", "Math placeholders should not appear")

	// Should contain the actual code content (may be in syntax-highlighted HTML with spans)
	assert.Contains(t, html, "mod tidy", "Code content 'mod tidy' should be in output")
	assert.Contains(t, html, "build", "Code content 'build' should be in output")

	// Should contain proper list and code block structure
	assert.Contains(t, html, "<li>", "Should have list items")
	assert.Contains(t, html, "<pre", "Should have code blocks (may include style attribute)")
	assert.Contains(t, html, "<code>", "Should have code elements")
}

func TestRenderMarkdown_MixedInlineAndFencedCode(t *testing.T) {
	// Regression test for issue where inline code backticks would interfere with fenced code blocks
	// The inline code regex `[^`]*` could match parts of triple backticks if not protected in correct order.
	markdown := "**The Update Cycle:**\n" +
		"1.  **Update the specific dependency:**\n" +
		"    ```bash\n" +
		"    go get github.com/package/name@v1.2.3\n" +
		"    ```\n" +
		"2.  **Clean up the module file:**\n" +
		"    ```bash\n" +
		"    go mod tidy\n" +
		"    ```\n" +
		"3.  **Refresh the vendor directory:**\n" +
		"    ```bash\n" +
		"    go mod vendor\n" +
		"    ```\n" +
		"4.  **Commit the changes:** You must commit both the updated `go.mod`, `go.sum`, and the updated `vendor/` directory.\n"

	html, err := RenderMarkdown(markdown)
	assert.NoError(t, err)

	// Should not contain any placeholder tokens in any form
	assert.NotContains(t, html, "PROTECTED_CODE", "PROTECTED_CODE placeholders should not appear")
	assert.NotContains(t, html, "__PROTECTED_CODE", "Underscore-wrapped placeholders should not appear")
	assert.NotContains(t, html, "KMCODExxx", "Math code protection placeholders should not appear")

	// Verify code blocks are present (may be split across syntax highlighting spans)
	assert.Contains(t, html, "get github.com/package/name", "First code block content should be present")
	assert.Contains(t, html, "tidy", "Second code block content should be present")
	assert.Contains(t, html, "vendor", "Third code block content should be present")

	// Verify inline code is present (as individual <code> elements)
	assert.Contains(t, html, "<code>go.mod</code>", "go.mod inline code should be present")
	assert.Contains(t, html, "<code>go.sum</code>", "go.sum inline code should be present")
	assert.Contains(t, html, "vendor/", "vendor/ inline code should be present")

	// Verify list structure
	assert.Contains(t, html, "<li>", "Should have list items")
	assert.Contains(t, html, "<ol>", "Should have ordered list")

	// Verify code blocks are in <pre> tags, not inline
	assert.Contains(t, html, "<pre", "Code blocks should be in <pre> elements")
}

func TestRenderMarkdown_UpdateCycleExactScenario(t *testing.T) {
	// Exact scenario from GitHub issue: Update cycle with multiple indented code blocks
	// This was the exact failing case reported by the user
	markdown := "**The Update Cycle:**\n" +
		"1.  **Update the specific dependency:**\n" +
		"    ```bash\n" +
		"    go get github.com/package/name@v1.2.3\n" +
		"    ```\n" +
		"2.  **Clean up the module file:**\n" +
		"    ```bash\n" +
		"    go mod tidy\n" +
		"    ```\n" +
		"3.  **Refresh the vendor directory:**\n" +
		"    ```bash\n" +
		"    go mod vendor\n" +
		"    ```\n" +
		"4.  **Commit the changes:** You must commit both the updated `go.mod`, `go.sum`, and the updated `vendor/` directory."

	html, err := RenderMarkdown(markdown)
	assert.NoError(t, err)

	// Main assertion: NO placeholders in output
	assert.NotContains(t, html, "PROTECTED_CODE_", "Issue: PROTECTED_CODE placeholders leaked into output")
	assert.NotContains(t, html, "__PROTECTED", "Issue: __PROTECTED placeholders leaked into output")

	// Verify all three code blocks are rendered properly
	assert.Contains(t, html, "<pre", "Should have at least one code block in <pre>")

	// Count pre tags to ensure all 3 code blocks are present
	preCount := strings.Count(html, "<pre")
	assert.GreaterOrEqual(t, preCount, 3, "Should have at least 3 code blocks (one for each step)")

	// Verify all code content is present
	assert.Contains(t, html, "github.com/package/name@v1.2.3")
	assert.Contains(t, html, "mod tidy")
	assert.Contains(t, html, "mod vendor")

	// Verify the inline code references are also present
	assert.Contains(t, html, "go.mod")
	assert.Contains(t, html, "go.sum")
}
