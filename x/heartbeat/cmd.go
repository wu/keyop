package heartbeat

import (
	"keyop/core"

	"github.com/spf13/cobra"
)

func NewCmd(deps core.Dependencies) *cobra.Command {
	heartbeatCmd := &cobra.Command{
		Use:   "heartbeat",
		Short: "Heartbeat Utility",
		Long:  `Execute the heartbeat command and display the message data.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := NewService(deps, core.ServiceConfig{})
			return svc.Check()
		},
	}

	return heartbeatCmd
}
