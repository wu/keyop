package core

import "os"

// OsProviderIface is a minimal interface to retrieve the current hostname.
// It allows dependency injection for testing or alternative OS implementations.
type OsProviderIface interface {
	Hostname() (string, error)
}

// OsProvider is the production implementation of OsProviderIface using the standard library.
type OsProvider struct{}

func (OsProvider) Hostname() (string, error) { return os.Hostname() }

// FakeOsProvider is provided for testing
type FakeOsProvider struct {
	Host string
	Err  error
}

func (f FakeOsProvider) Hostname() (string, error) { return f.Host, f.Err }
