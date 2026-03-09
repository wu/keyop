//nolint:revive
package macosNotes

import (
	"fmt"
	"keyop/core"
	"runtime"
	"strings"
)

// Service syncs and watches macOS Notes changes and translates them into application events for downstream consumers.
type Service struct {
	Deps        core.Dependencies
	Cfg         core.ServiceConfig
	noteName    string
	parseFormat bool
}

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	var errs []error

	noteName, _ := svc.Cfg.Config["note_name"].(string)
	if noteName == "" {
		err := fmt.Errorf("required config 'note_name' is missing or empty")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	return errs
}

// Initialize performs one-time startup required by the service (resource loading or connectivity checks).
func (svc *Service) Initialize() error {
	svc.noteName, _ = svc.Cfg.Config["note_name"].(string)
	svc.parseFormat, _ = svc.Cfg.Config["parse_format"].(bool)
	return nil
}

// Check performs the service's periodic work: collect data, evaluate state, and publish messages/metrics.
func (svc *Service) Check() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("macosNotes service is only supported on macOS")
	}

	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()
	osProvider := svc.Deps.MustGetOsProvider()

	// AppleScript to get the content of the note
	appleScript := fmt.Sprintf(`tell application "Notes" to get body of note "%s"`, svc.noteName)
	cmd := osProvider.Command("osascript", "-e", appleScript)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error("failed to get note content", "note", svc.noteName, "error", err, "output", string(output))
		return fmt.Errorf("failed to get note content: %w", err)
	}

	content := strings.TrimSpace(string(output))

	if svc.parseFormat {
		logger.Warn("parsing content!", "note", svc.noteName, "content", content)
		content = svc.parseNotes(content)
		logger.Warn("parsed content!", "note", svc.noteName, "content", content)
	} else {
		content = strings.ReplaceAll(content, "<br>", "")
		content = strings.ReplaceAll(content, "<li></li>", "")
	}

	err = messenger.Send(core.Message{
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "note_content",
		Text:        content,
	})
	if err != nil {
		logger.Error("failed to send note content", "error", err)
		return err
	}

	return nil
}

func (svc *Service) parseNotes(input string) string {
	logger := svc.Deps.MustGetLogger()

	// Find Tasks section
	logger.Warn("parsing tasks section", "note", svc.noteName)
	tasksIdx := strings.Index(input, "<h2>tasks</h2>")
	if tasksIdx == -1 {
		logger.Warn("tasks section not found", "note", svc.noteName)
		return ""
	}

	// Find the end of Tasks section or start of next section
	// We look for the next <h2> after Tasks
	logger.Warn("finding end of tasks section", "note", svc.noteName)
	nextSectionIdx := strings.Index(input[tasksIdx+len("<h2>tasks</h2>"):], "<h2>")
	var tasksContent string
	if nextSectionIdx == -1 {
		tasksContent = input[tasksIdx:]
	} else {
		tasksContent = input[tasksIdx : tasksIdx+len("<h2>tasks</h2>")+nextSectionIdx]
	}

	logger.Info("tasks content", "note", svc.noteName, "content", tasksContent)

	var todo []string
	var done []string
	isDone := false

	// The input is likely a single line from AppleScript or has some newlines.
	// Let's normalize by replacing </div>, <ul>, </ul>, <li> with newlines to split easily
	// or just work with tags.

	// A more robust way might be to use a proper HTML parser, but let's try a simple approach first.
	// Replace tags with markers and then split.
	s := tasksContent
	s = strings.ReplaceAll(s, "<div>", "\n<div>\n")
	s = strings.ReplaceAll(s, "</div>", "\n</div>\n")
	s = strings.ReplaceAll(s, "<ul>", "\n<ul>\n")
	s = strings.ReplaceAll(s, "</ul>", "\n</ul>\n")
	s = strings.ReplaceAll(s, "<li>", "\n<li>")
	s = strings.ReplaceAll(s, "</li>", "</li>\n")

	logger.Info("Content after replaceall", "content", s)

	lines := strings.Split(s, "\n")
	depth := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.Contains(line, "<ul>") {
			depth++
			continue
		}
		if strings.Contains(line, "</ul>") {
			depth--
			if depth < 0 {
				depth = 0
			}
			continue
		}

		if strings.HasPrefix(line, "<li>") {
			content := line
			logger.Warn("processing list item", "content", content)
			// Remove all tags within <li>...</li>
			for {
				start := strings.Index(content, "<")
				if start == -1 {
					break
				}
				end := strings.Index(content[start:], ">")
				if end == -1 {
					break
				}
				content = content[:start] + content[start+end+1:]
			}
			content = strings.TrimSpace(content)

			if content == "" {
				continue
			}

			if strings.Contains(content, "❌") {
				logger.Info("encountered done marker")

				isDone = true
				continue
			}

			indent := ""
			if depth > 0 {
				indent = strings.Repeat("  ", depth-1)
			}
			if isDone {
				if strings.Contains(content, "@active") {
					content = strings.ReplaceAll(content, "@active", "")
					content = strings.TrimSpace(content)
				}
				formatted := fmt.Sprintf("DONE: \n  %s- %s", indent, content)
				done = append(done, formatted)
			} else {
				prefix := "TODO: "
				if strings.Contains(content, "@active") {
					prefix = "ACTIVE: "
				}
				formatted := fmt.Sprintf("%s\n  %s- %s", prefix, indent, content)
				todo = append(todo, formatted)
			}
		}
	}

	var result strings.Builder
	if len(todo) > 0 {
		for _, s := range todo {
			result.WriteString(s)
			result.WriteString("\n")
		}
	}

	if len(done) > 0 {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		for _, s := range done {
			result.WriteString(s)
			result.WriteString("\n")
		}
	}

	return strings.TrimSpace(result.String())
}
