package heartbeat

import (
	"keyop/core"
	"time"

	"github.com/spf13/cobra"
)

func NewHeartbeatCmd(deps core.Dependencies) *cobra.Command {
	heartbeatCmd := &cobra.Command{
		Use:   "heartbeat",
		Short: "Heartbeat Utility",
		Long:  `Execute the heartbeat command and display the message data.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return Check(deps)
		},
	}

	return heartbeatCmd
}

var startTime time.Time

func init() {
	startTime = time.Now()
}

type Heartbeat struct {
	Now           time.Time
	Uptime        string
	UptimeSeconds int64
	Hostname      string
}

func Check(deps core.Dependencies) error {
	deps.Logger.Debug("heartbeat called")

	uptime := time.Since(startTime)

	heartbeat := Heartbeat{
		Now:           time.Now(),
		Uptime:        uptime.Round(time.Second).String(),
		UptimeSeconds: int64(uptime / time.Second),
		Hostname:      deps.Hostname,
	}
	deps.Logger.Info("heartbeat", "data", heartbeat)

	return nil
}
