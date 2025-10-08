package heartbeat

import (
	"keyop/core"

	"github.com/spf13/cobra"
)

func NewHeartbeatCmd(deps core.Dependencies) *cobra.Command {
	heartbeatCmd := &cobra.Command{
		Use:   "heartbeat",
		Short: "Display Heartbeat",
		Long:  `Execute the heartbeat command and display the heartbeat output.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return heartbeat(deps)
		},
	}

	return heartbeatCmd
}

func heartbeat(deps core.Dependencies) error {
	deps.Logger.Info("heartbeat called")
	return nil
}
