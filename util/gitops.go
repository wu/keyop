package util

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/wu/keyop/core"
)

// EnsureGitRepo initializes a git repository in dir if one does not already
// exist. Returns an error if the .git directory cannot be stat'd for a reason
// other than not existing, or if git init fails.
func EnsureGitRepo(os core.OsProviderApi, dir string) error {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	if err == nil {
		return nil // repo already present
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat .git in %q: %w", dir, err)
	}
	out, initErr := os.Command("git", "-C", dir, "init").CombinedOutput()
	if initErr != nil {
		return fmt.Errorf("git init in %q: %w (output: %s)", dir, initErr, out)
	}
	return nil
}

// SetGitIdentity sets user.name and user.email in the local repo config at dir.
// Call this after EnsureGitRepo to guarantee commits succeed in environments
// where no global git identity is configured (e.g. containers).
func SetGitIdentity(os core.OsProviderApi, dir, name, email string) error {
	if out, err := os.Command("git", "-C", dir, "config", "user.name", name).CombinedOutput(); err != nil {
		return fmt.Errorf("git config user.name in %q: %w (output: %s)", dir, err, out)
	}
	if out, err := os.Command("git", "-C", dir, "config", "user.email", email).CombinedOutput(); err != nil {
		return fmt.Errorf("git config user.email in %q: %w (output: %s)", dir, err, out)
	}
	return nil
}

// GitCommitAll stages all changes in dir with "git add -A" and commits with
// message. Returns nil if there is nothing to commit.
func GitCommitAll(os core.OsProviderApi, dir, message string) error {
	if out, err := os.Command("git", "-C", dir, "add", "-A").CombinedOutput(); err != nil {
		return fmt.Errorf("git add -A in %q: %w (output: %s)", dir, err, out)
	}
	out, err := os.Command("git", "-C", dir, "commit", "-m", message).CombinedOutput()
	if err != nil {
		outStr := string(out)
		if strings.Contains(outStr, "nothing to commit") || strings.Contains(outStr, "nothing added to commit") {
			return nil
		}
		return fmt.Errorf("git commit in %q: %w (output: %s)", dir, err, outStr)
	}
	return nil
}
