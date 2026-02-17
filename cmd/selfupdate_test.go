package cmd

import (
	"bytes"
	"compress/gzip"
	"keyop/core"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInstallUpdate(t *testing.T) {
	// Setup
	tmpDir, err := os.MkdirTemp("", "keyop-test-*")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	exePath := filepath.Join(tmpDir, "keyop")
	err = os.WriteFile(exePath, []byte("original binary"), 0644)
	assert.NoError(t, err)

	newContent := "new binary content"
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, err = gw.Write([]byte(newContent))
	assert.NoError(t, err)
	err = gw.Close()
	assert.NoError(t, err)

	gzReader, err := gzip.NewReader(&buf)
	assert.NoError(t, err)

	logger := &core.FakeLogger{}

	// Execute
	err = installUpdate(logger, gzReader, exePath)

	// Assert
	assert.NoError(t, err)

	// Verify content
	content, err := os.ReadFile(exePath)
	assert.NoError(t, err)
	assert.Equal(t, newContent, string(content))

	// Verify permissions (on unix)
	info, err := os.Stat(exePath)
	assert.NoError(t, err)
	assert.Equal(t, os.FileMode(0755), info.Mode().Perm())
}

func TestProgressReader(t *testing.T) {
	data := []byte("hello world")
	buf := bytes.NewReader(data)
	total := int64(len(data))

	var progressCalls []int64
	pr := &progressReader{
		Reader: buf,
		Total:  total,
		OnProgress: func(current, total int64) {
			progressCalls = append(progressCalls, current)
		},
	}

	out := make([]byte, 5)
	n, err := pr.Read(out)
	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, []int64{5}, progressCalls)

	n, err = pr.Read(out)
	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, []int64{5, 10}, progressCalls)

	n, err = pr.Read(out)
	assert.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []int64{5, 10, 11}, progressCalls)
}
