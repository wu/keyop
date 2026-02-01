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

func TestOsProvider_ReadDir(t *testing.T) {
	provider := OsProvider{}
	tmpDir := t.TempDir()

	err := os.WriteFile(tmpDir+"/file1", []byte("1"), 0644)
	assert.NoError(t, err)
	err = os.WriteFile(tmpDir+"/file2", []byte("2"), 0644)
	assert.NoError(t, err)

	entries, err := provider.ReadDir(tmpDir)
	assert.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestOsProvider_Stat(t *testing.T) {
	provider := OsProvider{}
	tmpFile := t.TempDir() + "/testfile"
	_ = os.WriteFile(tmpFile, []byte("test"), 0644)

	info, err := provider.Stat(tmpFile)
	assert.NoError(t, err)
	assert.Equal(t, "testfile", info.Name())
}

func TestOsProvider_Remove(t *testing.T) {
	provider := OsProvider{}
	tmpFile := t.TempDir() + "/testfile"
	_ = os.WriteFile(tmpFile, []byte("test"), 0644)

	err := provider.Remove(tmpFile)
	assert.NoError(t, err)

	_, err = os.Stat(tmpFile)
	assert.True(t, os.IsNotExist(err))
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

	t.Run("provided file", func(t *testing.T) {
		mockFile := &FakeFile{}
		f := FakeOsProvider{
			File: mockFile,
		}
		file, err := f.OpenFile("any", os.O_RDONLY, 0)
		assert.NoError(t, err)
		assert.Equal(t, mockFile, file)
	})
}

func TestFakeOsProvider_ReadDir(t *testing.T) {
	t.Run("default behavior", func(t *testing.T) {
		f := FakeOsProvider{}
		entries, err := f.ReadDir("any")
		assert.NoError(t, err)
		assert.Nil(t, entries)
	})

	t.Run("custom behavior", func(t *testing.T) {
		testErr := assert.AnError
		f := FakeOsProvider{
			ReadDirFunc: func(dirname string) ([]os.DirEntry, error) {
				assert.Equal(t, "testdir", dirname)
				return nil, testErr
			},
		}
		entries, err := f.ReadDir("testdir")
		assert.ErrorIs(t, err, testErr)
		assert.Nil(t, entries)
	})
}

func TestFakeOsProvider_Stat(t *testing.T) {
	t.Run("default behavior", func(t *testing.T) {
		f := FakeOsProvider{}
		info, err := f.Stat("any")
		assert.ErrorIs(t, err, os.ErrNotExist)
		assert.Nil(t, info)
	})

	t.Run("custom behavior", func(t *testing.T) {
		testErr := assert.AnError
		f := FakeOsProvider{
			StatFunc: func(name string) (os.FileInfo, error) {
				assert.Equal(t, "testfile", name)
				return nil, testErr
			},
		}
		info, err := f.Stat("testfile")
		assert.ErrorIs(t, err, testErr)
		assert.Nil(t, info)
	})
}

func TestFakeOsProvider_Remove(t *testing.T) {
	t.Run("default behavior", func(t *testing.T) {
		f := FakeOsProvider{}
		err := f.Remove("any")
		assert.NoError(t, err)
	})

	t.Run("custom behavior", func(t *testing.T) {
		testErr := assert.AnError
		f := FakeOsProvider{
			RemoveFunc: func(name string) error {
				assert.Equal(t, "testfile", name)
				return testErr
			},
		}
		err := f.Remove("testfile")
		assert.ErrorIs(t, err, testErr)
	})
}

func TestFakeFile(t *testing.T) {
	t.Run("Close", func(t *testing.T) {
		closed := false
		f := &FakeFile{
			CloseFunc: func() error {
				closed = true
				return nil
			},
		}
		err := f.Close()
		assert.NoError(t, err)
		assert.True(t, closed)

		f2 := &FakeFile{}
		err = f2.Close()
		assert.NoError(t, err)
	})

	t.Run("WriteString", func(t *testing.T) {
		// We need something that implements io.ReadWriteSeeker
		// A simple way is to use a custom implementation or just a buffer if it satisfies it
		// But FakeFile expects io.ReadWriteSeeker which bytes.Buffer doesn't fully (it lacks Seek)

		// Let's use a temporary file to provide a real ReadWriteSeeker for the test
		tmp, _ := os.CreateTemp("", "fakefiletest")
		defer os.Remove(tmp.Name())
		defer tmp.Close()

		f := &FakeFile{
			ReadWriteSeeker: tmp,
		}
		n, err := f.WriteString("test")
		assert.NoError(t, err)
		assert.Equal(t, 4, n)

		_, _ = tmp.Seek(0, 0)
		content, _ := os.ReadFile(tmp.Name())
		assert.Equal(t, "test", string(content))
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
