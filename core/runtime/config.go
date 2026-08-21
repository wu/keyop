package runtime

import (
	"bytes"
	"fmt"
	"github.com/wu/keyop/core"
	"github.com/wu/keyop/util"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	yaml "gopkg.in/yaml.v3"
)

// YAML representation of services in the config file
type serviceConfigYaml struct {
	Name     string                      `yaml:"name,omitempty"`
	Freq     string                      `yaml:"freq"`
	Service  string                      `yaml:"service"`
	Pubs     map[string]eventChannelYaml `yaml:"pubs"`
	Subs     map[string]eventChannelYaml `yaml:"subs"`
	Config   map[string]interface{}      `yaml:"config,omitempty"`
	PubRules []core.RuleSpec             `yaml:"pub_rules,omitempty"`
	SubRules []core.RuleSpec             `yaml:"sub_rules,omitempty"`
}

type eventChannelYaml struct {
	Name             string `yaml:"name"`
	Remote           string `yaml:"remote"`
	Description      string `yaml:"description"`
	MaxAge           string `yaml:"max_age"`
	MaxRetries       *int   `yaml:"max_retries"`
	RetryBackoffBase string `yaml:"retry_backoff_base"`
	RetryBackoffMax  string `yaml:"retry_backoff_max"`
}

// parseOptionalDuration parses a duration string, returning 0 for an empty value.
func parseOptionalDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	return time.ParseDuration(s)
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

	userHome, err := os.UserHomeDir()
	if err != nil {
		// Fallback to a sensible default or just empty string if we can't get it
		// In configDirPath it uses filepath.Join(".", ".keyop", "conf") if it fails.
		userHome = ""
	}

	templateData := struct {
		ShortHostname string
		HomeDir       string
	}{
		ShortHostname: shortHostname,
		HomeDir:       userHome,
	}

	var allServiceConfigsSource []struct {
		serviceConfigYaml
		filename string
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		ext := filepath.Ext(file.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		if file.Name() == "plugins.yaml" || file.Name() == "messenger.yaml" {
			continue
		}

		p := filepath.Join(dir, file.Name())
		logger.Info("Loading service config file", "path", p)
		b, err := os.ReadFile(p) //nolint:gosec // reading trusted config file under config dir
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

		var serviceConfigSource serviceConfigYaml
		if err := yaml.Unmarshal(processed.Bytes(), &serviceConfigSource); err != nil {
			return nil, fmt.Errorf("error unmarshaling %s: %w", p, err)
		}

		// determine filename without extension
		filenameBase := strings.TrimSuffix(file.Name(), ext)

		allServiceConfigsSource = append(allServiceConfigsSource, struct {
			serviceConfigYaml
			filename string
		}{serviceConfigSource, filenameBase})
	}

	var serviceConfigs []core.ServiceConfig
	for _, wrapper := range allServiceConfigsSource {
		serviceConfigSource := wrapper.serviceConfigYaml

		pubs := make(map[string]core.ChannelInfo)
		for key, value := range serviceConfigSource.Pubs {
			maxAge, err := parseOptionalDuration(value.MaxAge)
			if err != nil {
				return nil, fmt.Errorf("error parsing max_age for pub %s: %w", key, err)
			}
			pubs[key] = core.ChannelInfo{
				Name:        value.Name,
				Remote:      value.Remote,
				Description: value.Description,
				MaxAge:      maxAge,
			}
		}

		subs := make(map[string]core.ChannelInfo)
		for key, value := range serviceConfigSource.Subs {
			maxAge, err := parseOptionalDuration(value.MaxAge)
			if err != nil {
				return nil, fmt.Errorf("error parsing max_age for sub %s: %w", key, err)
			}
			retryBase, err := parseOptionalDuration(value.RetryBackoffBase)
			if err != nil {
				return nil, fmt.Errorf("error parsing retry_backoff_base for sub %s: %w", key, err)
			}
			retryMax, err := parseOptionalDuration(value.RetryBackoffMax)
			if err != nil {
				return nil, fmt.Errorf("error parsing retry_backoff_max for sub %s: %w", key, err)
			}
			subs[key] = core.ChannelInfo{
				Name:             value.Name,
				Remote:           value.Remote,
				Description:      value.Description,
				MaxAge:           maxAge,
				MaxRetries:       value.MaxRetries,
				RetryBackoffBase: retryBase,
				RetryBackoffMax:  retryMax,
			}
		}

		// use filename
		name := wrapper.filename

		// Rules are parsed here so a malformed condition stops startup at config
		// load, in the same way a bad max_age does. Field paths and value types
		// are checked later, in validateServiceConfig, once payload prototypes
		// are registered.
		pubRules, pubErrs := core.ParseRules(name+" pub_rules", serviceConfigSource.PubRules)
		subRules, subErrs := core.ParseRules(name+" sub_rules", serviceConfigSource.SubRules)
		if errs := append(pubErrs, subErrs...); len(errs) > 0 {
			for _, err := range errs {
				logger.Error("invalid rule in service config", "name", name, "error", err)
			}
			return nil, fmt.Errorf("invalid rules in service config %q", name)
		}

		svcConfig := core.ServiceConfig{
			Name:     name,
			Type:     serviceConfigSource.Service,
			Pubs:     pubs,
			Subs:     subs,
			Config:   serviceConfigSource.Config,
			PubRules: pubRules,
			SubRules: subRules,
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
