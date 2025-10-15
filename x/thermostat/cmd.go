package thermostat

import (
	"keyop/core"
	"keyop/x/temp"
	"os"

	"github.com/spf13/cobra"
)

var cmdMinTemp float64
var cmdMaxTemp float64
var cmdMode string

func NewCmd(deps core.Dependencies) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "thermostat",
		Short: "Thermostat Utility",
		Long:  `Run the thermostat utility.`,
		Run: func(cmd *cobra.Command, args []string) {

			logger := deps.MustGetLogger()

			tmpSvc := temp.NewDefaultService(deps)
			thermoSvc := NewDefaultService(deps)

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

	cmd.Flags().Float64VarP(&cmdMinTemp, "minTemp", "n", 60.0, "Minimum Temperature")
	cmd.Flags().Float64VarP(&cmdMaxTemp, "maxTemp", "x", 80.0, "Maximum Temperature")
	cmd.Flags().StringVarP(&cmdMode, "mode", "m", "auto", "Thermostat Mode (off, heat, cool, auto)")

	return cmd
}

func NewDefaultService(deps core.Dependencies) core.Service {
	svc := NewService(deps, core.ServiceConfig{
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
			"minTemp": cmdMinTemp,
			"maxTemp": cmdMaxTemp,
			"mode":    cmdMode,
		},
	})

	return svc
}
