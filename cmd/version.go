package cmd

import (
	"runtime"

	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	Commit    = "none"
	Branch    = "none"
	BuildTime = "unknown"
)

func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version information",
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Printf("Keyop Version: %s\n", Version)
			cmd.Printf("Git Commit:    %s\n", Commit)
			cmd.Printf("Git Branch:    %s\n", Branch)
			cmd.Printf("Build Time:    %s\n", BuildTime)
			cmd.Printf("Go Version:    %s\n", runtime.Version())
			cmd.Printf("OS/Arch:       %s/%s\n", runtime.GOOS, runtime.GOARCH)
		},
	}
}
