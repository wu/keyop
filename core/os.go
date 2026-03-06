//nolint:revive
package core

import (
	"io"
	"os"
	"os/exec"
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

// FakeFile is a simple in-memory file used by tests. We keep an embedded
// ReadWriteSeeker for convenience but also provide explicit method
// implementations to ensure the methods exist on *FakeFile.
type FakeFile struct {
	io.ReadWriteSeeker
	CloseFunc func() error
}

func (f *FakeFile) Close() error {
	if f.CloseFunc != nil {
		return f.CloseFunc()
	}
	return nil
}

func (f *FakeFile) WriteString(s string) (n int, err error) {
	return f.Write([]byte(s))
}

// Provide explicit Read/Write/Seek so callers don't depend on promotion.
func (f *FakeFile) Write(p []byte) (int, error) {
	if f.ReadWriteSeeker != nil {
		return f.ReadWriteSeeker.Write(p)
	}
	return 0, os.ErrInvalid
}

func (f *FakeFile) Read(p []byte) (int, error) {
	if f.ReadWriteSeeker != nil {
		return f.ReadWriteSeeker.Read(p)
	}
	return 0, os.ErrInvalid
}

func (f *FakeFile) Seek(offset int64, whence int) (int64, error) {
	if f.ReadWriteSeeker != nil {
		return f.ReadWriteSeeker.Seek(offset, whence)
	}
	return 0, os.ErrInvalid
}

// OsProvider is the production implementation of OsProviderApi using the standard library.
type OsProvider struct{}

func (OsProvider) Hostname() (string, error)    { return os.Hostname() }
func (OsProvider) UserHomeDir() (string, error) { return os.UserHomeDir() }
func (OsProvider) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name) //nolint:gosec // intentional OS wrapper forwarding variable path
}
func (OsProvider) OpenFile(name string, flag int, perm os.FileMode) (FileApi, error) {
	return os.OpenFile(name, flag, perm) //nolint:gosec // intentional OS wrapper
}
func (OsProvider) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}
func (OsProvider) RemoveAll(path string) error {
	return os.RemoveAll(path)
}
func (OsProvider) ReadDir(dirname string) ([]os.DirEntry, error) {
	return os.ReadDir(dirname)
}
func (OsProvider) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}
func (OsProvider) Chtimes(name string, atime time.Time, mtime time.Time) error {
	return os.Chtimes(name, atime, mtime)
}
func (OsProvider) Remove(name string) error {
	return os.Remove(name)
}
func (OsProvider) Command(name string, arg ...string) CommandApi {
	return exec.Command(name, arg...) //nolint:gosec // intentional OS wrapper for executing configured commands
}

// FakeOsProvider is provided for testing
type FakeOsProvider struct {
	Host    string
	Home    string
	Err     error
	HomeErr error

	ReadFileFunc    func(name string) ([]byte, error)
	UserHomeDirFunc func() (string, error)
	OpenFileFunc    func(name string, flag int, perm os.FileMode) (FileApi, error)
	MkdirAllFunc    func(path string, perm os.FileMode) error
	RemoveAllFunc   func(path string) error
	ReadDirFunc     func(dirname string) ([]os.DirEntry, error)
	StatFunc        func(name string) (os.FileInfo, error)
	ChtimesFunc     func(name string, atime time.Time, mtime time.Time) error
	RemoveFunc      func(name string) error
	CommandFunc     func(name string, arg ...string) CommandApi

	File FileApi
}

func (f FakeOsProvider) Hostname() (string, error) { return f.Host, f.Err }
func (f FakeOsProvider) UserHomeDir() (string, error) {
	if f.UserHomeDirFunc != nil {
		return f.UserHomeDirFunc()
	}
	return f.Home, f.HomeErr
}
func (f FakeOsProvider) ReadFile(name string) ([]byte, error) {
	if f.ReadFileFunc != nil {
		return f.ReadFileFunc(name)
	}
	return nil, os.ErrNotExist
}
func (f FakeOsProvider) OpenFile(name string, flag int, perm os.FileMode) (FileApi, error) {
	if f.OpenFileFunc != nil {
		return f.OpenFileFunc(name, flag, perm)
	}
	if f.File != nil {
		return f.File, nil
	}
	return nil, os.ErrNotExist
}
func (f FakeOsProvider) MkdirAll(path string, perm os.FileMode) error {
	if f.MkdirAllFunc != nil {
		return f.MkdirAllFunc(path, perm)
	}
	return nil
}
func (f FakeOsProvider) RemoveAll(path string) error {
	if f.RemoveAllFunc != nil {
		return f.RemoveAllFunc(path)
	}
	return nil
}
func (f FakeOsProvider) ReadDir(dirname string) ([]os.DirEntry, error) {
	if f.ReadDirFunc != nil {
		return f.ReadDirFunc(dirname)
	}
	return nil, nil
}
func (f FakeOsProvider) Stat(name string) (os.FileInfo, error) {
	if f.StatFunc != nil {
		return f.StatFunc(name)
	}
	return nil, os.ErrNotExist
}
func (f FakeOsProvider) Chtimes(name string, atime time.Time, mtime time.Time) error {
	if f.ChtimesFunc != nil {
		return f.ChtimesFunc(name, atime, mtime)
	}
	return nil
}
func (f FakeOsProvider) Remove(name string) error {
	if f.RemoveFunc != nil {
		return f.RemoveFunc(name)
	}
	return nil
}
func (f FakeOsProvider) Command(name string, arg ...string) CommandApi {
	if f.CommandFunc != nil {
		return f.CommandFunc(name, arg...)
	}
	return &FakeCommand{}
}

type FakeCommand struct {
	RunFunc            func() error
	CombinedOutputFunc func() ([]byte, error)
	OutputFunc         func() ([]byte, error)
}

func (f *FakeCommand) Run() error {
	if f.RunFunc != nil {
		return f.RunFunc()
	}
	return nil
}
func (f *FakeCommand) CombinedOutput() ([]byte, error) {
	if f.CombinedOutputFunc != nil {
		return f.CombinedOutputFunc()
	}
	return nil, nil
}
func (f *FakeCommand) Output() ([]byte, error) {
	if f.OutputFunc != nil {
		return f.OutputFunc()
	}
	return nil, nil
}
