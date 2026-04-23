package systemctl

import (
	"github.com/wu/keyop/core"

	"github.com/spf13/cobra"
)

// NewCmd builds the systemctl subcommand providing installation and management helpers.
func NewCmd(deps core.Dependencies) *cobra.Command {
	installCmd := &cobra.Command{
		Use:   "systemctl",
		Short: "keyop systemd utilities",
		Long:  `Install into systemd and manage the service.`,
	}

	installCmd.AddCommand(NewInstallCmd(deps))
	installCmd.AddCommand(NewUninstallCmd(deps))
	installCmd.AddCommand(NewStartCmd(deps))
	installCmd.AddCommand(NewStopCmd(deps))
	installCmd.AddCommand(NewRestartCmd(deps))

	return installCmd
}
