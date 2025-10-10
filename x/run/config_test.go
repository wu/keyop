package run

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_run_missing_config_returns_error(t *testing.T) {
	// change to an empty temp directory without config.yaml
	dir := t.TempDir()
	oldWD, _ := os.Getwd()
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// load should fail before running
	_, err := loadChecks()
	assert.Error(t, err, "expected error when config is missing")
}

func Test_loadChecks_success(t *testing.T) {
	// setup: temp dir with a valid config.yaml containing two checks
	dir := t.TempDir()
	cfg := "- name: heartbeat\n  freq: 1s\n  x: heartbeat\n" +
		"- name: office-temp\n  freq: 2s\n  x: temp\n"
	if err := ioutil.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	oldWD, _ := os.Getwd()
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	checks, err := loadChecks()
	assert.NoError(t, err)
	if assert.Len(t, checks, 2) {
		// first check assertions
		assert.Equal(t, "heartbeat", checks[0].Name)
		assert.Equal(t, "heartbeat", checks[0].X)
		assert.Equal(t, 1*time.Second, checks[0].Freq)
		assert.NotNil(t, checks[0].Func)

		// second check assertions
		assert.Equal(t, "office-temp", checks[1].Name)
		assert.Equal(t, "temp", checks[1].X)
		assert.Equal(t, 2*time.Second, checks[1].Freq)
		assert.NotNil(t, checks[1].Func)
	}
}

func Test_loadChecks_bad_duration(t *testing.T) {
	// setup: temp dir with an invalid duration string
	dir := t.TempDir()
	cfg := "- name: heartbeat\n  freq: not-a-duration\n  x: heartbeat\n"
	if err := ioutil.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	oldWD, _ := os.Getwd()
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	_, err := loadChecks()
	assert.Error(t, err, "expected error for invalid duration in config")
}
