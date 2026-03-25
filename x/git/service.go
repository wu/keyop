// Package git integrates with Git repositories to detect changes and emit related events.
//
//nolint:revive
package git

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"keyop/core"
	"keyop/util"
)

// Service implements a simple version-control-backed file writer.
// It listens for messages on the configured 'input' channel, writes the full
// JSON-serialized message to a text file named from the message subject
// (Message.Summary is used as the subject). It ensures the directory exists,
// initializes a git repo if needed, runs `git add` and `git commit` using the
// subject as the commit message. On errors, it emits an error event via the
// messenger.
type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig
	// dir is the target directory where files are written and the git repo lives.
	dir string
}

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{Deps: deps, Cfg: cfg}
}

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	var errs []error

	// The "input" subscription is required so the service has a channel to listen on.
	subErrs := util.ValidateConfig("subs", svc.Cfg.Subs, []string{"input"}, logger)
	errs = append(errs, subErrs...)

	// "dir", when supplied, must be a non-empty string.
	if dirRaw, exists := svc.Cfg.Config["dir"]; exists {
		if d, ok := dirRaw.(string); !ok || d == "" {
			err := fmt.Errorf("git: config field 'dir' must be a non-empty string")
			logger.Error(err.Error())
			errs = append(errs, err)
		}
	}

	return errs
}

// Initialize performs one-time startup required by the service (resource loading or connectivity checks).
func (svc *Service) Initialize() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()
	osProvider := svc.Deps.MustGetOsProvider()

	// Determine directory from config or default
	if d, ok := svc.Cfg.Config["dir"].(string); ok && d != "" {
		svc.dir = d
	} else {
		// Default to a local directory named "version_files"
		svc.dir = "./version_files"
	}

	// Ensure directory exists
	if err := osProvider.MkdirAll(svc.dir, 0o755); err != nil {
		logger.Error("git: failed to create directory", "dir", svc.dir, "error", err)
		// send error event
		if err := messenger.Send(core.Message{
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "error",
			Text:        "failed to create directory",
			Data:        map[string]string{"op": "mkdir", "error": err.Error()},
		}); err != nil {
			logger.Error("git: failed to send error event", "error", err)
		}

		// don't fail initialization; service can still subscribe and attempt later
	}

	// Find input channel name from Subs map; default to "input"
	channelName := "input"
	if ch, ok := svc.Cfg.Subs["input"]; ok && ch.Name != "" {
		channelName = ch.Name
	}

	ctx := svc.Deps.MustGetContext()
	if err := messenger.Subscribe(ctx, svc.Cfg.Name, channelName, svc.Cfg.Type, svc.Cfg.Name, 0, svc.handleMessage); err != nil {
		logger.Error("git: failed to subscribe to channel", "channel", channelName, "error", err)
		return err
	}

	return nil
}

// Check performs the service's periodic work: collect data, evaluate state, and publish messages/metrics.
func (svc *Service) Check() error { return nil }

