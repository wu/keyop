package core

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Compile-time checks that implementations satisfy the interface
var _ OsProviderApi = OsProvider{}
var _ OsProviderApi = FakeOsProvider{}

func TestOsProvider_Hostname_MatchesStdlib(t *testing.T) {
	want, wantErr := os.Hostname()

	got, err := (OsProvider{}).Hostname()

	// Both should succeed or fail the same way; in practice stdlib shouldn't error
	if wantErr != nil {
		assert.Error(t, err)
		return
	}

	assert.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestFakeOsProvider_Hostname_ReturnsProvidedHost(t *testing.T) {
	f := FakeOsProvider{Host: "example-host"}
	got, err := f.Hostname()
	assert.NoError(t, err)
	assert.Equal(t, "example-host", got)
}

func TestFakeOsProvider_Hostname_PropagatesError(t *testing.T) {
	testErr := assert.AnError
	f := FakeOsProvider{Host: "ignored-host", Err: testErr}
	got, err := f.Hostname()
	assert.ErrorIs(t, err, testErr)
	// ensure host value is returned alongside error (documented behavior in type)
	assert.Equal(t, "ignored-host", got)
}

func TestOsProvider_OpenFile(t *testing.T) {
	provider := OsProvider{}
	tmpFile := t.TempDir() + "/testfile"

	file, err := provider.OpenFile(tmpFile, os.O_CREATE|os.O_RDWR, 0644)
	assert.NoError(t, err)
	assert.NotNil(t, file)

	n, err := file.WriteString("hello")
	assert.NoError(t, err)
	assert.Equal(t, 5, n)

	err = file.Close()
	assert.NoError(t, err)

	// Clean up happens automatically with TempDir
}

func TestOsProvider_MkdirAll(t *testing.T) {
	provider := OsProvider{}
	tmpDir := t.TempDir() + "/a/b/c"

	err := provider.MkdirAll(tmpDir, 0755)
	assert.NoError(t, err)

	info, err := os.Stat(tmpDir)
	assert.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestFakeOsProvider_OpenFile(t *testing.T) {
	t.Run("default behavior", func(t *testing.T) {
		f := FakeOsProvider{}
		file, err := f.OpenFile("any", os.O_RDONLY, 0)
		assert.ErrorIs(t, err, os.ErrNotExist)
		assert.Nil(t, file)
	})

	t.Run("custom behavior", func(t *testing.T) {
		testErr := assert.AnError
		f := FakeOsProvider{
			OpenFileFunc: func(name string, flag int, perm os.FileMode) (FileApi, error) {
				assert.Equal(t, "testfile", name)
				return nil, testErr
			},
		}
		file, err := f.OpenFile("testfile", os.O_RDONLY, 0)
		assert.ErrorIs(t, err, testErr)
		assert.Nil(t, file)
	})
}

func TestFakeOsProvider_MkdirAll(t *testing.T) {
	t.Run("default behavior", func(t *testing.T) {
		f := FakeOsProvider{}
		err := f.MkdirAll("any", 0)
		assert.NoError(t, err)
	})

	t.Run("custom behavior", func(t *testing.T) {
		testErr := assert.AnError
		f := FakeOsProvider{
			MkdirAllFunc: func(path string, perm os.FileMode) error {
				assert.Equal(t, "testdir", path)
				return testErr
			},
		}
		err := f.MkdirAll("testdir", 0)
		assert.ErrorIs(t, err, testErr)
	})
}
