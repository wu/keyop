package temp

import (
	"errors"
	"fmt"
	"keyop/core"
	"keyop/util"
	"os"
	"strconv"
	"strings"
)

var devicePath string

type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

func (svc Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	return util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"events"}, logger)
}

func (svc Service) Initialize() error {

	// todo: check that temp device exists here

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

	contentBytes, err := os.ReadFile(devicePath)
	if err != nil {
		temp.Error = fmt.Sprintf("could not read from %s: %s", devicePath, err.Error())
		logger.Error("temp", "data", temp)
		return temp, err
	}

	content := string(contentBytes)

	if len(content) == 0 {
		temp.Error = fmt.Sprintf("no content retrieved from temp device %s", devicePath)
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

	logger.Debug("temp", "data", temp)

	err = messenger.Send(core.Message{
		ChannelName: svc.Cfg.Pubs["events"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Text:        fmt.Sprintf("%s is %.3fF", svc.Cfg.Name, temp.TempF),
		Metric:      float64(temp.TempF),
		Data:        temp,
	})
	return temp, err
}
