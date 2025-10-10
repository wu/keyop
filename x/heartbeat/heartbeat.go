package heartbeat

import (
	"keyop/core"
	"time"

	"github.com/spf13/cobra"
)

func NewCmd(deps core.Dependencies) *cobra.Command {
	heartbeatCmd := &cobra.Command{
		Use:   "heartbeat",
		Short: "Heartbeat Utility",
		Long:  `Execute the heartbeat command and display the message data.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := NewService(deps)
			return svc.Check()
		},
	}

	return heartbeatCmd
}

var startTime time.Time

func init() {
	startTime = time.Now()
}

type Service struct {
	Deps core.Dependencies
}

func NewService(deps core.Dependencies) core.Service {
	return &Service{Deps: deps}
}

type Event struct {
	Now           time.Time
	Uptime        string
	UptimeSeconds int64
	Hostname      string
}

func (svc Service) Check() error {
	logger := svc.Deps.Logger
	logger.Debug("heartbeat called")

	uptime := time.Since(startTime)

	heartbeat := Event{
		Now:           time.Now(),
		Uptime:        uptime.Round(time.Second).String(),
		UptimeSeconds: int64(uptime / time.Second),
		Hostname:      svc.Deps.Hostname,
	}
	logger.Info("heartbeat", "data", heartbeat)

	return nil
}
