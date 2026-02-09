package run

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"bytes"

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
	MaxAge      string `yaml:"max_age"`
}

func configDirPath() string {
	if dir := os.Getenv("KEYOP_CONF_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".keyop", "conf")
	}
	return filepath.Join(home, ".keyop", "conf")
}

// loadServiceConfigs reads all yaml files in ~/.keyop/conf and creates ServiceConfig objects
func loadServiceConfigs(deps core.Dependencies) ([]core.ServiceConfig, error) {
	dir := configDirPath()
	logger := deps.MustGetLogger()
	logger.Info("Loading service configs from directory", "path", dir)

	files, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config directory does not exist: %s", dir)
		}
		return nil, err
	}

	shortHostname, err := util.GetShortHostname(deps.MustGetOsProvider())
	if err != nil {
		return nil, fmt.Errorf("error getting short hostname: %w", err)
	}

	templateData := struct {
		ShortHostname string
	}{
		ShortHostname: shortHostname,
	}

	var allServiceConfigsSource []serviceConfigYaml

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		ext := filepath.Ext(file.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		p := filepath.Join(dir, file.Name())
		logger.Info("Loading service config file", "path", p)
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}

		// Process template
		tmpl, err := template.New(file.Name()).Parse(string(b))
		if err != nil {
			return nil, fmt.Errorf("error parsing template %s: %w", p, err)
		}

		var processed bytes.Buffer
		if err := tmpl.Execute(&processed, templateData); err != nil {
			return nil, fmt.Errorf("error executing template %s: %w", p, err)
		}

		var serviceConfigsSource []serviceConfigYaml
		if err := yaml.Unmarshal(processed.Bytes(), &serviceConfigsSource); err != nil {
			return nil, fmt.Errorf("error unmarshaling %s: %w", p, err)
		}
		allServiceConfigsSource = append(allServiceConfigsSource, serviceConfigsSource...)
	}

	var serviceConfigs []core.ServiceConfig
	for _, serviceConfigSource := range allServiceConfigsSource {

		pubs := make(map[string]core.ChannelInfo)
		for key, value := range serviceConfigSource.Pubs {
			var maxAge time.Duration
			if value.MaxAge != "" {
				var err error
				maxAge, err = time.ParseDuration(value.MaxAge)
				if err != nil {
					return nil, fmt.Errorf("error parsing max_age for pub %s: %w", key, err)
				}
			}
			pubs[key] = core.ChannelInfo{
				Name:        value.Name,
				Description: value.Description,
				MaxAge:      maxAge,
			}
		}

		subs := make(map[string]core.ChannelInfo)
		for key, value := range serviceConfigSource.Subs {
			var maxAge time.Duration
			if value.MaxAge != "" {
				var err error
				maxAge, err = time.ParseDuration(value.MaxAge)
				if err != nil {
					return nil, fmt.Errorf("error parsing max_age for sub %s: %w", key, err)
				}
			}
			subs[key] = core.ChannelInfo{
				Name:        value.Name,
				Description: value.Description,
				MaxAge:      maxAge,
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
