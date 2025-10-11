package cmd

import (
	"context"
	"keyop/core"
	"keyop/x/heartbeat"
	"keyop/x/run"
	"keyop/x/temp"
	"log/slog"

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
	rootCmd.AddCommand(temp.NewCmd(deps))
	rootCmd.AddCommand(run.NewCmd(deps))

	return rootCmd
}
func Execute() {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := core.Dependencies{}
	deps.SetOsProvider(core.OsProvider{})
	deps.SetLogger(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	deps.SetContext(ctx)

	rootCmd := NewRootCmd(deps)
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
