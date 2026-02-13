package run

import (
	"fmt"
	"keyop/core"
	"os"
	"path/filepath"
	"plugin"

	"gopkg.in/yaml.v3"
)

type PluginsConfig struct {
	Plugins []PluginInfo `yaml:"plugins"`
}

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

func LoadPlugins(deps core.Dependencies) error {
	logger := deps.MustGetLogger()
	configPath := pluginConfigPath()

	logger.Info("Loading plugins config", "path", configPath)
	b, err := os.ReadFile(configPath)
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
		if err := loadPlugin(p, deps); err != nil {
			logger.Error("Failed to load plugin", "name", p.Name, "error", err)
			return err
		}
	}

	return nil
}

func loadPlugin(info PluginInfo, deps core.Dependencies) error {
	p, err := plugin.Open(info.Path)
	if err != nil {
		return fmt.Errorf("could not open plugin: %w", err)
	}

	symbol, err := p.Lookup("NewService")
	if err != nil {
		return fmt.Errorf("could not find NewService symbol: %w", err)
	}

	newService, ok := symbol.(func(core.Dependencies, core.ServiceConfig) core.Service)
	if !ok {
		return fmt.Errorf("NewService has wrong signature")
	}

	ServiceRegistry[info.Name] = newService
	deps.MustGetLogger().Info("Registered plugin service", "name", info.Name)

	return nil
}
