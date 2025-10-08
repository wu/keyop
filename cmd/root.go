package cmd

import (
	"keyop/core"
	"keyop/x/heartbeat"
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

	rootCmd.AddCommand(heartbeat.NewHeartbeatCmd(deps))

	return rootCmd
}
func Execute() {

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{
		Logger: logger,
	}

	rootCmd := NewRootCmd(deps)
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
