package run

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// YAML representation of services in the config file
type serviceConfigYaml struct {
	Name string `yaml:"name"`
	Freq string `yaml:"freq"`
	X    string `yaml:"x"`
}

func configFilePath() string {
	// read config.yaml from the current working directory
	return filepath.Join(".", "config.yaml")
}

// loadServices reads config.yaml and creates ServiceConfig objects
func loadServices() ([]ServiceConfig, error) {
	p := configFilePath()
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}

	var serviceConfigSource []serviceConfigYaml
	if err := yaml.Unmarshal(b, &serviceConfigSource); err != nil {
		return nil, err
	}

	var serviceConfigs []ServiceConfig
	for _, yc := range serviceConfigSource {
		dur, err := time.ParseDuration(yc.Freq)
		if err != nil {
			return nil, err
		}
		svcConfig := ServiceConfig{
			Name:    yc.Name,
			Freq:    dur,
			Type:    yc.X,
			NewFunc: ServiceRegistry[yc.X],
		}
		serviceConfigs = append(serviceConfigs, svcConfig)
	}
	return serviceConfigs, nil
}
