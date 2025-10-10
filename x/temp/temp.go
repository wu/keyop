package temp

import (
	"errors"
	"fmt"
	"keyop/core"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var devicePath string

func NewCmd(deps core.Dependencies) *cobra.Command {
	tmpCmd := &cobra.Command{
		Use:   "temp",
		Short: "Temp Utility",
		Long:  `Read a Ds18b20 temperature sensor and display the message data.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := NewService(deps)
			return svc.Check()
		},
	}

	tmpCmd.Flags().StringVarP(&devicePath, "device", "d", "/sys/bus/w1/devices/28-000006388d49/w1_slave", "Device Path")

	return tmpCmd
}

type Service struct {
	Deps core.Dependencies
}

func NewService(deps core.Dependencies) core.Service {
	return &Service{Deps: deps}
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
	logger := svc.Deps.Logger
	logger.Debug("temp check called")

	temp := Event{
		Now: time.Now(),
	}

	contentBytes, err := os.ReadFile(devicePath)
	if err != nil {
		temp.Error = fmt.Sprintf("could not read from %s: %s", devicePath, err.Error())
		logger.Info("temp", "data", temp)
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

	return temp, nil
}
