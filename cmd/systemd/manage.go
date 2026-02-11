package systemd

import (
	"fmt"
	"keyop/core"

	"github.com/spf13/cobra"
)

func NewStartCmd(deps core.Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the systemd service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSystemctl(deps, "start")
		},
	}
}

func NewStopCmd(deps core.Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the systemd service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSystemctl(deps, "stop")
		},
	}
}

func NewRestartCmd(deps core.Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the systemd service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSystemctl(deps, "restart")
		},
	}
}

func runSystemctl(deps core.Dependencies, action string) error {
	logger := deps.MustGetLogger()
	osProvider := deps.MustGetOsProvider()

	logger.Info(fmt.Sprintf("%s keyop service", capitalize(action)))
	if err := osProvider.Command("systemctl", action, "keyop.service").Run(); err != nil {
		return fmt.Errorf("failed to %s keyop service: %w", action, err)
	}

	return nil
}

func capitalize(s string) string {
	if len(s) == 0 {
		return ""
	}
	return string(s[0]-32) + s[1:]
}
