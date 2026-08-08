package runtime

import (
	"context"
	"fmt"
	"github.com/wu/keyop/core"
	"os"
	"path/filepath"
	"plugin"

	yaml "gopkg.in/yaml.v3"
)

// PluginsConfig describes the plugins.yaml structure: a list of plugins, their paths, and per-plugin config overrides.
type PluginsConfig struct {
	Plugins []PluginInfo `yaml:"plugins"`
}

// PluginInfo contains metadata for a single plugin entry (path, enabled flag, and configuration) as read from plugins.yaml.
type PluginInfo struct {
	Name    string `yaml:"name"`
	Path    string `yaml:"path"`
	Enabled bool   `yaml:"enabled"`
}

func pluginConfigPath() string {
	if dir := os.Getenv("KEYOP_CONF_DIR"); dir != "" {
		return filepath.Join(dir, "plugins.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".keyop", "conf", "plugins.yaml")
	}
	return filepath.Join(home, ".keyop", "conf", "plugins.yaml")
}

// LoadPlugins reads the plugins configuration, loads enabled plugins, and registers
// their services and payload types.
//
// An enabled plugin whose .so is missing is a fatal error. Skipping it would start
// keyop successfully with the plugin's services silently absent — on a display host
// that presents as working software showing nothing, which is worse than not starting.
func LoadPlugins(deps core.Dependencies) error {
	return loadPluginsFrom(deps, false)
}

// LoadPluginsAllowMissing behaves like LoadPlugins but downgrades a missing plugin
// file to a warning. It exists for `validate-config --ignore-unknown`, which validates
// other hosts' config directories on a machine that does not have their .so files.
// Every other caller wants LoadPlugins.
func LoadPluginsAllowMissing(deps core.Dependencies) error {
	return loadPluginsFrom(deps, true)
}

func loadPluginsFrom(deps core.Dependencies, allowMissing bool) error {
	logger := deps.MustGetLogger()
	configPath := pluginConfigPath()

	logger.Info("Loading plugins config", "path", configPath)
	b, err := os.ReadFile(configPath) //nolint:gosec // reading trusted plugins config file
	if err != nil {
		if os.IsNotExist(err) {
			logger.Info("Plugins config not found, skipping plugin loading", "path", configPath)
			return nil
		}
		return fmt.Errorf("error reading plugins config: %w", err)
	}

	var config PluginsConfig
	if err := yaml.Unmarshal(b, &config); err != nil {
		return fmt.Errorf("error unmarshaling plugins config: %w", err)
	}

	for _, p := range config.Plugins {
		if !p.Enabled {
			logger.Info("Plugin disabled, skipping", "name", p.Name)
			continue
		}

		logger.Info("Loading plugin", "name", p.Name, "path", p.Path)
		if _, statErr := os.Stat(p.Path); statErr != nil {
			if os.IsNotExist(statErr) {
				// List the directory where the plugin was expected, to distinguish a
				// wrong path from an absent build.
				pluginDir := filepath.Dir(p.Path)
				files := []os.DirEntry{}
				if dirEntries, readErr := os.ReadDir(pluginDir); readErr == nil {
					files = dirEntries
				}
				if allowMissing {
					logger.Warn("Plugin file not found, skipping", "name", p.Name, "path", p.Path, "dir_listing", files)
					continue
				}
				logger.Error("Plugin file not found", "name", p.Name, "path", p.Path, "dir_listing", files)
				logger.Error("Hint: ensure the plugin .so exists at the absolute path above (container paths: /root/.keyop or /.keyop)")
				return fmt.Errorf("plugin %q is enabled but its file is missing: %s", p.Name, p.Path)
			}
			return fmt.Errorf("error stating plugin file %s: %w", p.Path, statErr)
		}
		if err := loadPlugin(p, deps); err != nil {
			logger.Error("Failed to load plugin", "name", p.Name, "error", err)
			return err
		}
	}

	// Log successful plugin loading
	logger.Info("Plugins loaded successfully")
	return nil
}

func loadPlugin(info PluginInfo, deps core.Dependencies) error {
	logger := deps.MustGetLogger()
	p, err := plugin.Open(info.Path)
	if err != nil {
		return fmt.Errorf("could not open plugin: %w", err)
	}

	// Try to register payloads if the service implements RegisterPayloadTypesProvider.
	// We look up the NewService symbol and check if the returned service instance
	// implements the RegisterPayloadTypesProvider interface.

	symbol, err := p.Lookup("NewService")
	if err != nil {
		return fmt.Errorf("could not find NewService symbol: %w", err)
	}

	newServiceFunc, ok := symbol.(func(core.Dependencies, core.ServiceConfig, context.Context) core.Service)
	if !ok {
		return fmt.Errorf("NewService has wrong signature")
	}

	// To register payloads, we need an instance of the plugin.
	// Since NewService might be called multiple times for different service instances,
	// we should ideally have a one-time registration.
	// Let's try to invoke NewService with a dummy config to see if it implements RegisterPayloadTypesProvider.
	dummySvc := newServiceFunc(deps, core.ServiceConfig{Name: "discovery-" + info.Name}, context.Background())
	// GetMessenger, not MustGetMessenger: validate-config loads plugins without
	// a messenger, and the nil branch below is the intended handling.
	newMsgr := deps.GetMessenger()
	if newMsgr != nil {
		// Call RegisterPayloadTypes if available
		if regFn, ok := dummySvc.(core.RegisterPayloadTypesProvider); ok {
			if err := regFn.RegisterPayloadTypes(newMsgr, logger); err != nil {
				logger.Error("Plugin failed to register payload types", "plugin", info.Name, "error", err)
				if !core.IsDuplicatePayloadRegistration(err) {
					return fmt.Errorf("payload type registration failed for plugin %s: %w", info.Name, err)
				}
			} else {
				logger.Info("Plugin registered payload types", "plugin", info.Name)
			}
		} else {
			logger.Debug("Plugin does not implement RegisterPayloadTypes", "plugin", info.Name)
		}
	} else {
		logger.Warn("new messenger not available; cannot register plugin payloads", "plugin", info.Name)
	}

	// Wrap newServiceFunc to return interface{} instead of core.Service
	core.RegisterService(info.Name, func(deps core.Dependencies, cfg core.ServiceConfig, ctx context.Context) interface{} {
		return newServiceFunc(deps, cfg, ctx)
	})
	logger.Info("Registered plugin service", "name", info.Name)

	return nil
}
