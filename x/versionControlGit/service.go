// Package versionControlGit integrates with Git repositories to detect changes and emit related events.
//
//nolint:revive
package versionControlGit

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
	// dataPath, when set, selects a node inside Message.Data using dot notation
	// (e.g. "new.content"). If empty, the entire message is stored as JSON.
	dataPath string
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
			err := fmt.Errorf("versionControlGit: config field 'dir' must be a non-empty string")
			logger.Error(err.Error())
			errs = append(errs, err)
		}
	}

	// "data_path", when supplied, must be a non-empty string.
	if dpRaw, exists := svc.Cfg.Config["data_path"]; exists {
		if dp, ok := dpRaw.(string); !ok || dp == "" {
			err := fmt.Errorf("versionControlGit: config field 'data_path' must be a non-empty string")
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

	// Optional data path configuration: select a node inside Message.Data
	if p, ok := svc.Cfg.Config["data_path"].(string); ok && p != "" {
		svc.dataPath = p
	}

	// Ensure directory exists
	if err := osProvider.MkdirAll(svc.dir, 0o755); err != nil {
		logger.Error("versionControlGit: failed to create directory", "dir", svc.dir, "error", err)
		// send error event
		if err := messenger.Send(core.Message{
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "error",
			Text:        "failed to create directory",
			Data:        map[string]string{"op": "mkdir", "error": err.Error()},
		}); err != nil {
			logger.Error("versionControlGit: failed to send error event", "error", err)
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
		logger.Error("versionControlGit: failed to subscribe to channel", "channel", channelName, "error", err)
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
		logger.Error("versionControlGit: failed to create directory on message", "dir", svc.dir, "error", err)
		sendErrorEvent(messenger, svc.Cfg, "mkdir", err, nil)
		return nil
	}

	filePath := filepath.Join(svc.dir, filename)

	// Determine file content. If dataPath is set, try to extract that node from msg.Data.
	var data []byte
	var err error
	if svc.dataPath != "" {
		extracted, exErr := extractDataNode(msg.Data, svc.dataPath)
		if exErr != nil {
			logger.Error("versionControlGit: failed to extract data node", "path", svc.dataPath, "error", exErr)
			sendErrorEvent(messenger, svc.Cfg, "extract-data-node", exErr, nil)
			// fall back to storing full message
			data, err = json.MarshalIndent(msg, "", "  ")
			if err != nil {
				logger.Error("versionControlGit: failed to marshal message (fallback)", "error", err)
				sendErrorEvent(messenger, svc.Cfg, "marshal", err, nil)
				return nil
			}
		} else {
			data = extracted
		}
	} else {
		// Serialize full message to JSON for storage
		data, err = json.MarshalIndent(msg, "", "  ")
		if err != nil {
			logger.Error("versionControlGit: failed to marshal message", "error", err)
			sendErrorEvent(messenger, svc.Cfg, "marshal", err, nil)
			return nil
		}
	}

	// Write file
	f, err := osProvider.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		logger.Error("versionControlGit: failed to open file for writing", "file", filePath, "error", err)
		sendErrorEvent(messenger, svc.Cfg, "write", err, nil)
		return nil
	}
	defer func() {
		if err := f.Close(); err != nil {
			logger.Error("versionControlGit: failed to close file", "file", filePath, "error", err)
		}
	}()

	if _, err := f.Write(data); err != nil {
		logger.Error("versionControlGit: failed to write file", "file", filePath, "error", err)
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
				logger.Error("versionControlGit: git init failed", "dir", svc.dir, "error", cmdErr, "output", string(out))
				sendErrorEvent(messenger, svc.Cfg, "git-init", cmdErr, out)
				// continue; do not return error to avoid retry loops
			}
		} else {
			logger.Error("versionControlGit: stat .git failed", "dir", svc.dir, "error", err)
			sendErrorEvent(messenger, svc.Cfg, "git-stat", err, nil)
		}
	}

	// git add <file>
	cmdAdd := osProvider.Command("git", "-C", svc.dir, "add", filename)
	outAdd, addErr := cmdAdd.CombinedOutput()
	if addErr != nil {
		logger.Error("versionControlGit: git add failed", "file", filename, "error", addErr, "output", string(outAdd))
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
			logger.Debug("versionControlGit: nothing to commit", "file", filename, "output", outStr)
		} else {
			logger.Error("versionControlGit: git commit failed", "file", filename, "error", commitErr, "output", outStr)
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
		Text:        fmt.Sprintf("versionControlGit: %s failed", op),
		Data:        payload,
	}); err != nil {
		fmt.Printf("versionControlGit: failed to send error event: %v\n", err)
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

// extractDataNode extracts a node from the message Data according to dot notation path
// and returns the content bytes to store in the file. Rules:
// - If final value is a string, return its bytes directly.
// - Otherwise, marshal the final value to pretty JSON and return that.
// - If the path does not exist or msgData is nil, return an error.
func extractDataNode(msgData interface{}, path string) ([]byte, error) {
	if msgData == nil {
		return nil, fmt.Errorf("message Data is nil")
	}

	// Convert msgData into a generic map[string]interface{} by marshaling -> unmarshaling.
	b, err := json.Marshal(msgData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal msg.Data: %w", err)
	}
	var node interface{}
	if err := json.Unmarshal(b, &node); err != nil {
		return nil, fmt.Errorf("failed to unmarshal msg.Data: %w", err)
	}

	parts := strings.Split(path, ".")
	cur := node
	for _, p := range parts {
		// current must be a map[string]interface{}
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("path element '%s' not found", p)
		}
		v, exists := m[p]
		if !exists {
			return nil, fmt.Errorf("path element '%s' not found", p)
		}
		cur = v
	}

	// cur is the selected node
	switch v := cur.(type) {
	case string:
		return []byte(v), nil
	case []byte:
		return v, nil
	default:
		// pretty-print JSON
		out, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected node: %w", err)
		}
		return out, nil
	}
}
