package run

import (
	"keyop/core"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// YAML representation of services in the config file
type serviceConfigYaml struct {
	Name string                  `yaml:"name"`
	Freq string                  `yaml:"freq"`
	X    string                  `yaml:"x"`
	Pubs map[string]eventPubYaml `yaml:"pubs"`
}

type eventPubYaml struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func configFilePath() string {
	// read config.yaml from the current working directory
	return filepath.Join(".", "config.yaml")
}

// loadServices reads config.yaml and creates ServiceConfig objects
func loadServices(deps core.Dependencies) ([]core.ServiceConfig, error) {
	p := configFilePath()
	deps.Logger.Info("Loading service config", "path", p)
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
		dur, err := time.ParseDuration(serviceConfigSource.Freq)
		if err != nil {
			return nil, err
		}

		pubs := make(map[string]core.ChannelInfo)
		for key, value := range serviceConfigSource.Pubs {
			pubs[key] = core.ChannelInfo{
				Name:        value.Name,
				Description: value.Description,
			}
		}

		svcConfig := core.ServiceConfig{
			Name: serviceConfigSource.Name,
			Freq: dur,
			Type: serviceConfigSource.X,
			Pubs: pubs,
		}
		deps.Logger.Info("Loaded service config", "config", svcConfig)

		serviceConfigs = append(serviceConfigs, svcConfig)
	}
	return serviceConfigs, nil
}
