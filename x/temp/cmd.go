package temp

import (
	"keyop/core"
	"os"

	"github.com/spf13/cobra"
)

func NewCmd(deps core.Dependencies) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "temp",
		Short: "Temperature Sensor Utility",
		Long:  `Read a Ds18b20 temperature sensor and display the message data.`,
		Run: func(cmd *cobra.Command, args []string) {

			logger := deps.MustGetLogger()
			logger.Info("device path", "path", devicePath)
			svc := NewDefaultService(deps)

			logger.Warn("validating config")
			if errs := svc.ValidateConfig(); len(errs) > 0 {
				logger.Error("validation error", "errors", errs)
				os.Exit(1)
			}

			if err := svc.Initialize(); err != nil {
				logger.Error("service initialization error", "error", err)
				os.Exit(1)
			}

			if err := svc.Check(); err != nil {
				logger.Error("service check error", "error", err)
				os.Exit(1)
			}
		},
	}

	cmd.Flags().StringVarP(&devicePath, "device", "d", "/sys/bus/w1/devices/28-000006388d49/w1_slave", "Device Path")

	return cmd
}

func NewDefaultService(deps core.Dependencies) core.Service {
	svc := NewService(deps, core.ServiceConfig{
		Name: "temp",
		Type: "temp",
		Pubs: map[string]core.ChannelInfo{
			"events": {Name: "temp", Description: "temperature events"},
		},
	})
	return svc
}
