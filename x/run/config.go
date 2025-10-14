package run

import (
	"fmt"
	"keyop/core"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// YAML representation of services in the config file
type serviceConfigYaml struct {
	Name   string                      `yaml:"name"`
	Freq   string                      `yaml:"freq"`
	X      string                      `yaml:"x"`
	Pubs   map[string]eventChannelYaml `yaml:"pubs"`
	Subs   map[string]eventChannelYaml `yaml:"subs"`
	Config map[string]interface{}      `yaml:"config,omitempty"`
}

type eventChannelYaml struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func configFilePath() string {
	// read config.yaml from the current working directory
	return filepath.Join(".", "config.yaml")
}

// loadServiceConfigs reads config.yaml and creates ServiceConfig objects
func loadServiceConfigs(deps core.Dependencies) ([]core.ServiceConfig, error) {
	p := configFilePath()
	logger := deps.MustGetLogger()
	logger.Info("Loading service config", "path", p)
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}

	var serviceConfigsSource []serviceConfigYaml
	if err := yaml.Unmarshal(b, &serviceConfigsSource); err != nil {
		return nil, err
	}

	var serviceConfigs []core.ServiceConfig
	for _, serviceConfigSource := range serviceConfigsSource {

		pubs := make(map[string]core.ChannelInfo)
		for key, value := range serviceConfigSource.Pubs {
			pubs[key] = core.ChannelInfo{
				Name:        value.Name,
				Description: value.Description,
			}
		}

		subs := make(map[string]core.ChannelInfo)
		for key, value := range serviceConfigSource.Subs {
			subs[key] = core.ChannelInfo{
				Name:        value.Name,
				Description: value.Description,
			}
		}

		svcConfig := core.ServiceConfig{
			Name:   serviceConfigSource.Name,
			Type:   serviceConfigSource.X,
			Pubs:   pubs,
			Subs:   subs,
			Config: serviceConfigSource.Config,
		}

		if serviceConfigSource.Freq != "" {
			dur, err := time.ParseDuration(serviceConfigSource.Freq)
			if err != nil {
				return nil, err
			}

			svcConfig.Freq = dur
		}
		logger.Info("Loaded service config", "config", svcConfig)

		serviceConfigs = append(serviceConfigs, svcConfig)
	}

	if len(serviceConfigs) == 0 {
		logger.Error("config load", "error", "no services configured")
		return serviceConfigs, fmt.Errorf("no services configured")
	}

	return serviceConfigs, nil
}
