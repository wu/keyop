package cmd

import (
	"fmt"
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
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Keyop Version: %s\n", Version)
			fmt.Printf("Git Commit:    %s\n", Commit)
			fmt.Printf("Git Branch:    %s\n", Branch)
			fmt.Printf("Build Time:    %s\n", BuildTime)
			fmt.Printf("Go Version:    %s\n", runtime.Version())
			fmt.Printf("OS/Arch:       %s/%s\n", runtime.GOOS, runtime.GOARCH)
		},
	}
}
