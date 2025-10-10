package run

import (
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
	//goland:noinspection GoUnhandledErrorResult
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// load should fail before running
	_, err := loadServices()
	assert.Error(t, err, "expected error when config is missing")
}

func Test_loadServices_success(t *testing.T) {
	// setup: temp dir with a valid config.yaml containing two svcs
	dir := t.TempDir()
	cfg := "- name: heartbeat\n  freq: 1s\n  x: heartbeat\n" +
		"- name: office-temp\n  freq: 2s\n  x: temp\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	oldWD, _ := os.Getwd()
	//goland:noinspection GoUnhandledErrorResult
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	svcs, err := loadServices()
	assert.NoError(t, err)
	if assert.Len(t, svcs, 2) {
		// first check assertions
		assert.Equal(t, "heartbeat", svcs[0].Name)
		assert.Equal(t, "heartbeat", svcs[0].Type)
		assert.Equal(t, 1*time.Second, svcs[0].Freq)
		assert.NotNil(t, svcs[0].NewFunc)

		// second check assertions
		assert.Equal(t, "office-temp", svcs[1].Name)
		assert.Equal(t, "temp", svcs[1].Type)
		assert.Equal(t, 2*time.Second, svcs[1].Freq)
		assert.NotNil(t, svcs[1].NewFunc)
	}
}

func Test_loadServices_bad_duration(t *testing.T) {
	// setup: temp dir with an invalid duration string
	dir := t.TempDir()
	cfg := "- name: heartbeat\n  freq: not-a-duration\n  x: heartbeat\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	oldWD, _ := os.Getwd()
	//goland:noinspection GoUnhandledErrorResult
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	_, err := loadServices()
	assert.Error(t, err, "expected error for invalid duration in config")
}
