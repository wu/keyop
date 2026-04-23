package runtime

import (
	"github.com/wu/keyop/core"
	"github.com/wu/keyop/core/adapter"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigDirPath_DefaultPath(t *testing.T) {
	// Unset env var to test default
	t.Setenv("KEYOP_CONF_DIR", "")

	path := configDirPath()
	assert.NotEmpty(t, path)
	assert.Contains(t, path, ".keyop")
	assert.Contains(t, path, "conf")
}

func TestConfigDirPath_CustomEnvVar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	path := configDirPath()
	assert.Equal(t, dir, path)
}

func TestLoadServiceConfigs_WithMultipleServices(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	// Create multiple config files
	configs := []struct {
		name    string
		content string
	}{
		{
			name: "10-heartbeat.yaml",
			content: `
name: heartbeat
freq: 1s
service: heartbeat
pubs:
  events:
    name: heartbeat
`,
		},
		{
			name: "20-monitor.yaml",
			content: `
name: cpu-monitor
freq: 5s
service: cpuMonitor
config:
  threshold: 80
`,
		},
	}

	for _, cfg := range configs {
		path := filepath.Join(dir, cfg.name)
		err := os.WriteFile(path, []byte(cfg.content), 0o600)
		require.NoError(t, err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(adapter.OsProvider{})

	svcs, err := loadServiceConfigs(deps)
	assert.NoError(t, err)
	assert.Len(t, svcs, 2)
	assert.Equal(t, "10-heartbeat", svcs[0].Name)
	assert.Equal(t, "20-monitor", svcs[1].Name)
}

func TestLoadServiceConfigs_InvalidFreqFormat(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	cfg := `
name: test
freq: invalid_duration
service: test
`
	path := filepath.Join(dir, "test.yaml")
	err := os.WriteFile(path, []byte(cfg), 0o600)
	require.NoError(t, err)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(adapter.OsProvider{})

	_, err = loadServiceConfigs(deps)
	assert.Error(t, err)
}

func TestLoadServiceConfigs_SkipsNonYAMLFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	// Create valid YAML
	validCfg := `
name: test
service: test
`
	path := filepath.Join(dir, "test.yaml")
	err := os.WriteFile(path, []byte(validCfg), 0o600)
	require.NoError(t, err)

	// Create non-YAML file
	err = os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0o600)
	require.NoError(t, err)

	// Create directory
	err = os.Mkdir(filepath.Join(dir, "subdir"), 0o755)
	require.NoError(t, err)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(adapter.OsProvider{})

	svcs, err := loadServiceConfigs(deps)
	assert.NoError(t, err)
	assert.Len(t, svcs, 1, "should only load the .yaml file")
}

func TestLoadServiceConfigs_SkipsSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	// Create valid service config
	svcCfg := `name: svc1
service: test`
	err := os.WriteFile(filepath.Join(dir, "svc1.yaml"), []byte(svcCfg), 0o600)
	require.NoError(t, err)

	// Create plugins.yaml (should be skipped)
	err = os.WriteFile(filepath.Join(dir, "plugins.yaml"), []byte("plugins: []"), 0o600)
	require.NoError(t, err)

	// Create messenger.yaml (should be skipped)
	err = os.WriteFile(filepath.Join(dir, "messenger.yaml"), []byte("storage: {}"), 0o600)
	require.NoError(t, err)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(adapter.OsProvider{})

	svcs, err := loadServiceConfigs(deps)
	assert.NoError(t, err)
	assert.Len(t, svcs, 1)
	assert.Equal(t, "svc1", svcs[0].Name)
}

func TestLoadServiceConfigs_MaxAgeInSubs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	cfg := `
name: test
service: test
subs:
  events:
    name: my-events
    max_age: 24h
`
	path := filepath.Join(dir, "test.yaml")
	err := os.WriteFile(path, []byte(cfg), 0o600)
	require.NoError(t, err)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(adapter.OsProvider{})

	svcs, err := loadServiceConfigs(deps)
	assert.NoError(t, err)
	require.Len(t, svcs, 1)
	assert.Equal(t, int64(86400000000000), svcs[0].Subs["events"].MaxAge.Nanoseconds()) // 24h
}

func TestLoadServiceConfigs_InvalidYAMLSyntax(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	// Invalid YAML
	path := filepath.Join(dir, "bad.yaml")
	err := os.WriteFile(path, []byte("{ invalid yaml ]"), 0o600)
	require.NoError(t, err)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(adapter.OsProvider{})

	_, err = loadServiceConfigs(deps)
	assert.Error(t, err)
}

func TestLoadServiceConfigs_ConfigWithArbitraryConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	cfg := `
name: test
service: test
config:
  str_value: hello
  num_value: 42
  bool_value: true
  nested:
    key: value
`
	path := filepath.Join(dir, "test.yaml")
	err := os.WriteFile(path, []byte(cfg), 0o600)
	require.NoError(t, err)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(adapter.OsProvider{})

	svcs, err := loadServiceConfigs(deps)
	assert.NoError(t, err)
	require.Len(t, svcs, 1)

	config := svcs[0].Config
	assert.Equal(t, "hello", config["str_value"])
	// YAML unmarshals to int when there's no decimal, so check both cases
	numVal := config["num_value"]
	assert.True(t, numVal == 42 || numVal == float64(42), "num_value should be 42")
	assert.Equal(t, true, config["bool_value"])
}
