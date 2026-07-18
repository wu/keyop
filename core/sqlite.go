package core

import (
	"fmt"
	"os"
	"path/filepath"
)

// ExpandHome replaces a leading ~ in path with the user's home directory.
// The path is returned unchanged if it does not start with ~ or the home
// directory cannot be determined.
func ExpandHome(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

// SQLiteDSN builds a connection string for modernc.org/sqlite that enables a
// busy timeout, WAL journaling, and NORMAL synchronous mode so concurrent
// writers wait for locks instead of failing immediately with SQLITE_BUSY.
// A leading ~ in dbPath is expanded, so callers may pass raw or
// pre-expanded paths interchangeably.
// modernc.org/sqlite only honors _pragma=name(value) query parameters;
// mattn-style parameters (_journal_mode, _timeout, ...) are silently ignored.
func SQLiteDSN(dbPath string) string {
	return fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", ExpandHome(dbPath))
}
