package run

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// YAML representation of a check in the config file
// kept internal to config parsing
type checkYAML struct {
	Name string `yaml:"name"`
	Freq string `yaml:"freq"`
	X    string `yaml:"x"`
}

func configFilePath() string {
	// keep it simple: put config.yaml next to the running binary (current working dir)
	return filepath.Join(".", "config.yaml")
}

// loadChecks reads config.yaml and converts it into a slice of Check with bound funcs
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
			Name: yc.Name,
			Freq: dur,
			X:    yc.X,
			Func: checkMethods[yc.X],
		}
		checks = append(checks, chk)
	}
	return checks, nil
}
