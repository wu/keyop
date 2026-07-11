package launchd

import (
	"fmt"
	"os"

	"github.com/wu/keyop/core"

	"github.com/spf13/cobra"
)

// NewUninstallCmd returns a cobra command that uninstalls the launchd LaunchAgent.
func NewUninstallCmd(deps core.Dependencies) *cobra.Command {
	uninstallCmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall the launchd LaunchAgent",
		Long:  `Unload the agent and remove the plist.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return uninstallLaunchd(deps)
		},
	}

	return uninstallCmd
}

func uninstallLaunchd(deps core.Dependencies) error {
	logger := deps.MustGetLogger()
	osProvider := deps.MustGetOsProvider()

	logger.Info("Unloading LaunchAgent", "target", serviceTarget())
	if err := osProvider.Command("launchctl", "bootout", serviceTarget()).Run(); err != nil {
		logger.Warn("Failed to unload agent (it might not be loaded)", "error", err)
	}

	path, err := plistPath(osProvider)
	if err != nil {
		return err
	}

	logger.Info("Removing plist", "path", path)
	if err := osProvider.Remove(path); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove plist: %w", err)
		}
		logger.Warn("Plist does not exist")
	}

	logger.Info("Uninstallation successful")
	return nil
}