func (svc *Service) handleMessage(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()
	osProvider := svc.Deps.MustGetOsProvider()

	// Dispatch by payload type.
	switch msg.DataType {
	case "notes.content_rename.v1":
		return svc.handleRename(msg)
	case "notes.content_remove.v1":
		return svc.handleRemove(msg)
	}

	// Determine subject/name for the file. We assume Message.Summary is the subject.
	subject := strings.TrimSpace(msg.Summary)
	if subject == "" {
		// fallback: use first line of Text
		if msg.Text != "" {
			lines := strings.SplitN(msg.Text, "\n", 2)
			subject = strings.TrimSpace(lines[0])
		}
	}
	if subject == "" {
		// final fallback: use timestamp
		subject = time.Now().Format(time.RFC3339)
	}

	// Sanitize subject into a safe filename (remove path separators and unsafe chars)
	filename := sanitizeFilename(subject) + ".txt"

	// Ensure target dir exists (may have failed at Initialize)
	if err := osProvider.MkdirAll(svc.dir, 0o755); err != nil {
		logger.Error("git: failed to create directory on message", "dir", svc.dir, "error", err)
		sendErrorEvent(messenger, svc.Cfg, "mkdir", err, nil)
		return nil
	}

	filePath := filepath.Join(svc.dir, filename)

	// Determine file content. If this is a ContentChange event, prefer the 'new' field
	// from the message Data payload (old/new/name/updated_at). Otherwise, store the
	// full message as JSON. Prefer using typed payload decoding via DataType when available.
	var data []byte
	var err error

	const contentChangeDataType = "notes.content_change.v1"

	reg := core.GetPayloadRegistry()
	isContentChange := msg.DataType == contentChangeDataType

	if isContentChange {
		// Attempt to decode typed payload using the global registry.
		if reg != nil {
			typed, derr := reg.Decode(msg.DataType, msg.Data)
			if derr != nil {
				logger.Error("git: failed to decode typed payload", "dataType", msg.DataType, "error", derr)
				// fallthrough to other handling below
			} else {
				// If the decoded type matches our ContentChangeEvent, use its New field.
				switch v := typed.(type) {
				case *ContentChangeEvent:
					if v != nil && v.New != "" {
						data = []byte(v.New)
					} else {
						data, _ = json.MarshalIndent(v, "", "  ")
					}
				case ContentChangeEvent:
					if v.New != "" {
						data = []byte(v.New)
					} else {
						data, _ = json.MarshalIndent(v, "", "  ")
					}
				default:
					// If the decoded payload is a generic map, try to extract "new" key.
					if m, ok := typed.(map[string]interface{}); ok {
						if newVal, hasNew := m["new"]; hasNew {
							switch nv := newVal.(type) {
							case string:
								data = []byte(nv)
							case []byte:
								data = nv
							default:
								if out, merr := json.MarshalIndent(nv, "", "  "); merr == nil {
									data = out
								} else {
									logger.Error("git: failed to marshal 'new' field", "error", merr)
									data = []byte("")
								}
							}
						} else {
							data, _ = json.MarshalIndent(typed, "", "  ")
						}
					} else {
						data, _ = json.MarshalIndent(typed, "", "  ")
					}
				}
			}
		}
	}

	// If data still empty, fallback to previous behavior:
	// - If ContentChange event, look for msg.Data['new'] when msg.Data is a map.
	// - Otherwise, serialize full message.
	if len(data) == 0 {
		if isContentChange {
			if m, ok := msg.Data.(map[string]any); ok {
				if newVal, hasNew := m["new"]; hasNew {
					switch v := newVal.(type) {
					case string:
						data = []byte(v)
					case []byte:
						data = v
					default:
						if out, merr := json.MarshalIndent(v, "", "  "); merr == nil {
							data = out
						} else {
							logger.Error("git: failed to marshal 'new' field", "error", merr)
							data = []byte("")
						}
					}
				} else {
					// No 'new' field; fall back to full message
					data, err = json.MarshalIndent(msg, "", "  ")
					if err != nil {
						logger.Error("git: failed to marshal message", "error", err)
						sendErrorEvent(messenger, svc.Cfg, "marshal", err, nil)
						return nil
					}
				}
			} else {
				// Data is not a map - try to marshal it directly if present
				if msg.Data != nil {
					if out, merr := json.MarshalIndent(msg.Data, "", "  "); merr == nil {
						data = out
					} else {
						logger.Error("git: failed to marshal msg.Data", "error", merr)
						data, _ = json.MarshalIndent(msg, "", "  ")
					}
				} else {
					data, _ = json.MarshalIndent(msg, "", "  ")
				}
			}
		} else {
			// Serialize full message to JSON for storage
			data, err = json.MarshalIndent(msg, "", "  ")
			if err != nil {
				logger.Error("git: failed to marshal message", "error", err)
				sendErrorEvent(messenger, svc.Cfg, "marshal", err, nil)
				return nil
			}
		}
	}

	// Write file
	f, err := osProvider.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		logger.Error("git: failed to open file for writing", "file", filePath, "error", err)
		sendErrorEvent(messenger, svc.Cfg, "write", err, nil)
		return nil
	}
	defer func() {
		if err := f.Close(); err != nil {
			logger.Error("git: failed to close file", "file", filePath, "error", err)
		}
	}()

	if _, err := f.Write(data); err != nil {
		logger.Error("git: failed to write file", "file", filePath, "error", err)
		sendErrorEvent(messenger, svc.Cfg, "write", err, nil)
		return nil
	}

	// Ensure git repo exists
	gitDir := filepath.Join(svc.dir, ".git")
	if _, err := osProvider.Stat(gitDir); err != nil {
		// if not exists, initialize
		if os.IsNotExist(err) {
			cmd := osProvider.Command("git", "-C", svc.dir, "init")
			out, cmdErr := cmd.CombinedOutput()
			if cmdErr != nil {
				logger.Error("git: git init failed", "dir", svc.dir, "error", cmdErr, "output", string(out))
				sendErrorEvent(messenger, svc.Cfg, "git-init", cmdErr, out)
				// continue; do not return error to avoid retry loops
			}
		} else {
			logger.Error("git: stat .git failed", "dir", svc.dir, "error", err)
			sendErrorEvent(messenger, svc.Cfg, "git-stat", err, nil)
		}
	}

	// git add <file>
	cmdAdd := osProvider.Command("git", "-C", svc.dir, "add", filename)
	outAdd, addErr := cmdAdd.CombinedOutput()
	if addErr != nil {
		logger.Error("git: git add failed", "file", filename, "error", addErr, "output", string(outAdd))
		sendErrorEvent(messenger, svc.Cfg, "git-add", addErr, outAdd)
		// continue
	}

	// git commit -m <subject>
	cmdCommit := osProvider.Command("git", "-C", svc.dir, "commit", "-m", subject)
	outCommit, commitErr := cmdCommit.CombinedOutput()
	if commitErr != nil {
		// ignore "nothing to commit" errors
		outStr := string(outCommit)
		if strings.Contains(outStr, "nothing to commit") || strings.Contains(outStr, "nothing added to commit") {
			// nothing new — not an error
			logger.Debug("git: nothing to commit", "file", filename, "output", outStr)
		} else {
			logger.Error("git: git commit failed", "file", filename, "error", commitErr, "output", outStr)
			sendErrorEvent(messenger, svc.Cfg, "git-commit", commitErr, outCommit)
		}
	}

	return nil
}

