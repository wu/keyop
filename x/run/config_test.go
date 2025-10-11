package run

import (
	"keyop/core"
	"log/slog"
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
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)

	_, err := loadServices(deps)
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

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)

	svcs, err := loadServices(deps)
	assert.NoError(t, err)
	if assert.Len(t, svcs, 2) {
		// first check assertions
		assert.Equal(t, "heartbeat", svcs[0].Name)
		assert.Equal(t, "heartbeat", svcs[0].Type)
		assert.Equal(t, 1*time.Second, svcs[0].Freq)
		assert.NotNil(t, ServiceRegistry[svcs[0].Type])
		// second check assertions
		assert.Equal(t, "office-temp", svcs[1].Name)
		assert.Equal(t, "temp", svcs[1].Type)
		assert.Equal(t, 2*time.Second, svcs[1].Freq)
		assert.NotNil(t, ServiceRegistry[svcs[0].Type])
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

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)

	_, err := loadServices(deps)
	assert.Error(t, err, "expected error for invalid duration in config")
}

func Test_loadServices_pubs_loaded(t *testing.T) {
	// setup config with pubs structure similar to sample
	dir := t.TempDir()
	cfg := "- name: heartbeat\n" +
		"  freq: 1s\n" +
		"  x: heartbeat\n" +
		"  pubs:\n" +
		"    events:\n" +
		"      name: heartbeat\n" +
		"      description: Publish Heartbeat Events every 1 second\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	oldWD, _ := os.Getwd()
	//goland:noinspection GoUnhandledErrorResult
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)

	svcs, err := loadServices(deps)
	assert.NoError(t, err)
	if assert.Len(t, svcs, 1) {
		assert.Equal(t, "heartbeat", svcs[0].Pubs["events"].Name)
		assert.Equal(t, "Publish Heartbeat Events every 1 second", svcs[0].Pubs["events"].Description)
	}
}
