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
