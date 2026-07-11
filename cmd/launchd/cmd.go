// Package launchd installs and manages keyop as a macOS launchd LaunchAgent.
// A LaunchAgent (not a LaunchDaemon) is used because macOS-specific services
// (notify, speak, reminders) require a user GUI session.
package launchd

import (
	"github.com/wu/keyop/core"

	"github.com/spf13/cobra"
)

// NewCmd builds the launchd subcommand providing installation and management helpers.
func NewCmd(deps core.Dependencies) *cobra.Command {
	launchdCmd := &cobra.Command{
		Use:   "launchd",
		Short: "keyop macOS launchd utilities",
		Long:  `Install as a launchd LaunchAgent and manage the service.`,
	}

	launchdCmd.AddCommand(NewInstallCmd(deps))
	launchdCmd.AddCommand(NewUninstallCmd(deps))
	launchdCmd.AddCommand(NewStartCmd(deps))
	launchdCmd.AddCommand(NewStopCmd(deps))
	launchdCmd.AddCommand(NewRestartCmd(deps))

	return launchdCmd
}
