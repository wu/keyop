package cmd

import (
	"fmt"
	"keyop/cmd/systemd"
	"keyop/core"
	"keyop/util"
	"keyop/x/run"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewRootCmd(deps core.Dependencies) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "keyop",
		Short: "Event-Driven Intelligence Toolkit",
		Long:  `More information coming soon`,
	}

	rootCmd.PersistentFlags().BoolP("stdout", "o", false, "display the logs in colorized output to stdout")

	rootCmd.AddCommand(run.NewCmd(deps))
	rootCmd.AddCommand(systemd.NewCmd(deps))
	rootCmd.AddCommand(NewSelfUpdateCmd(deps))
	rootCmd.AddCommand(NewVersionCmd())

	return rootCmd
}
func Execute() {

	var console bool
	fs := pflag.NewFlagSet("keyop", pflag.ContinueOnError)
	fs.ParseErrorsWhitelist.UnknownFlags = true
	fs.BoolVarP(&console, "stdout", "o", false, "display the logs in colorized output to stdout")
	versionFlag := fs.BoolP("version", "v", false, "display version information")
	_ = fs.Parse(os.Args[1:])

	if *versionFlag {
		fmt.Printf("Keyop Version: %s\n", Version)
		fmt.Printf("Git Commit:    %s\n", Commit)
		fmt.Printf("Git Branch:    %s\n", Branch)
		fmt.Printf("Build Time:    %s\n", BuildTime)
		os.Exit(0)
	}

	// always enable stdout if running with "systemd" as the first argument
	if len(os.Args) > 1 && os.Args[1] != "run" {
		console = true
	}

	deps := util.InitializeDependencies(console)
	defer deps.MustGetCancel()()

	rootCmd := NewRootCmd(deps)
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
