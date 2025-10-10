package run

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// YAML representation of checks in the config file
type checkYAML struct {
	Name string `yaml:"name"`
	Freq string `yaml:"freq"`
	X    string `yaml:"x"`
}

func configFilePath() string {
	// read config.yaml from the current working directory
	return filepath.Join(".", "config.yaml")
}

// loadChecks reads config.yaml and creates Check objects
func loadChecks() ([]Check, error) {
	p := configFilePath()
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}

	var fileCfg []checkYAML
	if err := yaml.Unmarshal(b, &fileCfg); err != nil {
		return nil, err
	}

	var checks []Check
	for _, yc := range fileCfg {
		dur, err := time.ParseDuration(yc.Freq)
		if err != nil {
			return nil, err
		}
		chk := Check{
			Name:    yc.Name,
			Freq:    dur,
			X:       yc.X,
			NewFunc: ServiceRegistry[yc.X],
		}
		checks = append(checks, chk)
	}
	return checks, nil
}
