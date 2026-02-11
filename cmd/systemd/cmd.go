package systemd

import (
	"keyop/core"

	"github.com/spf13/cobra"
)

func NewCmd(deps core.Dependencies) *cobra.Command {
	installCmd := &cobra.Command{
		Use:   "systemd",
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
