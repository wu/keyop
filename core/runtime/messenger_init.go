package runtime

import (
	"fmt"
	"github.com/wu/keyop/core"
	"os"
	"path/filepath"
	"strings"

	km "github.com/wu/keyop-messenger"
	"gopkg.in/yaml.v3"
)

// messengerFileConfig is the structure of messenger.yaml.
// It embeds the keyop-messenger Config (all fields inline).
type messengerFileConfig struct {
	km.Config `yaml:",inline"`
}

// initMessenger looks for messenger.yaml in the keyop conf directory.
// If the file is absent the function returns (nil, nil) and the caller
// should proceed without the messenger.
//
// When the file is present it:
//  1. Parses and validates the keyop-messenger config
//  2. Expands ~ in storage.data_dir
//  3. Creates and starts a *km.Messenger
//  4. Registers all canonical core payload types with the new messenger
//
// The caller is responsible for calling messenger.Close() when the context is done.
func initMessenger(deps core.Dependencies) (*km.Messenger, error) {
	logger := deps.MustGetLogger()

	cfgPath := filepath.Join(configDirPath(), "messenger.yaml")
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		logger.Info("messenger.yaml not found; new messenger disabled", "path", cfgPath)
		return nil, nil
	}

	raw, err := os.ReadFile(cfgPath) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("read messenger.yaml: %w", err)
	}

	var fileCfg messengerFileConfig
	if err := yaml.Unmarshal(raw, &fileCfg); err != nil {
		return nil, fmt.Errorf("parse messenger.yaml: %w", err)
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
		return nil, fmt.Errorf("invalid messenger.yaml: %w", err)
	}

	m, err := km.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("create new messenger: %w", err)
	}

	if err := registerCorePayloadTypes(m, logger); err != nil {
		_ = m.Close()
		return nil, err
	}

	logger.Info("New messenger started",
		"data_dir", cfg.Storage.DataDir,
	)

	return m, nil
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
		{"core.gps.v1", &core.GpsEvent{}},
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
