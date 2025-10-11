package cmd

import (
	"context"
	"keyop/core"
	"keyop/util"
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
		Short: "keyop is an IOT tool",
		Long:  `More information coming soon`,
	}

	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	rootCmd.AddCommand(heartbeat.NewCmd(deps))
	rootCmd.AddCommand(temp.NewCmd(deps))
	rootCmd.AddCommand(run.NewCmd(deps))

	return rootCmd
}
func Execute() {

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	hostname, err := util.GetShortHostname()
	if err != nil {
		logger.Error("failed to get hostname", "error", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps := core.Dependencies{}
	deps.SetHostname(hostname)
	deps.SetLogger(logger)
	deps.SetContext(ctx)

	rootCmd := NewRootCmd(deps)
	err = rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
