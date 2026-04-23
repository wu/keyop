package systemctl

import (
	"fmt"
	"github.com/wu/keyop/core"

	"github.com/spf13/cobra"
)

// NewStartCmd returns a cobra command that starts the systemd service.
func NewStartCmd(deps core.Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the systemd service",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSystemctl(deps, "start")
		},
	}
}

// NewStopCmd returns a cobra command that stops the systemd service.
func NewStopCmd(deps core.Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the systemd service",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSystemctl(deps, "stop")
		},
	}
}

// NewRestartCmd returns a cobra command that restarts the systemd service.
func NewRestartCmd(deps core.Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the systemd service",
		RunE: func(_ *cobra.Command, _ []string) error {
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
