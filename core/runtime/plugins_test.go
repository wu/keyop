package runtime

import (
	"github.com/wu/keyop/core"
	"github.com/wu/keyop/core/testutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginConfigPath_DefaultPath(t *testing.T) {
	// Unset the env var to use default
	t.Setenv("KEYOP_CONF_DIR", "")

	path := pluginConfigPath()
	assert.NotEmpty(t, path)
	assert.True(t, filepath.IsAbs(path) || filepath.IsLocal(path))
	assert.Contains(t, path, "plugins.yaml")
}

func TestPluginConfigPath_CustomEnvVar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	path := pluginConfigPath()
	expected := filepath.Join(dir, "plugins.yaml")
	assert.Equal(t, expected, path)
}

func TestLoadPlugins_ConfigNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	deps := core.Dependencies{}
	logger := &testutil.FakeLogger{}
	deps.SetLogger(logger)

	// Don't create plugins.yaml - should return nil (graceful)
	err := LoadPlugins(deps)
	assert.NoError(t, err)
}

func TestLoadPlugins_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	// Create invalid YAML
	pluginsPath := filepath.Join(dir, "plugins.yaml")
	err := os.WriteFile(pluginsPath, []byte("invalid: yaml: ["), 0o600)
	require.NoError(t, err)

	deps := core.Dependencies{}
	logger := &testutil.FakeLogger{}
	deps.SetLogger(logger)

	err = LoadPlugins(deps)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error unmarshaling plugins config")
}

func TestLoadPlugins_DisabledPlugin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	// Create plugins.yaml with disabled plugin
	pluginsYAML := `
plugins:
  - name: disabled_plugin
    path: /path/to/plugin.so
    enabled: false
`
	pluginsPath := filepath.Join(dir, "plugins.yaml")
	err := os.WriteFile(pluginsPath, []byte(pluginsYAML), 0o600)
	require.NoError(t, err)

	deps := core.Dependencies{}
	logger := &testutil.FakeLogger{}
	deps.SetLogger(logger)

	// Should skip disabled plugin without error
	err = LoadPlugins(deps)
	assert.NoError(t, err)
}

func TestLoadPlugins_EmptyConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	// Create empty plugins.yaml
	pluginsYAML := `
plugins: []
`
	pluginsPath := filepath.Join(dir, "plugins.yaml")
	err := os.WriteFile(pluginsPath, []byte(pluginsYAML), 0o600)
	require.NoError(t, err)

	deps := core.Dependencies{}
	logger := &testutil.FakeLogger{}
	deps.SetLogger(logger)

	err = LoadPlugins(deps)
	assert.NoError(t, err)
}

func TestLoadPlugins_MissingPlugin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)

	// Create plugins.yaml with non-existent plugin
	pluginsYAML := `
plugins:
  - name: missing_plugin
    path: /nonexistent/path/plugin.so
    enabled: true
`
	pluginsPath := filepath.Join(dir, "plugins.yaml")
	err := os.WriteFile(pluginsPath, []byte(pluginsYAML), 0o600)
	require.NoError(t, err)

	deps := core.Dependencies{}
	logger := &testutil.FakeLogger{}
	deps.SetLogger(logger)

	// Should log error and continue (skip plugin)
	err = LoadPlugins(deps)
	assert.NoError(t, err) // LoadPlugins doesn't fail on missing plugins, just logs
}
