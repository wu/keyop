package thermostat

import (
	"keyop/core"
	"keyop/x/temp"
	"os"

	"github.com/spf13/cobra"
)

var minTemp float64
var maxTemp float64

func NewCmd(deps core.Dependencies) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "thermostat",
		Short: "Thermostat Utility",
		Long:  `Run the thermostat utility.`,
		Run: func(cmd *cobra.Command, args []string) {

			logger := deps.MustGetLogger()

			tmpSvc := temp.NewService(deps, core.ServiceConfig{
				Name: "temp",
				Type: "temp",
				Pubs: map[string]core.ChannelInfo{
					"events": {Name: "temp", Description: "temperature events"},
				},
			})

			thermoSvc := NewService(deps, core.ServiceConfig{
				Name: "thermostat",
				Type: "thermostat",
				Subs: map[string]core.ChannelInfo{
					"temp": {Name: "temp", Description: "Read temperature events"},
				},
				Pubs: map[string]core.ChannelInfo{
					"events": {Name: "thermostat", Description: "Publish thermostat events"},
					"heater": {Name: "heater-control", Description: "Publish to heater controller channel"},
					"cooler": {Name: "cooler-control", Description: "Publish to fan/ac controller channel"},
				},
				Config: map[string]interface{}{
					"minTemp": minTemp,
					"maxTemp": maxTemp,
				},
			})

			logger.Warn("validating config")
			if errs := thermoSvc.ValidateConfig(); len(errs) > 0 {
				logger.Error("thermostat validation error", "errors", errs)
				os.Exit(1)
			}
			if errs := tmpSvc.ValidateConfig(); len(errs) > 0 {
				logger.Error("temp validation error", "errors", errs)
				os.Exit(1)
			}

			if err := thermoSvc.Initialize(); err != nil {
				logger.Error("thermostat initialization error", "error", err)
				os.Exit(1)
			}
			if err := tmpSvc.Initialize(); err != nil {
				logger.Error("service initialization error", "error", err)
				os.Exit(1)
			}

			if err := tmpSvc.Check(); err != nil {
				logger.Error("service check error", "error", err)
				os.Exit(1)
			}
		},
	}

	cmd.Flags().Float64VarP(&minTemp, "minTemp", "n", 60.0, "Minimum Temperature")
	cmd.Flags().Float64VarP(&maxTemp, "maxTemp", "x", 80.0, "Maximum Temperature")

	return cmd
}
