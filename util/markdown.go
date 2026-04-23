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
	"github.com/yuin/goldmark/parser"
	goldmark_html "github.com/yuin/goldmark/renderer/html"
	"go.abhg.dev/goldmark/anchor"
)

// customMonokaiStyle is monokai with warmer, less saturated replacements:
//   - #f92672 (hot pink/red) → #e8884f (muted orange) for tags, operators, namespaces
//   - #e6db74 (yellow)       → #a8c97f (soft green)   for string literals
var customMonokaiStyle = func() *chroma.Style {
	base := chromastyles.Get("monokai")
	b := base.Builder()
	orange := "#e8884f"
	green := "#a8c97f"
	blue := "#66d9ef" // monokai cyan for builtins
	purple := "#b490f4"
	b.Add(chroma.KeywordNamespace, blue)
	b.Add(chroma.NameTag, blue)
	b.Add(chroma.Operator, blue)
	b.Add(chroma.GenericDeleted, blue)
	b.Add(chroma.LiteralDate, green)
	b.Add(chroma.LiteralString, green)
	b.Add(chroma.NameVariable, purple)
	b.Add(chroma.NameBuiltin, orange) // Highlight builtins (echo, cd, sudo, etc.) in cyan
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
// Supports [[toc]] markers for table of contents generation.
func RenderMarkdown(content string) (string, error) {
	// Check if TOC marker is present and replace with placeholder
	// Use HTML comment as placeholder so it won't be rendered as content
	const tocPlaceholder = "<!-- KEYOP_TOC_PLACEHOLDER -->"
	hasTOC := strings.Contains(content, "[[toc]]")
	if hasTOC {
		content = strings.ReplaceAll(content, "[[toc]]", tocPlaceholder)
	}

	// Preprocess to preserve list grouping
	content = PreprocessMarkdownLists(content)
	// Convert plain URLs to markdown links (before wiki links so we don't double-process)
	content = PreprocessPlainLinks(content)
	// Convert wiki-style links to markdown links
	content = PreprocessWikiLinks(content)

	// Build extensions list
	// Note: We don't use goldmark's TOC extender because we generate the TOC manually
	// at the [[toc]] marker location. Using goldmark's extender would auto-insert a TOC
	// at the beginning, which complicates removal.
	extensions := []goldmark.Extender{
		extension.Table,
		&anchor.Extender{},
		highlighting.NewHighlighting(
			highlighting.WithCustomStyle(customMonokaiStyle),
			highlighting.WithFormatOptions(
			// Use customised Monokai theme which works well with dark backgrounds
			),
		),
	}

	md := goldmark.New(
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithExtensions(extensions...),
		goldmark.WithRendererOptions(
			goldmark_html.WithUnsafe(),
		),
	)

	var buf bytes.Buffer
	if err := md.Convert([]byte(content), &buf); err != nil {
		return "", fmt.Errorf("failed to render markdown: %w", err)
	}

	html := buf.String()

	// Remove the paragraph symbol (¶) from anchor links while keeping the link functionality
	html = strings.ReplaceAll(html, ">¶</a>", "></a>")

	// Add style to nested lists to eliminate extra vertical margins while preserving left indentation
	// This ensures consistent spacing between all list items regardless of nesting
	// A <ul> is nested if it appears after an <li> but before the corresponding </li>
	// This happens when there's an <li> followed by content and then <ul>
	// Replace pattern: <li>...<ul> becomes <li>...<ul style="margin-top: 0; margin-bottom: 0;">
	// This removes vertical margins but allows CSS margin-left rules to apply for indentation
	nestedListPattern := regexp.MustCompile(`<li>([^<]*)<ul>`)
	html = nestedListPattern.ReplaceAllString(html, "<li>$1<ul style=\"margin-top: 0; margin-bottom: 0;\">")

	// Also handle the case with newlines: <li>\n<ul>
	html = strings.ReplaceAll(html, "<li>\n<ul>", "<li>\n<ul style=\"margin-top: 0; margin-bottom: 0;\">")

	// If TOC was requested, manually build and insert it at the placeholder location
	if hasTOC {
		// Build a TOC manually from all h2-h6 headings with proper nesting
		// Pattern captures both the heading level and the full heading tag
		idPattern := regexp.MustCompile(`<(h[2-6])[^>]*id="([^"]*)"[^>]*>(.*?)</h[2-6]>`)
		matches := idPattern.FindAllStringSubmatch(html, -1)

		if len(matches) > 0 {
			// Build TOC HTML with proper nesting based on heading levels
			var tocHTML strings.Builder
			tocHTML.WriteString("<h2 id=\"table-of-contents\">Contents <a class=\"anchor\" href=\"#table-of-contents\"></a></h2>\n<ul>\n")

			prevLevel := 2 // Start at h2 level
			for i, match := range matches {
				headingTag := match[1] // h2, h3, h4, etc.
				headingID := match[2]
				headingContent := match[3]

				// Extract level from tag (h2 -> 2, h3 -> 3, etc.)
				currentLevel := int(headingTag[1] - '0')

				// Strip HTML tags and the paragraph symbol from heading content
				headingText := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(headingContent, "")
				headingText = strings.TrimSpace(headingText)
				headingText = strings.TrimSuffix(headingText, "¶")
				headingText = strings.TrimSpace(headingText)

				// Handle nesting: open new lists when going deeper, close when going shallower
				if currentLevel > prevLevel {
					// Open new nested lists with margin-top/bottom: 0 to maintain consistent spacing while preserving indentation
					for j := prevLevel; j < currentLevel; j++ {
						tocHTML.WriteString("<ul style=\"margin-top: 0; margin-bottom: 0;\">\n")
					}
				} else if currentLevel < prevLevel {
					// Close nested lists
					for j := currentLevel; j < prevLevel; j++ {
						tocHTML.WriteString("</li>\n</ul>\n")
					}
					tocHTML.WriteString("</li>\n")
				} else if i > 0 {
					// Same level, close the previous item
					tocHTML.WriteString("</li>\n")
				}

				fmt.Fprintf(&tocHTML, "<li><a href=\"#%s\">%s</a>", headingID, headingText)
				prevLevel = currentLevel
			}

			// Close all remaining open tags
			for j := 2; j < prevLevel; j++ {
				tocHTML.WriteString("</li>\n</ul>\n")
			}
			tocHTML.WriteString("</li>\n</ul>\n")

			// Replace the placeholder with the built TOC
			html = strings.ReplaceAll(html, tocPlaceholder, tocHTML.String())
		} else {
			// If we can't find headings, just remove the placeholder
			html = strings.ReplaceAll(html, tocPlaceholder, "")
		}
	}

	return html, nil
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
	// BUT: Don't protect lines that are list items (indented - or *)
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		if len(line) >= 4 && strings.HasPrefix(line, "    ") {
			trimmed := strings.TrimSpace(line)
			isListItem := strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ")
			if !isListItem {
				key := fmt.Sprintf("__PROTECTED_INDENT_CODE_%d__", protectKey)
				protectedLinks[key] = line
				protectKey++
				lines[i] = key
			}
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
// Note: [[toc]] is replaced with a placeholder before this function is called.
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
