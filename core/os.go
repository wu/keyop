//nolint:revive
package core

import (
	"os"
	"time"
)

// OsProviderApi is a minimal interface to retrieve the current hostname.
// It allows dependency injection for testing or alternative OS implementations.
type OsProviderApi interface {
	Hostname() (string, error)
	UserHomeDir() (string, error)
	ReadFile(name string) ([]byte, error)
	OpenFile(name string, flag int, perm os.FileMode) (FileApi, error)
	MkdirAll(path string, perm os.FileMode) error
	RemoveAll(path string) error
	ReadDir(dirname string) ([]os.DirEntry, error)
	Stat(name string) (os.FileInfo, error)
	Chtimes(name string, atime time.Time, mtime time.Time) error
	Remove(name string) error
	Command(name string, arg ...string) CommandApi
}

// CommandApi abstracts command execution to allow testing.
type CommandApi interface {
	Run() error
	CombinedOutput() ([]byte, error)
	Output() ([]byte, error)
}

// FileApi is the minimal file-like interface used by the codebase.
// Explicitly include the common methods so f.Close and f.Write are always available
// on implementations instead of relying solely on embedded promotions.
type FileApi interface {
	// Declare methods explicitly to avoid relying on embedded interface promotion
	Close() error
	Write(p []byte) (n int, err error)
	Read(p []byte) (n int, err error)
	Seek(offset int64, whence int) (int64, error)
	WriteString(s string) (n int, err error)
}

// Ensure the standard library's *os.File implements our FileApi at compile time.
var _ FileApi = (*os.File)(nil)
