// Package cmd provides CLI command definitions and bootstrapping for the keyop application.
package cmd

import (
	"fmt"
	"github.com/wu/keyop/cmd/systemctl"
	"github.com/wu/keyop/core"
	"github.com/wu/keyop/core/runtime"
	"github.com/wu/keyop/util"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// NewRootCmd builds the root CLI command and registers subcommands.
func NewRootCmd(deps core.Dependencies) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "keyop",
		Short: "Event-Driven Intelligence Toolkit",
		Long:  `More information coming soon`,
	}

	rootCmd.PersistentFlags().BoolP("stdout", "o", false, "display the logs in colorized output to stdout")

	rootCmd.AddCommand(runtime.NewCmd(deps))
	rootCmd.AddCommand(systemctl.NewCmd(deps))
	rootCmd.AddCommand(NewSelfUpdateCmd(deps))
	rootCmd.AddCommand(NewVersionCmd())

	return rootCmd
}

// Execute parses flags, initializes dependencies, and runs the root command; it exits on failure.
func Execute() {

	var console bool
	fs := pflag.NewFlagSet("keyop", pflag.ContinueOnError)
	fs.ParseErrorsAllowlist = pflag.ParseErrorsAllowlist{UnknownFlags: true}
	fs.BoolVarP(&console, "stdout", "o", false, "display the logs in colorized output to stdout")
	versionFlag := fs.BoolP("version", "v", false, "display version information")
	_ = fs.Parse(os.Args[1:]) // Ignore parse errors: unknown flags are allowed via ParseErrorsAllowlist

	if *versionFlag {
		fmt.Printf("Keyop Version: %s\n", Version)
		fmt.Printf("Git Commit:    %s\n", Commit)
		fmt.Printf("Git Branch:    %s\n", Branch)
		fmt.Printf("Build Time:    %s\n", BuildTime)
		os.Exit(0)
	}

	// when to enable stdout
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
