//nolint:revive
package adapter

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestOsProvider_Hostname_MatchesStdlib(t *testing.T) {
	want, wantErr := os.Hostname()

	got, err := (OsProvider{}).Hostname()

	if wantErr != nil {
		assert.Error(t, err)
		return
	}

	assert.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestOsProvider_OpenFile(t *testing.T) {
	provider := OsProvider{}
	tmpFile := t.TempDir() + "/testfile"

	file, err := provider.OpenFile(tmpFile, os.O_CREATE|os.O_RDWR, 0600)
	assert.NoError(t, err)
	assert.NotNil(t, file)

	n, err := file.WriteString("hello")
	assert.NoError(t, err)
	assert.Equal(t, 5, n)

	err = file.Close()
	assert.NoError(t, err)
}

func TestOsProvider_MkdirAll(t *testing.T) {
	provider := OsProvider{}
	tmpDir := t.TempDir() + "/a/b/c"

	err := provider.MkdirAll(tmpDir, 0750)
	assert.NoError(t, err)

	info, err := os.Stat(tmpDir)
	assert.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestOsProvider_ReadDir(t *testing.T) {
	provider := OsProvider{}
	tmpDir := t.TempDir()

	err := os.WriteFile(tmpDir+"/file1", []byte("1"), 0600)
	assert.NoError(t, err)
	err = os.WriteFile(tmpDir+"/file2", []byte("2"), 0600)
	assert.NoError(t, err)

	entries, err := provider.ReadDir(tmpDir)
	assert.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestOsProvider_Stat(t *testing.T) {
	provider := OsProvider{}
	tmpFile := t.TempDir() + "/testfile"
	if err := os.WriteFile(tmpFile, []byte("test"), 0600); err != nil {
		assert.NoError(t, err)
	}
	info, err := provider.Stat(tmpFile)
	assert.NoError(t, err)
	assert.Equal(t, "testfile", info.Name())
}

func TestOsProvider_Remove(t *testing.T) {
	provider := OsProvider{}
	tmpFile := t.TempDir() + "/testfile"
	if err := os.WriteFile(tmpFile, []byte("test"), 0600); err != nil {
		assert.NoError(t, err)
	}
	err := provider.Remove(tmpFile)
	assert.NoError(t, err)

	_, err = os.Stat(tmpFile)
	assert.True(t, os.IsNotExist(err))
}

func TestOsProvider_Chtimes(t *testing.T) {
	provider := OsProvider{}
	tmpFile := t.TempDir() + "/testfile"
	if err := os.WriteFile(tmpFile, []byte("test"), 0600); err != nil {
		assert.NoError(t, err)
	}
	now := time.Now()
	err := provider.Chtimes(tmpFile, now, now)
	assert.NoError(t, err)
}

func TestOsProvider_Command(t *testing.T) {
	provider := OsProvider{}
	cmd := provider.Command("echo", "hello")
	assert.NotNil(t, cmd)

	out, err := cmd.Output()
	assert.NoError(t, err)
	assert.Contains(t, string(out), "hello")
}
