package temp

import (
	"keyop/core"

	"github.com/spf13/cobra"
)

func NewCmd(deps core.Dependencies) *cobra.Command {
	tmpCmd := &cobra.Command{
		Use:   "temp",
		Short: "Temperature Sensor Utility",
		Long:  `Read a Ds18b20 temperature sensor and display the message data.`,
		RunE: func(cmd *cobra.Command, args []string) error {

			svc := NewService(deps, core.ServiceConfig{
				Name: "temp",
				Type: "temp",
				Pubs: map[string]core.ChannelInfo{
					"events": {Name: "temp", Description: "temperature events"},
				},
			})

			errs := svc.ValidateConfig()
			if len(errs) > 0 {
				return errs[0]
			}

			err := svc.Initialize()
			if err != nil {
				return err
			}

			return svc.Check()
		},
	}

	tmpCmd.Flags().StringVarP(&devicePath, "device", "d", "/sys/bus/w1/devices/28-000006388d49/w1_slave", "Device Path")

	return tmpCmd
}
