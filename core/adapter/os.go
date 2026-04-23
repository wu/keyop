//nolint:revive
package adapter

import (
	"github.com/wu/keyop/core"
	"os"
	"os/exec"
	"time"
)

// OsProvider is the production implementation of core.OsProviderApi using the standard library.
type OsProvider struct{}

func (OsProvider) Hostname() (string, error)    { return os.Hostname() }
func (OsProvider) UserHomeDir() (string, error) { return os.UserHomeDir() }
func (OsProvider) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name) //nolint:gosec // intentional OS wrapper forwarding variable path
}
func (OsProvider) OpenFile(name string, flag int, perm os.FileMode) (core.FileApi, error) {
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
func (OsProvider) Command(name string, arg ...string) core.CommandApi {
	return exec.Command(name, arg...) //nolint:gosec // intentional OS wrapper for executing configured commands
}

// Compile-time check that OsProvider satisfies core.OsProviderApi.
var _ core.OsProviderApi = OsProvider{}
