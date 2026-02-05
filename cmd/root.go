package cmd

import (
	"keyop/core"
	"keyop/util"
	"keyop/x/heartbeat"
	"keyop/x/run"
	"os"

	"github.com/spf13/cobra"
)

func NewRootCmd(deps core.Dependencies) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "keyop",
		Short: "Event-Driven Intelligence Toolkit",
		Long:  `More information coming soon`,
	}

	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	rootCmd.AddCommand(heartbeat.NewCmd(deps))
	rootCmd.AddCommand(run.NewCmd(deps))

	return rootCmd
}
func Execute() {

	deps := util.InitializeDependencies()
	defer deps.MustGetCancel()()

	rootCmd := NewRootCmd(deps)
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
