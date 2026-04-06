package run

import (
	"fmt"
	"keyop/core"
	"os"
	"path/filepath"
	"strings"

	km "github.com/wu/keyop-messenger"
	"gopkg.in/yaml.v3"
)

// messengerFileConfig is the structure of messenger.yaml.
// It embeds the keyop-messenger Config (all fields inline) and adds a keyop-specific
// bridge section that controls which channels are mirrored to the old messenger.
type messengerFileConfig struct {
	km.Config `yaml:",inline"`
	Bridge    messengerBridgeConfig `yaml:"bridge"`
}

type messengerBridgeConfig struct {
	// Channels is the list of channel names to mirror from the new messenger to
	// the old messenger. Remove a channel once all its publishers and subscribers
	// have been migrated to the new messenger.
	Channels []string `yaml:"channels"`
}

// initNewMessenger looks for messenger.yaml in the keyop conf directory.
// If the file is absent the function returns (nil, nil, nil) and the caller
// should proceed with the old messenger only.
//
// When the file is present it:
//  1. Parses and validates the keyop-messenger config
//  2. Expands ~ in storage.data_dir
//  3. Creates and starts a *km.Messenger
//  4. Registers all canonical core payload types with the new messenger
//  5. Creates a MessengerBridge for the configured channels
//
// The caller is responsible for calling messenger.Close() when the context is done,
// and for calling bridge.Start(ctx).
func initNewMessenger(deps core.Dependencies) (*km.Messenger, *core.MessengerBridge, error) {
	logger := deps.MustGetLogger()

	cfgPath := filepath.Join(configDirPath(), "messenger.yaml")
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		logger.Info("messenger.yaml not found; new messenger disabled", "path", cfgPath)
		return nil, nil, nil
	}

	raw, err := os.ReadFile(cfgPath) //nolint:gosec
	if err != nil {
		return nil, nil, fmt.Errorf("read messenger.yaml: %w", err)
	}

	var fileCfg messengerFileConfig
	if err := yaml.Unmarshal(raw, &fileCfg); err != nil {
		return nil, nil, fmt.Errorf("parse messenger.yaml: %w", err)
	}

	// Expand ~ in data_dir.
	fileCfg.Storage.DataDir = expandHome(fileCfg.Storage.DataDir)
	if fileCfg.Storage.DataDir == "" {
		home, _ := os.UserHomeDir()
		fileCfg.Storage.DataDir = filepath.Join(home, ".keyop", "msgs")
		logger.Info("messenger.yaml: storage.data_dir not set; using default", "dir", fileCfg.Storage.DataDir)
	}

	cfg := &fileCfg.Config
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, nil, fmt.Errorf("invalid messenger.yaml: %w", err)
	}

	m, err := km.New(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("create new messenger: %w", err)
	}

	if err := registerCorePayloadTypes(m, logger); err != nil {
		_ = m.Close()
		return nil, nil, err
	}

	logger.Info("New messenger started",
		"data_dir", cfg.Storage.DataDir,
		"bridge_channels", len(fileCfg.Bridge.Channels),
	)

	var bridge *core.MessengerBridge
	if len(fileCfg.Bridge.Channels) > 0 {
		bridge = core.NewMessengerBridge(deps.MustGetMessenger(), m, fileCfg.Bridge.Channels, logger)
		logger.Info("Messenger bridge configured", "channels", fileCfg.Bridge.Channels)
	}

	return m, bridge, nil
}

// registerCorePayloadTypes registers all canonical core payload types with the
// new messenger so that the bridge and migrated services can decode them.
func registerCorePayloadTypes(m *km.Messenger, _ core.Logger) error {
	types := []struct {
		name  string
		proto any
	}{
		{"core.metric.v1", &core.MetricEvent{}},
		{"core.alert.v1", &core.AlertEvent{}},
		{"core.status.v1", &core.StatusEvent{}},
		{"core.error.v1", &core.ErrorEvent{}},
		{"core.temp.v1", &core.TempEvent{}},
		{"core.device.status.v1", &core.DeviceStatusEvent{}},
		{"core.switch.v1", &core.SwitchEvent{}},
		{"core.switch.command.v1", &core.SwitchCommand{}},
		{"weatherstation.event.v1", &core.WeatherStationEvent{}},
	}
	for _, t := range types {
		if err := m.RegisterPayloadType(t.name, t.proto); err != nil {
			return fmt.Errorf("register payload type %q: %w", t.name, err)
		}
	}
	return nil
}

// expandHome replaces a leading ~ with the current user's home directory.
func expandHome(path string) string {
	if path == "" {
		return path
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