// handleRename processes a content_rename event: git mv old file → new file, then commit.
func (svc *Service) handleRename(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()
	osProvider := svc.Deps.MustGetOsProvider()

	// Decode the rename payload.
	var oldName, newName string
	reg := core.GetPayloadRegistry()
	if reg != nil && msg.DataType != "" {
		if typed, err := reg.Decode(msg.DataType, msg.Data); err == nil {
			switch v := typed.(type) {
			case *ContentRenameEvent:
				oldName, newName = v.OldName, v.NewName
			case ContentRenameEvent:
				oldName, newName = v.OldName, v.NewName
			}
		}
	}
	// Fallback: try raw map (untyped decode path).
	if oldName == "" {
		if m, ok := msg.Data.(map[string]any); ok {
			oldName, _ = m["old_name"].(string)
			newName, _ = m["new_name"].(string)
		}
	}

	if oldName == "" || newName == "" || oldName == newName {
		return nil
	}

	oldFilename := sanitizeFilename(oldName) + ".txt"
	newFilename := sanitizeFilename(newName) + ".txt"

	if err := osProvider.MkdirAll(svc.dir, 0o755); err != nil {
		logger.Error("git: failed to create directory for rename", "dir", svc.dir, "error", err)
		sendErrorEvent(messenger, svc.Cfg, "mkdir", err, nil)
		return nil
	}

	// Only rename if the old file exists.
	oldPath := filepath.Join(svc.dir, oldFilename)
	if _, err := osProvider.Stat(oldPath); err != nil {
		if os.IsNotExist(err) {
			logger.Debug("git: rename skipped — old file does not exist", "file", oldFilename)
			return nil
		}
		logger.Error("git: stat failed for rename", "file", oldFilename, "error", err)
		return nil
	}

	// git mv <old> <new>
	cmdMv := osProvider.Command("git", "-C", svc.dir, "mv", oldFilename, newFilename)
	outMv, mvErr := cmdMv.CombinedOutput()
	if mvErr != nil {
		logger.Error("git: git mv failed", "old", oldFilename, "new", newFilename, "error", mvErr, "output", string(outMv))
		sendErrorEvent(messenger, svc.Cfg, "git-mv", mvErr, outMv)
		return nil
	}

	// git commit -m "Rename <old> to <new>"
	commitMsg := fmt.Sprintf("Rename %s to %s", oldName, newName)
	cmdCommit := osProvider.Command("git", "-C", svc.dir, "commit", "-m", commitMsg)
	outCommit, commitErr := cmdCommit.CombinedOutput()
	if commitErr != nil {
		outStr := string(outCommit)
		if !strings.Contains(outStr, "nothing to commit") && !strings.Contains(outStr, "nothing added to commit") {
			logger.Error("git: git commit failed after mv", "error", commitErr, "output", outStr)
			sendErrorEvent(messenger, svc.Cfg, "git-commit", commitErr, outCommit)
		}
	}

	return nil
}

