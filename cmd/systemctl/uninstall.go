package systemctl

import (
	"fmt"
	"keyop/core"
	"os"

	"github.com/spf13/cobra"
)

func NewUninstallCmd(deps core.Dependencies) *cobra.Command {
	uninstallCmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall the systemd service",
		Long:  `Stop the service, disable it, and remove the configuration file.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return uninstallSystemd(deps)
		},
	}

	return uninstallCmd
}

func uninstallSystemd(deps core.Dependencies) error {
	logger := deps.MustGetLogger()
	osProvider := deps.MustGetOsProvider()

	servicePath := "/etc/systemd/system/keyop.service"

	logger.Info("Stopping keyop service")
	if err := osProvider.Command("systemctl", "stop", "keyop.service").Run(); err != nil {
		logger.Warn("Failed to stop service (it might not be running)", "error", err)
	}

	logger.Info("Disabling keyop service")
	if err := osProvider.Command("systemctl", "disable", "keyop.service").Run(); err != nil {
		logger.Warn("Failed to disable service", "error", err)
	}

	logger.Info("Removing service file", "path", servicePath)
	if err := osProvider.Remove(servicePath); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove service file: %w", err)
		}
		logger.Warn("Service file does not exist")
	}

	logger.Info("Reloading systemd daemon")
	if err := osProvider.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("failed to reload systemd daemon: %w", err)
	}

	logger.Info("Uninstallation successful")
	return nil
}
