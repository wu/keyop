package logManager

import (
	"compress/gzip"
	"fmt"
	"io"
	"keyop/core"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestService_Check(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "logManagerTest")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.OsProvider{})

	cfg := core.ServiceConfig{
		Config: map[string]interface{}{
			"dataDir": tmpDir,
		},
	}
	svc := NewService(deps, cfg)

	// Create a new log file (should NOT be gzipped)
	newFile := filepath.Join(tmpDir, "new.log")
	err = os.WriteFile(newFile, []byte("new log content"), 0644)
	assert.NoError(t, err)

	// Create an old log file (should BE gzipped)
	oldFile := filepath.Join(tmpDir, "old.log")
	err = os.WriteFile(oldFile, []byte("old log content"), 0644)
	assert.NoError(t, err)

	// Set modification time to 50 hours ago
	oldTime := time.Now().Add(-50 * time.Hour)
	err = os.Chtimes(oldFile, oldTime, oldTime)
	assert.NoError(t, err)

	// Run Check
	err = svc.Check()
	assert.NoError(t, err)

	// Verify new file still exists and is not gzipped
	_, err = os.Stat(newFile)
	assert.NoError(t, err)
	_, err = os.Stat(newFile + ".gz")
	assert.True(t, os.IsNotExist(err))

	// Verify old file is gzipped and original is removed
	_, err = os.Stat(oldFile)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(oldFile + ".gz")
	assert.NoError(t, err)

	// Verify content of gzipped file
	gzInfo, err := os.Stat(oldFile + ".gz")
	assert.NoError(t, err)
	// Truncate to seconds because some filesystems or OS providers might have precision differences
	assert.Equal(t, oldTime.Truncate(time.Second), gzInfo.ModTime().Truncate(time.Second))

	f, err := os.Open(oldFile + ".gz")
	assert.NoError(t, err)
	defer f.Close()
	gz, err := gzip.NewReader(f)
	assert.NoError(t, err)
	defer gz.Close()
	content, err := io.ReadAll(gz)
	assert.NoError(t, err)
	assert.Equal(t, "old log content", string(content))
	assert.Equal(t, oldTime.Truncate(time.Second), gz.ModTime.Truncate(time.Second))
}

func TestService_Lifecycle(t *testing.T) {
	deps := core.Dependencies{}
	cfg := core.ServiceConfig{}
	svc := NewService(deps, cfg)

	assert.Nil(t, svc.ValidateConfig())
	assert.Nil(t, svc.Initialize())
}

func TestService_Check_Errors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("ReadDir failure", func(t *testing.T) {
		deps := core.Dependencies{}
		deps.SetLogger(logger)
		deps.SetOsProvider(core.FakeOsProvider{
			ReadDirFunc: func(dirname string) ([]os.DirEntry, error) {
				return nil, fmt.Errorf("read error")
			},
		})
		svc := NewService(deps, core.ServiceConfig{})
		err := svc.Check()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read data directory")
	})

	t.Run("Data directory does not exist", func(t *testing.T) {
		deps := core.Dependencies{}
		deps.SetLogger(logger)
		deps.SetOsProvider(core.FakeOsProvider{
			ReadDirFunc: func(dirname string) ([]os.DirEntry, error) {
				return nil, os.ErrNotExist
			},
		})
		svc := NewService(deps, core.ServiceConfig{})
		err := svc.Check()
		assert.NoError(t, err)
	})

}

func TestService_Check_Filtering(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tmpDir := t.TempDir()

	// Subdirectory (should be skipped)
	err := os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)
	assert.NoError(t, err)

	// Non-log file (should be skipped)
	err = os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content"), 0644)
	assert.NoError(t, err)

	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.OsProvider{})
	svc := NewService(deps, core.ServiceConfig{
		Config: map[string]interface{}{"dataDir": tmpDir},
	})

	err = svc.Check()
	assert.NoError(t, err)

	// Verify no .gz files created
	files, _ := os.ReadDir(tmpDir)
	for _, f := range files {
		assert.False(t, strings.HasSuffix(f.Name(), ".gz"))
	}
}

