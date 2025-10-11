package core

import "os"

// OsProviderApi is a minimal interface to retrieve the current hostname.
// It allows dependency injection for testing or alternative OS implementations.
type OsProviderApi interface {
	Hostname() (string, error)
}

// OsProvider is the production implementation of OsProviderApi using the standard library.
type OsProvider struct{}

func (OsProvider) Hostname() (string, error) { return os.Hostname() }

// FakeOsProvider is provided for testing
type FakeOsProvider struct {
	Host string
	Err  error
}

func (f FakeOsProvider) Hostname() (string, error) { return f.Host, f.Err }
