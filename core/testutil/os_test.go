package testutil

import (
	"github.com/wu/keyop/core"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Compile-time checks that implementations satisfy the interface
var _ core.OsProviderApi = FakeOsProvider{}

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
	assert.Equal(t, "ignored-host", got)
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
			OpenFileFunc: func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
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

	t.Run("error if file not set", func(t *testing.T) {
		f := FakeOsProvider{}
		file, err := f.OpenFile("nonexistent", os.O_RDONLY, 0)
		assert.ErrorIs(t, err, os.ErrNotExist)
		assert.Nil(t, file)
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
		tmp, _ := os.CreateTemp("", "fakefiletest")
		//goland:noinspection GoUnhandledErrorResult
		t.Cleanup(func() {
			if err := os.Remove(tmp.Name()); err != nil { //nolint:gosec // test-only temp file
				t.Logf("failed to remove %s: %v", tmp.Name(), err)
			}
		})
		//goland:noinspection GoUnhandledErrorResult
		t.Cleanup(func() {
			if err := tmp.Close(); err != nil {
				t.Logf("failed to close tmp file: %v", err)
			}
		})

		f := &FakeFile{
			ReadWriteSeeker: tmp,
		}
		n, err := f.WriteString("test")
		assert.NoError(t, err)
		assert.Equal(t, 4, n)

		if _, err := tmp.Seek(0, 0); err != nil {
			t.Fatalf("failed to seek tmp file: %v", err)
		}
		content, err := os.ReadFile(tmp.Name()) //nolint:gosec // test-only temp file
		if err != nil {
			t.Fatalf("failed to read tmp file: %v", err)
		}
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

func TestFakeOsProvider_Chtimes(t *testing.T) {
	t.Run("default behavior", func(t *testing.T) {
		f := FakeOsProvider{}
		err := f.Chtimes("any", time.Now(), time.Now())
		assert.NoError(t, err)
	})

	t.Run("custom behavior", func(t *testing.T) {
		testErr := assert.AnError
		f := FakeOsProvider{
			ChtimesFunc: func(name string, atime time.Time, mtime time.Time) error {
				assert.Equal(t, "testfile", name)
				return testErr
			},
		}
		err := f.Chtimes("testfile", time.Now(), time.Now())
		assert.ErrorIs(t, err, testErr)
	})
}

func TestFakeOsProvider_Command(t *testing.T) {
	t.Run("default behavior", func(t *testing.T) {
		f := FakeOsProvider{}
		cmd := f.Command("any")
		assert.NotNil(t, cmd)
		assert.IsType(t, &FakeCommand{}, cmd)
	})

	t.Run("custom behavior", func(t *testing.T) {
		f := FakeOsProvider{
			CommandFunc: func(name string, arg ...string) core.CommandApi {
				return &FakeCommand{
					RunFunc: func() error {
						return assert.AnError
					},
				}
			},
		}
		cmd := f.Command("test")
		err := cmd.Run()
		assert.ErrorIs(t, err, assert.AnError)
	})
}

func TestFakeCommand(t *testing.T) {
	t.Run("Run", func(t *testing.T) {
		f := &FakeCommand{}
		assert.NoError(t, f.Run())

		f.RunFunc = func() error { return assert.AnError }
		assert.ErrorIs(t, f.Run(), assert.AnError)
	})

	t.Run("CombinedOutput", func(t *testing.T) {
		f := &FakeCommand{}
		out, err := f.CombinedOutput()
		assert.NoError(t, err)
		assert.Nil(t, out)

		f.CombinedOutputFunc = func() ([]byte, error) { return []byte("hello"), nil }
		out, err = f.CombinedOutput()
		assert.NoError(t, err)
		assert.Equal(t, []byte("hello"), out)
	})

	t.Run("Output", func(t *testing.T) {
		f := &FakeCommand{}
		out, err := f.Output()
		assert.NoError(t, err)
		assert.Nil(t, out)

		f.OutputFunc = func() ([]byte, error) { return []byte("world"), nil }
		out, err = f.Output()
		assert.NoError(t, err)
		assert.Equal(t, []byte("world"), out)
	})
}
