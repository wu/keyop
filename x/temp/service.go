package temp

import (
	"errors"
	"fmt"
	"keyop/core"
	"keyop/util"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

type Service struct {
	Deps       core.Dependencies
	Cfg        core.ServiceConfig
	DevicePath string
	MaxTemp    *float64
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps: deps,
		Cfg:  cfg,
	}

	if devicePath, ok := cfg.Config["devicePath"].(string); ok {
		svc.DevicePath = devicePath
	}

	if maxTemp, ok := cfg.Config["maxTemp"].(float64); ok {
		svc.MaxTemp = &maxTemp
	}

	return svc
}

func (svc Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"events", "metrics", "errors"}, logger)

	if _, ok := svc.Cfg.Config["devicePath"].(string); !ok {
		errs = append(errs, fmt.Errorf("temp: devicePath not set in config"))
	}

	if val, ok := svc.Cfg.Config["maxTemp"]; ok {
		if _, ok := val.(float64); !ok {
			errs = append(errs, fmt.Errorf("temp: maxTemp must be a float"))
		}
	}

	return errs
}

func (svc Service) Initialize() error {

	if svc.DevicePath == "" {
		return fmt.Errorf("temp: devicePath not set")
	}

	if _, err := os.Stat(svc.DevicePath); err != nil {
		return fmt.Errorf("temp: device path %s does not exist: %w", svc.DevicePath, err)
	}

	return nil
}

type Event struct {
	TempC float32 `json:"TempC,omitempty"`
	TempF float32 `json:"TempF,omitempty"`
	Raw   string  `json:"Raw,omitempty"`
	Error string  `json:"Error,omitempty"`
}

func (svc Service) Check() error {
	_, err := svc.temp()
	return err
}

func (svc Service) temp() (Event, error) {
	logger := svc.Deps.MustGetLogger()
	logger.Debug("temp check called")

	messenger := svc.Deps.MustGetMessenger()

	temp := Event{}

	contentBytes, err := os.ReadFile(svc.DevicePath)
	if err != nil {
		temp.Error = fmt.Sprintf("could not read from %s: %s", svc.DevicePath, err.Error())
		logger.Error("temp", "data", temp)
		return temp, err
	}

	content := string(contentBytes)

	if len(content) == 0 {
		temp.Error = fmt.Sprintf("no content retrieved from temp device %s", svc.DevicePath)
		logger.Error("temp", "data", temp)
		return temp, errors.New(temp.Error)
	}

	idx := strings.Index(content, "t=")

	temp.Raw = content[idx+2 : len(content)-1]
	logger.Debug("Ds18b20", "RAW TEMP", temp.Raw)

	tempInt, err := strconv.Atoi(temp.Raw)
	if err != nil {
		temp.Error = fmt.Sprintf("unable to convert temp string to int: %s: %s", temp.Raw, err.Error())
		logger.Error("temp", "data", temp)
		return temp, errors.New(temp.Error)
	}

	temp.TempC = float32(tempInt) / 1000
	temp.TempF = temp.TempC*9/5 + 32.0

	if svc.MaxTemp != nil && float64(temp.TempF) > *svc.MaxTemp {
		err := fmt.Errorf("temperature %.3f exceeds max %.3f", temp.TempF, *svc.MaxTemp)
		logger.Debug("temp", "error", err)
		return temp, err
	}

	logger.Debug("temp", "data", temp)

	metricPrefix, _ := svc.Cfg.Config["metricPrefix"].(string)
	metricName := svc.Cfg.Name
	if metricPrefix != "" {
		metricName = metricPrefix + svc.Cfg.Name
	}

	// generate correlation id for this check to tie together the events and metrics in the backend
	msgUuid := uuid.New().String()

	eventErr := messenger.Send(core.Message{
		Uuid:        msgUuid,
		ChannelName: svc.Cfg.Pubs["events"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Text:        fmt.Sprintf("%s is %.3fF", svc.Cfg.Name, temp.TempF),
		Summary:     fmt.Sprintf("%s is %.1f degrees", svc.Cfg.Name, temp.TempF),
		MetricName:  metricName,
		Metric:      float64(temp.TempF),
		Data:        temp,
	})
	if eventErr != nil {
		return temp, eventErr
	}

	metricErr := messenger.Send(core.Message{
		Uuid:        msgUuid,
		ChannelName: svc.Cfg.Pubs["metrics"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		MetricName:  metricName,
		Metric:      float64(temp.TempF),
		Text:        fmt.Sprintf("%s metric: %.3fF", svc.Cfg.Name, temp.TempF),
		Summary:     fmt.Sprintf("%s is %.1f degrees", svc.Cfg.Name, temp.TempF),
	})
	return temp, metricErr
}
