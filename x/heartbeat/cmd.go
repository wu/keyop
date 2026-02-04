package heartbeat

import (
	"keyop/core"
	"os"

	"github.com/spf13/cobra"
)

func NewCmd(deps core.Dependencies) *cobra.Command {
	heartbeatCmd := &cobra.Command{
		Use:   "heartbeat",
		Short: "Heartbeat Utility",
		Long:  `Execute the heartbeat command and display the message data.`,
		Run: func(cmd *cobra.Command, args []string) {

			logger := deps.MustGetLogger()

			svc := NewService(deps, core.ServiceConfig{
				Name: "heartbeat",
				Type: "heartbeat",
				Pubs: map[string]core.ChannelInfo{
					"events":  {Name: "heartbeat", Description: "Heartbeat events"},
					"metrics": {Name: "metrics", Description: "Heartbeat metrics"},
				},
			})

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

	return heartbeatCmd
}
