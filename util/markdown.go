//nolint:revive
package util

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	goldmark_html "github.com/yuin/goldmark/renderer/html"
)

// RenderMarkdown converts markdown to HTML using goldmark, preserving visual list grouping.
// It preprocesses the markdown to ensure blank lines between list items create separate lists,
// which preserves visual grouping from the source markdown in the rendered output.
// Includes syntax highlighting for code blocks using Chroma.
func RenderMarkdown(content string) (string, error) {
	// Preprocess to preserve list grouping
	content = PreprocessMarkdownLists(content)

	md := goldmark.New(
		goldmark.WithExtensions(
			extension.Table,
			highlighting.NewHighlighting(
				highlighting.WithStyle("monokai"),
				highlighting.WithFormatOptions(
				// Use the Monokai theme which works well with dark backgrounds
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
