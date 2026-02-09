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
	// directory does not exist
	dir := filepath.Join(t.TempDir(), "non-existent")
	t.Setenv("KEYOP_CONF_DIR", dir)

	// load should fail before running
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.OsProvider{})

	_, err := loadServiceConfigs(deps)
	assert.Error(t, err, "expected error when config directory is missing")
	assert.Contains(t, err.Error(), "config directory does not exist")
}

func Test_loadServices_success(t *testing.T) {
	// setup: temp dir with a valid config.yaml containing two svcs
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	cfg := "- name: heartbeat\n  freq: 1s\n  x: heartbeat\n" +
		"- name: office-temp\n  freq: 2s\n  x: temp\n"
	if err := os.WriteFile(filepath.Join(dir, "10-services.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.OsProvider{})

	svcs, err := loadServiceConfigs(deps)
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

func Test_loadServices_multiple_files_order(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	cfg2 := "- name: service2\n  x: heartbeat\n"
	if err := os.WriteFile(filepath.Join(dir, "20-service.yaml"), []byte(cfg2), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	cfg1 := "- name: service1\n  x: heartbeat\n"
	if err := os.WriteFile(filepath.Join(dir, "10-service.yaml"), []byte(cfg1), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.OsProvider{})

	svcs, err := loadServiceConfigs(deps)
	assert.NoError(t, err)
	if assert.Len(t, svcs, 2) {
		assert.Equal(t, "service1", svcs[0].Name)
		assert.Equal(t, "service2", svcs[1].Name)
	}
}

func Test_loadServices_bad_duration(t *testing.T) {
	// setup: temp dir with an invalid duration string
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	cfg := "- name: heartbeat\n  freq: not-a-duration\n  x: heartbeat\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.OsProvider{})

	_, err := loadServiceConfigs(deps)
	assert.Error(t, err, "expected error for invalid duration in config")
}

func Test_loadServices_pubs_loaded(t *testing.T) {
	// setup config with pubs structure similar to sample
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

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

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.OsProvider{})

	svcs, err := loadServiceConfigs(deps)
	assert.NoError(t, err)
	if assert.Len(t, svcs, 1) {
		assert.Equal(t, "heartbeat", svcs[0].Pubs["events"].Name)
		assert.Equal(t, "Publish Heartbeat Events every 1 second", svcs[0].Pubs["events"].Description)
	}
}

func Test_loadServices_subs_loaded(t *testing.T) {
	// setup config with subs structure similar to sample
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	cfg := "- name: foo\n" +
		"  x: heartbeat\n" +
		"  subs:\n" +
		"    events:\n" +
		"      name: heartbeat\n" +
		"      description: Read Heartbeat Events\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.OsProvider{})

	svcs, err := loadServiceConfigs(deps)
	assert.NoError(t, err)
	if assert.Len(t, svcs, 1) {
		assert.Equal(t, "heartbeat", svcs[0].Subs["events"].Name)
		assert.Equal(t, "Read Heartbeat Events", svcs[0].Subs["events"].Description)
	}
}

func Test_loadServices_maxAge_loaded(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	cfg := "- name: foo\n" +
		"  x: heartbeat\n" +
		"  subs:\n" +
		"    events:\n" +
		"      name: heartbeat\n" +
		"      max_age: 1h\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.OsProvider{})

	svcs, err := loadServiceConfigs(deps)
	assert.NoError(t, err)
	if assert.Len(t, svcs, 1) {
		assert.Equal(t, "heartbeat", svcs[0].Subs["events"].Name)
		assert.Equal(t, 1*time.Hour, svcs[0].Subs["events"].MaxAge)
	}
}

func Test_loadServiceConfigs_no_services_configured(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	cfg := "[]\n" // empty YAML array, valid but no services
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.OsProvider{})

	svcs, err := loadServiceConfigs(deps)
	assert.Error(t, err, "expected error when no services are configured")
	assert.Empty(t, svcs, "expected no services returned")
	if err != nil {
		assert.Contains(t, err.Error(), "no services configured")
	}
}

func Test_loadServiceConfigs_template_hostname(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	cfg := "- name: {{.ShortHostname}}-service\n  x: heartbeat\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.FakeOsProvider{Host: "myhost.example.com"})

	svcs, err := loadServiceConfigs(deps)
	assert.NoError(t, err)
	if assert.Len(t, svcs, 1) {
		assert.Equal(t, "myhost-service", svcs[0].Name)
	}
}

func Test_loadServiceConfigs_template_userhome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	cfg := "- name: home-service\n  x: heartbeat\n  config:\n    home: {{.HomeDir}}\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.FakeOsProvider{Host: "myhost.example.com"})

	svcs, err := loadServiceConfigs(deps)
	assert.NoError(t, err)

	userHome, _ := os.UserHomeDir()

	if assert.Len(t, svcs, 1) {
		assert.Equal(t, userHome, svcs[0].Config["home"])
	}
}
