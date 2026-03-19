package run

import (
	"fmt"
	"keyop/core"
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

// LoadPlugins reads the plugins configuration, loads enabled plugins, and registers their services and payload types.
func LoadPlugins(deps core.Dependencies) error {
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
				// Improved error message: list the directory where the plugin was expected and include hints
				pluginDir := filepath.Dir(p.Path)
				files := []os.DirEntry{}
				if dirEntries, readErr := os.ReadDir(pluginDir); readErr == nil {
					files = dirEntries
				}
				logger.Error("Plugin file not found, skipping", "name", p.Name, "path", p.Path, "dir_listing", files)
				logger.Error("Hint: ensure the plugin .so exists at the absolute path above (container paths: /root/.keyop or /.keyop)")
				continue
			}
			return fmt.Errorf("error stating plugin file %s: %w", p.Path, statErr)
		}
		if err := loadPlugin(p, deps); err != nil {
			logger.Error("Failed to load plugin", "name", p.Name, "error", err)
			return err
		}
	}

	// Log registered payload types after all plugins have registered
	reg := deps.MustGetMessenger().GetPayloadRegistry()
	if reg != nil {
		types := reg.KnownTypes()
		logger.Info("Payload registration complete", "total_types", len(types), "types", types)
	}

	return nil
}

func loadPlugin(info PluginInfo, deps core.Dependencies) error {
	p, err := plugin.Open(info.Path)
	if err != nil {
		return fmt.Errorf("could not open plugin: %w", err)
	}

	// 1. Try to register payloads if it's a PayloadProvider
	// We need a way to get the plugin instance.
	// Common pattern is a 'GetPlugin' or looking up the service constructor and checking if it's a plugin.
	// However, usually we lookup a symbol that returns the plugin interface.

	// Let's see if there's a "Plugin" symbol or if NewService returns something that implements PayloadProvider.
	// Given the current structure, we register the constructor in ServiceRegistry.

	symbol, err := p.Lookup("NewService")
	if err != nil {
		return fmt.Errorf("could not find NewService symbol: %w", err)
	}

	newServiceFunc, ok := symbol.(func(core.Dependencies, core.ServiceConfig) core.Service)
	if !ok {
		return fmt.Errorf("NewService has wrong signature")
	}

	// To register payloads, we need an instance of the plugin.
	// Since NewService might be called multiple times for different service instances,
	// we should ideally have a one-time registration.
	// Let's try to invoke NewService with a dummy config to see if it implements PayloadProvider.
	dummySvc := newServiceFunc(deps, core.ServiceConfig{Name: "discovery-" + info.Name})
	if rtPlugin, ok := dummySvc.(core.PayloadProvider); ok {
		reg := deps.MustGetMessenger().GetPayloadRegistry()
		if reg != nil {
			if err := rtPlugin.RegisterPayloads(reg); err != nil {
				deps.MustGetLogger().Error("Plugin failed payload registration", "plugin", info.Name, "error", err)
				// We decide here to fail the plugin load if payload registration fails,
				// unless it's a duplicate registration error which might happen during re-initialization.
				if !core.IsDuplicatePayloadRegistration(err) {
					return fmt.Errorf("payload registration failed for plugin %s: %w", info.Name, err)
				}
			} else {
				deps.MustGetLogger().Info("Plugin registered payloads", "plugin", info.Name)
			}
		}
	}

	ServiceRegistry[info.Name] = newServiceFunc
	deps.MustGetLogger().Info("Registered plugin service", "name", info.Name)

	return nil
}
