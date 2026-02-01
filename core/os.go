package core

import (
	"io"
	"os"
)

// OsProviderApi is a minimal interface to retrieve the current hostname.
// It allows dependency injection for testing or alternative OS implementations.
type OsProviderApi interface {
	Hostname() (string, error)
	OpenFile(name string, flag int, perm os.FileMode) (FileApi, error)
	MkdirAll(path string, perm os.FileMode) error
	ReadDir(dirname string) ([]os.DirEntry, error)
	Stat(name string) (os.FileInfo, error)
	Remove(name string) error
}

type FileApi interface {
	io.Closer
	io.Writer
	io.Reader
	io.Seeker
	WriteString(s string) (n int, err error)
}

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

// OsProvider is the production implementation of OsProviderApi using the standard library.
type OsProvider struct{}

func (OsProvider) Hostname() (string, error) { return os.Hostname() }
func (OsProvider) OpenFile(name string, flag int, perm os.FileMode) (FileApi, error) {
	f, err := os.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return f, nil
}
func (OsProvider) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}
func (OsProvider) ReadDir(dirname string) ([]os.DirEntry, error) {
	return os.ReadDir(dirname)
}
func (OsProvider) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}
func (OsProvider) Remove(name string) error {
	return os.Remove(name)
}

// FakeOsProvider is provided for testing
type FakeOsProvider struct {
	Host string
	Err  error

	OpenFileFunc func(name string, flag int, perm os.FileMode) (FileApi, error)
	MkdirAllFunc func(path string, perm os.FileMode) error
	ReadDirFunc  func(dirname string) ([]os.DirEntry, error)
	StatFunc     func(name string) (os.FileInfo, error)
	RemoveFunc   func(name string) error

	File FileApi
}

func (f FakeOsProvider) Hostname() (string, error) { return f.Host, f.Err }
func (f FakeOsProvider) OpenFile(name string, flag int, perm os.FileMode) (FileApi, error) {
	if f.OpenFileFunc != nil {
		return f.OpenFileFunc(name, flag, perm)
	}
	if f.File != nil {
		return f.File, nil
	}
	// Default behavior: create a memory-backed file if CREATE flag is set, otherwise return not exist
	return nil, os.ErrNotExist
}
func (f FakeOsProvider) MkdirAll(path string, perm os.FileMode) error {
	if f.MkdirAllFunc != nil {
		return f.MkdirAllFunc(path, perm)
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
func (f FakeOsProvider) Remove(name string) error {
	if f.RemoveFunc != nil {
		return f.RemoveFunc(name)
	}
	return nil
}
