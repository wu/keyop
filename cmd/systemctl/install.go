// Package systemctl implements the systemctl service for keyop and provides ValidateConfig, Initialize and Check hooks.
package systemctl

import (
	"fmt"
	"github.com/wu/keyop/core"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// NewInstallCmd returns a cobra command that installs keyop as a systemd service.
func NewInstallCmd(deps core.Dependencies) *cobra.Command {
	var user string
	var group string
	var home string

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install as a systemd service",
		Long:  `Generate systemd configuration and enable the service.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return installSystemd(deps, user, group, home)
		},
	}

	installCmd.Flags().StringVarP(&user, "user", "u", "root", "User to run the service as")
	installCmd.Flags().StringVarP(&group, "group", "g", "root", "Group to run the service as")
	installCmd.Flags().StringVar(&home, "home", "",
		"Absolute home directory for the service, setting HOME and WorkingDirectory. "+
			"Needed when the run-as user differs from the user owning ~/.keyop — e.g. a "+
			"service running as root whose data lives in /home/someone/.keyop. "+
			"Empty (the default) leaves systemd to derive HOME from the run-as user.")

	return installCmd
}

func installSystemd(deps core.Dependencies, user, group, home string) error {
	logger := deps.MustGetLogger()
	osProvider := deps.MustGetOsProvider()

	if home != "" && !filepath.IsAbs(home) {
		return fmt.Errorf("--home must be an absolute path, got %q", home)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	serviceLines := []string{
		fmt.Sprintf("ExecStart=%s run", exe),
		"Restart=always",
		fmt.Sprintf("User=%s", user),
		fmt.Sprintf("Group=%s", group),
	}
	// systemd derives HOME from the run-as user's passwd entry, and keyop resolves
	// its conf dir, plugins.yaml, messenger data dir, and sqlite paths from
	// os.UserHomeDir() — that is, from $HOME. When the two users differ, leaving
	// this unset does not fail: keyop quietly reads and writes the wrong tree
	// (e.g. /root/.keyop) while absolute paths in the config still point at the
	// intended one. Setting it explicitly reproduces what `sudo -E` does today.
	if home != "" {
		serviceLines = append(serviceLines,
			fmt.Sprintf("Environment=HOME=%s", home),
			fmt.Sprintf("WorkingDirectory=%s", home),
		)
	}

	serviceConfig := fmt.Sprintf(`[Unit]
Description=Keyop Event-Driven Intelligence Toolkit
After=network.target

[Service]
%s

[Install]
WantedBy=multi-user.target
`, strings.Join(serviceLines, "\n"))

	servicePath := "/etc/systemd/system/keyop.service"

	logger.Info("Installing systemd service", "path", servicePath)

	//nolint:gosec // systemd unit file is expected to be world-readable
	f, err := osProvider.OpenFile(servicePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create service file (do you have root privileges?): %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			logger.Warn("systemctl: failed to close service file", "err", err)
		}
	}()

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
