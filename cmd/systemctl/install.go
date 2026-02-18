package systemctl

import (
	"fmt"
	"keyop/core"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func NewInstallCmd(deps core.Dependencies) *cobra.Command {
	var user string
	var group string

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install as a systemd service",
		Long:  `Generate systemd configuration and enable the service.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return installSystemd(deps, user, group)
		},
	}

	installCmd.Flags().StringVarP(&user, "user", "u", "root", "User to run the service as")
	installCmd.Flags().StringVarP(&group, "group", "g", "root", "Group to run the service as")

	return installCmd
}

func installSystemd(deps core.Dependencies, user, group string) error {
	logger := deps.MustGetLogger()
	osProvider := deps.MustGetOsProvider()

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	serviceConfig := fmt.Sprintf(`[Unit]
Description=Keyop Event-Driven Intelligence Toolkit
After=network.target

[Service]
ExecStart=%s run
Restart=always
User=%s
Group=%s

[Install]
WantedBy=multi-user.target
`, exe, user, group)

	servicePath := "/etc/systemd/system/keyop.service"

	logger.Info("Installing systemd service", "path", servicePath)

	f, err := osProvider.OpenFile(servicePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create service file (do you have root privileges?): %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(serviceConfig); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}

	logger.Info("Reloading systemd daemon")
	if err := osProvider.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("failed to reload systemd daemon: %w", err)
	}

	logger.Info("Enabling keyop service")
	if err := osProvider.Command("systemctl", "enable", "keyop.service").Run(); err != nil {
		return fmt.Errorf("failed to enable keyop service: %w", err)
	}

	logger.Info("Starting keyop service")
	if err := osProvider.Command("systemctl", "start", "keyop.service").Run(); err != nil {
		return fmt.Errorf("failed to start keyop service: %w", err)
	}

	logger.Info("Installation successful")
	return nil
}
