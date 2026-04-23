package cmd

import (
	"runtime"

	"github.com/spf13/cobra"
)

var (
	// Version contains the current version string.
	Version = "dev"
	// Commit contains the git commit SHA used to build this binary.
	Commit = "none"
	// Branch contains the git branch name used to build this binary.
	Branch = "none"
	// BuildTime contains the build timestamp for the binary.
	BuildTime = "unknown"
)

// NewVersionCmd returns a cobra command that prints build/version information.
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
