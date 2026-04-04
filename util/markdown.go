//nolint:revive
package util

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	goldmark_html "github.com/yuin/goldmark/renderer/html"
)

// customMonokaiStyle is monokai with warmer, less saturated replacements:
//   - #f92672 (hot pink/red) → #e8884f (muted orange) for tags, operators, namespaces
//   - #e6db74 (yellow)       → #a8c97f (soft green)   for string literals
var customMonokaiStyle = func() *chroma.Style {
	base := chromastyles.Get("monokai")
	b := base.Builder()
	orange := "#e8884f"
	green := "#a8c97f"
	b.Add(chroma.KeywordNamespace, orange)
	b.Add(chroma.NameTag, orange)
	b.Add(chroma.Operator, orange)
	b.Add(chroma.GenericDeleted, orange)
	b.Add(chroma.LiteralDate, green)
	b.Add(chroma.LiteralString, green)
	style, err := b.Build()
	if err != nil {
		panic("failed to build custom monokai style: " + err.Error())
	}
	return style
}()

// RenderMarkdown converts markdown to HTML using goldmark, preserving visual list grouping.
// It preprocesses the markdown to ensure blank lines between list items create separate lists,
// which preserves visual grouping from the source markdown in the rendered output.
// Includes syntax highlighting for code blocks using Chroma.
// Also converts wiki-style links [[page title]] to internal navigation links.
// Plain URLs (http://, https://) are automatically converted to clickable links.
func RenderMarkdown(content string) (string, error) {
	// Preprocess to preserve list grouping
	content = PreprocessMarkdownLists(content)
	// Convert plain URLs to markdown links (before wiki links so we don't double-process)
	content = PreprocessPlainLinks(content)
	// Convert wiki-style links to markdown links
	content = PreprocessWikiLinks(content)

	md := goldmark.New(
		goldmark.WithExtensions(
			extension.Table,
			highlighting.NewHighlighting(
				highlighting.WithCustomStyle(customMonokaiStyle),
				highlighting.WithFormatOptions(
				// Use customised Monokai theme which works well with dark backgrounds
				),
			),
		),
		goldmark.WithRendererOptions(
			goldmark_html.WithUnsafe(),
		),
	)

	var buf bytes.Buffer
	if err := md.Convert([]byte(content), &buf); err != nil {
		return "", fmt.Errorf("failed to render markdown: %w", err)
	}

	return buf.String(), nil
}

// PreprocessMarkdownLists ensures blank lines between list items create separate lists.
// This preserves visual grouping from the source markdown in the rendered output.
// Uses HTML divs as separators between list groups, with blank lines to break list context.
func PreprocessMarkdownLists(content string) string {
	lines := strings.Split(content, "\n")
	var result []string
	var inList bool

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		isListItem := strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ")
		isBlank := trimmed == ""

		// If we're in a list and encounter a blank line followed by another list item,
		// insert a visual separator with blank lines to break the list context
		if inList && isBlank && i+1 < len(lines) {
			nextTrimmed := strings.TrimSpace(lines[i+1])
			nextIsListItem := strings.HasPrefix(nextTrimmed, "- ") || strings.HasPrefix(nextTrimmed, "* ")
			if nextIsListItem {
				// Add separator: blank line, div with gap, blank line
				result = append(result, "")
				result = append(result, `<div class="list-group-gap"></div>`)
				result = append(result, "")
				continue
			}
		}

		// Track if we're in a list
		if isListItem {
			inList = true
		} else if !isBlank {
			inList = false
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// PreprocessPlainLinks converts plain URLs (http://, https://) to markdown links.
// This makes bare URLs clickable without requiring explicit markdown link syntax.
// Example: "Check out https://github.com/dustin/go-humanize for details"
// becomes: "Check out [https://github.com/dustin/go-humanize](https://github.com/dustin/go-humanize) for details"
// Skips URLs that are already in markdown link format [text](url) or [url](url)
// Skips URLs inside code blocks (backtick or indented code)
// Strips trailing punctuation (.,;:!?) that typically ends sentences.
func PreprocessPlainLinks(content string) string {
	// First, protect all existing markdown links AND code blocks by temporarily replacing them
	// This regex matches [anything](anything) including links with URLs as text
	markdownLinkPattern := regexp.MustCompile(`\[[^\]]*\]\([^)]*\)`)
	protectedLinks := make(map[string]string)
	protectKey := 0

	result := markdownLinkPattern.ReplaceAllStringFunc(content, func(match string) string {
		key := fmt.Sprintf("__PROTECTED_LINK_%d__", protectKey)
		protectedLinks[key] = match
		protectKey++
		return key
	})

	// Protect inline code blocks (backticks) from URL processing
	// Matches `any text including urls`
	inlineCodePattern := regexp.MustCompile("`[^`]*`")
	result = inlineCodePattern.ReplaceAllStringFunc(result, func(match string) string {
		key := fmt.Sprintf("__PROTECTED_CODE_%d__", protectKey)
		protectedLinks[key] = match
		protectKey++
		return key
	})

	// Protect multi-line code blocks from URL processing
	// Matches ``` code ``` or ~~~ code ~~~
	codeBlockPattern := regexp.MustCompile("(?:```|~~~)[\\s\\S]*?(?:```|~~~)")
	result = codeBlockPattern.ReplaceAllStringFunc(result, func(match string) string {
		key := fmt.Sprintf("__PROTECTED_CODE_BLOCK_%d__", protectKey)
		protectedLinks[key] = match
		protectKey++
		return key
	})

	// Protect indented code blocks (lines starting with 4+ spaces)
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		if len(line) >= 4 && strings.HasPrefix(line, "    ") {
			key := fmt.Sprintf("__PROTECTED_INDENT_CODE_%d__", protectKey)
			protectedLinks[key] = line
			protectKey++
			lines[i] = key
		}
	}
	result = strings.Join(lines, "\n")

	// Now process plain URLs (those not already in markdown links or code blocks)
	// Regex to match URLs starting with http:// or https://
	// Captures the protocol and domain/path (stops at whitespace, bracket, or paren)
	urlPattern := regexp.MustCompile(`(https?://[^\s\]\)]+)`)
	result = urlPattern.ReplaceAllStringFunc(result, func(match string) string {
		// Strip trailing punctuation that typically ends sentences
		url := strings.TrimRight(match, ".,;:!?")
		stripped := match[len(url):]
		return fmt.Sprintf("[%s](%s)%s", url, url, stripped)
	})

	// Restore protected markdown links and code blocks
	for key, value := range protectedLinks {
		result = strings.ReplaceAll(result, key, value)
	}

	return result
}

// PreprocessWikiLinks converts wiki-style links [[page title]] to markdown links with data attributes.
// This allows for internal note linking with automatic note lookup by title.
// Example: [[My Note]] becomes [My Note](#wiki-link "My Note")
// Properly handles multi-byte UTF-8 characters like emoji.
func PreprocessWikiLinks(content string) string {
	// Use regex to find and replace [[...]] patterns
	// This is safer than manual rune iteration when dealing with multi-byte characters
	pattern := regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	result := pattern.ReplaceAllStringFunc(content, func(match string) string {
		// Extract text between [[ and ]]
		linkText := match[2 : len(match)-2]
		return `[` + linkText + `](#wiki-link "` + linkText + `")`
	})
	return result
}
