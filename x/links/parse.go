package links

import (
	"net/url"
	"strings"
)

// ParsedLink represents a single link parsed from bulk input.
type ParsedLink struct {
	URL   string
	Name  string
	Notes string
}

// ParseBulkInput parses OneTab-style newline-separated input.
// Each line can be:
//   - A bare URL (https://example.com)
//   - URL | Name (only the first pipe separates URL from Name)
//
// The name/title may contain pipe characters; only the first pipe is used as a separator.
// Blank lines and lines not starting with http:// or https:// are silently skipped.
func ParseBulkInput(text string) []ParsedLink {
	var result []ParsedLink
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		rawURL := line
		var name string

		// Check if there's a pipe separator
		if pipeIdx := strings.Index(line, "|"); pipeIdx >= 0 {
			rawURL = strings.TrimSpace(line[:pipeIdx])
			// Everything after the first pipe is the name (may contain pipes)
			name = strings.TrimSpace(line[pipeIdx+1:])
		}

		// Must start with http:// or https://
		if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
			continue
		}

		// Validate URL
		if _, err := url.Parse(rawURL); err != nil {
			continue
		}

		result = append(result, ParsedLink{
			URL:   rawURL,
			Name:  name,
			Notes: "",
		})
	}
	return result
}