func TestService_gzipFile_Errors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("Open source failure", func(t *testing.T) {
		deps := core.Dependencies{}
		deps.SetLogger(logger)
		deps.SetOsProvider(core.FakeOsProvider{
			OpenFileFunc: func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
				return nil, fmt.Errorf("open src error")
			},
		})
		svc := Service{Deps: deps}
		err := svc.gzipFile("src", time.Now())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "open src error")
	})

	t.Run("Open destination failure", func(t *testing.T) {
		deps := core.Dependencies{}
		deps.SetLogger(logger)
		deps.SetOsProvider(core.FakeOsProvider{
			OpenFileFunc: func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
				if strings.HasSuffix(name, ".gz") {
					return nil, fmt.Errorf("open dest error")
				}
				return &core.FakeFile{}, nil
			},
		})
		svc := Service{Deps: deps}
		err := svc.gzipFile("src", time.Now())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "open dest error")
	})
}

func TestService_Check_CustomMaxAge(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "logManagerTestCustom")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.OsProvider{})

	// Set maxAge to 10 hours
	cfg := core.ServiceConfig{
		Config: map[string]interface{}{
			"dataDir": tmpDir,
			"maxAge":  "10h",
		},
	}
	svc := NewService(deps, cfg)

	// Create a log file that is 12 hours old (should BE gzipped because it > 10h)
	oldFile := filepath.Join(tmpDir, "old_12h.log")
	err = os.WriteFile(oldFile, []byte("12h old content"), 0644)
	assert.NoError(t, err)
	oldTime12 := time.Now().Add(-12 * time.Hour)
	err = os.Chtimes(oldFile, oldTime12, oldTime12)
	assert.NoError(t, err)

	// Create a log file that is 8 hours old (should NOT be gzipped because it < 10h)
	newFile := filepath.Join(tmpDir, "new_8h.log")
	err = os.WriteFile(newFile, []byte("8h old content"), 0644)
	assert.NoError(t, err)
	oldTime8 := time.Now().Add(-8 * time.Hour)
	err = os.Chtimes(newFile, oldTime8, oldTime8)
	assert.NoError(t, err)

	// Run Check
	err = svc.Check()
	assert.NoError(t, err)

	// Verify 12h old file is gzipped
	_, err = os.Stat(oldFile)
	assert.True(t, os.IsNotExist(err), "12h old file should have been removed")
	_, err = os.Stat(oldFile + ".gz")
	assert.NoError(t, err, "12h old file should have been gzipped")

	// Verify 8h old file still exists
	_, err = os.Stat(newFile)
	assert.NoError(t, err, "8h old file should still exist")
	_, err = os.Stat(newFile + ".gz")
	assert.True(t, os.IsNotExist(err), "8h old file should NOT have been gzipped")
}

func TestService_Check_InvalidMaxAge(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "logManagerTestInvalid")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.OsProvider{})

	// Set invalid maxAge, should default to 48h
	cfg := core.ServiceConfig{
		Config: map[string]interface{}{
			"dataDir": tmpDir,
			"maxAge":  "invalid",
		},
	}
	svc := NewService(deps, cfg)

	// Create a log file that is 50 hours old (should BE gzipped by default 48h)
	oldFile := filepath.Join(tmpDir, "old_50h.log")
	err = os.WriteFile(oldFile, []byte("50h old content"), 0644)
	assert.NoError(t, err)
	oldTime50 := time.Now().Add(-50 * time.Hour)
	err = os.Chtimes(oldFile, oldTime50, oldTime50)
	assert.NoError(t, err)

	// Run Check
	err = svc.Check()
	assert.NoError(t, err)

	// Verify 50h old file is gzipped
	_, err = os.Stat(oldFile + ".gz")
	assert.NoError(t, err, "50h old file should have been gzipped by default threshold")
}
