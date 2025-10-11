package temp

import (
	"encoding/json"
	"errors"
	"fmt"
	"keyop/core"
	"os"
	"strconv"
	"strings"
	"time"
)

var devicePath string

type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{Deps: deps, Cfg: cfg}
}

type Event struct {
	Now   time.Time
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

	temp := Event{
		Now: time.Now(),
	}

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

	logger.Info("temp", "data", temp)

	// todo: get messenger at startup
	messenger := svc.Deps.MustGetMessenger()

	_, ok := svc.Cfg.Pubs["events"]
	if ok {
		jsonData, err := json.Marshal(temp)
		if err != nil {
			logger.Error("failed to marshal temp data", "error", err)
			return temp, err
		}
		logger.Info("Sending to events channel", "channel", svc.Cfg.Pubs["events"].Name)
		msg := core.Message{
			Time:    time.Now(),
			Service: svc.Cfg.Name,
			Value:   float64(temp.TempF),
			Data:    string(jsonData),
		}
		logger.Info("Sending to events channel", "message", msg)
		messenger.Send(svc.Cfg.Pubs["events"].Name, msg)
	}

	return temp, nil
}
