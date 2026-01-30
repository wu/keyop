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
}

type FileApi interface {
	io.Closer
	io.Writer
	WriteString(s string) (n int, err error)
}

// OsProvider is the production implementation of OsProviderApi using the standard library.
type OsProvider struct{}

func (OsProvider) Hostname() (string, error) { return os.Hostname() }
func (OsProvider) OpenFile(name string, flag int, perm os.FileMode) (FileApi, error) {
	return os.OpenFile(name, flag, perm)
}

// FakeOsProvider is provided for testing
type FakeOsProvider struct {
	Host string
	Err  error

	OpenFileFunc func(name string, flag int, perm os.FileMode) (FileApi, error)
}

func (f FakeOsProvider) Hostname() (string, error) { return f.Host, f.Err }
func (f FakeOsProvider) OpenFile(name string, flag int, perm os.FileMode) (FileApi, error) {
	if f.OpenFileFunc != nil {
		return f.OpenFileFunc(name, flag, perm)
	}
	return nil, os.ErrNotExist
}
