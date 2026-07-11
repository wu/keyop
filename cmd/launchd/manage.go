package launchd

import (
	"fmt"

	"github.com/wu/keyop/core"

	"github.com/spf13/cobra"
)

// NewStartCmd returns a cobra command that starts the LaunchAgent.
func NewStartCmd(deps core.Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the LaunchAgent",
		RunE: func(_ *cobra.Command, _ []string) error {
			return startLaunchd(deps)
		},
	}
}

// NewStopCmd returns a cobra command that stops (unloads) the LaunchAgent.
func NewStopCmd(deps core.Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the LaunchAgent (unloads it until the next start/install)",
		RunE: func(_ *cobra.Command, _ []string) error {
			logger := deps.MustGetLogger()
			osProvider := deps.MustGetOsProvider()

			// KeepAlive would immediately respawn a killed process, so stop
			// means unloading the agent from launchd.
			logger.Info("Unloading LaunchAgent", "target", serviceTarget())
			if err := osProvider.Command("launchctl", "bootout", serviceTarget()).Run(); err != nil {
				return fmt.Errorf("failed to stop agent: %w", err)
			}
			return nil
		},
	}
}

// NewRestartCmd returns a cobra command that restarts the LaunchAgent.
func NewRestartCmd(deps core.Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the LaunchAgent",
		RunE: func(_ *cobra.Command, _ []string) error {
			logger := deps.MustGetLogger()
			osProvider := deps.MustGetOsProvider()

			logger.Info("Restarting LaunchAgent", "target", serviceTarget())
			if err := osProvider.Command("launchctl", "kickstart", "-k", serviceTarget()).Run(); err != nil {
				return fmt.Errorf("failed to restart agent: %w", err)
			}
			return nil
		},
	}
}

// startLaunchd starts the agent: kickstart if it is already loaded, otherwise
// bootstrap the plist into the GUI domain (e.g. after a stop).
func startLaunchd(deps core.Dependencies) error {
	logger := deps.MustGetLogger()
	osProvider := deps.MustGetOsProvider()

	logger.Info("Starting LaunchAgent", "target", serviceTarget())
	if err := osProvider.Command("launchctl", "kickstart", serviceTarget()).Run(); err == nil {
		return nil
	}

	path, err := plistPath(osProvider)
	if err != nil {
		return err
	}
	logger.Info("Agent not loaded; bootstrapping", "path", path)
	if err := osProvider.Command("launchctl", "bootstrap", guiDomain(), path).Run(); err != nil {
		return fmt.Errorf("failed to start agent (is a GUI session active? headless Macs need automatic login): %w", err)
	}
	return nil
}