// handleRemove processes a content_remove event: git rm the file, then commit.
func (svc *Service) handleRemove(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()
	osProvider := svc.Deps.MustGetOsProvider()

	// Decode the remove payload.
	var name string
	reg := core.GetPayloadRegistry()
	if reg != nil && msg.DataType != "" {
		if typed, err := reg.Decode(msg.DataType, msg.Data); err == nil {
			switch v := typed.(type) {
			case *ContentRemoveEvent:
				name = v.Name
			case ContentRemoveEvent:
				name = v.Name
			}
		}
	}
	// Fallback: try raw map or Summary.
	if name == "" {
		if m, ok := msg.Data.(map[string]any); ok {
			name, _ = m["name"].(string)
		}
	}
	if name == "" {
		name = strings.TrimSpace(msg.Summary)
	}
	if name == "" {
		return nil
	}

	filename := sanitizeFilename(name) + ".txt"
	filePath := filepath.Join(svc.dir, filename)

	// Only proceed if the file actually exists.
	if _, err := osProvider.Stat(filePath); err != nil {
		if os.IsNotExist(err) {
			logger.Debug("git: remove skipped — file does not exist", "file", filename)
			return nil
		}
		logger.Error("git: stat failed for remove", "file", filename, "error", err)
		return nil
	}

	// git rm <file>
	cmdRm := osProvider.Command("git", "-C", svc.dir, "rm", filename)
	outRm, rmErr := cmdRm.CombinedOutput()
	if rmErr != nil {
		logger.Error("git: git rm failed", "file", filename, "error", rmErr, "output", string(outRm))
		sendErrorEvent(messenger, svc.Cfg, "git-rm", rmErr, outRm)
		return nil
	}

	// git commit -m "Delete <name>"
	commitMsg := fmt.Sprintf("Delete %s", name)
	cmdCommit := osProvider.Command("git", "-C", svc.dir, "commit", "-m", commitMsg)
	outCommit, commitErr := cmdCommit.CombinedOutput()
	if commitErr != nil {
		outStr := string(outCommit)
		if !strings.Contains(outStr, "nothing to commit") && !strings.Contains(outStr, "nothing added to commit") {
			logger.Error("git: git commit failed after rm", "error", commitErr, "output", outStr)
			sendErrorEvent(messenger, svc.Cfg, "git-commit", commitErr, outCommit)
		}
	}

	return nil
}

func sendErrorEvent(messenger core.MessengerApi, cfg core.ServiceConfig, op string, err error, output []byte) {
	payload := map[string]string{"op": op}
	if err != nil {
		payload["error"] = err.Error()
	}
	if output != nil {
		payload["output"] = string(output)
	}
	if err := messenger.Send(core.Message{
		ChannelName: cfg.Name,
		ServiceName: cfg.Name,
		ServiceType: cfg.Type,
		Event:       "error",
		Text:        fmt.Sprintf("git: %s failed", op),
		Data:        payload,
	}); err != nil {
		fmt.Printf("git: failed to send error event: %v\n", err)
	}
}

// sanitizeFilename makes a filesystem-safe filename from an arbitrary string.
// It removes path separators and replaces runes that are not [A-Za-z0-9._-] with '_'.
func sanitizeFilename(s string) string {
	// Trim spaces
	s = strings.TrimSpace(s)
	// Replace path separators
	s = strings.ReplaceAll(s, string(os.PathSeparator), "_")
	// Replace other potentially problematic chars
	re := regexp.MustCompile(`[^A-Za-z0-9._-]`)
	s = re.ReplaceAllString(s, "_")
	// Trim to reasonable length
	if len(s) > 100 {
		s = s[:100]
	}
	if s == "" {
		return "message"
	}
	return s
}
