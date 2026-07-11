package launchd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wu/keyop/core"

	"github.com/spf13/cobra"
)

// Label is the launchd service label for the keyop LaunchAgent.
const Label = "com.github.wu.keyop"

// guiDomain returns the launchd GUI domain target for the current user
// (e.g. "gui/501"). LaunchAgents live in the GUI domain, which requires an
// active login session — on headless Macs, enable automatic login.
func guiDomain() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

// serviceTarget returns the fully qualified launchd service target
// (e.g. "gui/501/com.github.wu.keyop").
func serviceTarget() string {
	return guiDomain() + "/" + Label
}

// plistPath returns the LaunchAgent plist location in the user's home.
func plistPath(osProvider core.OsProviderApi) (string, error) {
	home, err := osProvider.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist"), nil
}

// NewInstallCmd returns a cobra command that installs keyop as a launchd LaunchAgent.
func NewInstallCmd(deps core.Dependencies) *cobra.Command {
	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install as a launchd LaunchAgent",
		Long: `Generate a LaunchAgent plist for the current user and load it.

The agent runs in the GUI domain (required by the macOS-specific services),
so an active login session is needed — enable automatic login on headless
machines.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return installLaunchd(deps)
		},
	}

	return installCmd
}

func installLaunchd(deps core.Dependencies) error {
	logger := deps.MustGetLogger()
	osProvider := deps.MustGetOsProvider()

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	home, err := osProvider.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	logDir := filepath.Join(home, ".keyop", "logs")
	agentDir := filepath.Join(home, "Library", "LaunchAgents")
	for _, dir := range []string{logDir, agentDir} {
		if err := osProvider.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}

	// PATH includes homebrew and /usr/local so services that shell out to
	// user-installed tools keep working; launchd's default PATH is minimal.
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>run</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
	</dict>
</dict>
</plist>
`, Label, exe,
		filepath.Join(logDir, "launchd-stdout.log"),
		filepath.Join(logDir, "launchd-stderr.log"))

	path, err := plistPath(osProvider)
	if err != nil {
		return err
	}

	logger.Info("Installing LaunchAgent", "path", path)

	//nolint:gosec // plist is expected to be world-readable
	f, err := osProvider.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("failed to create plist: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			logger.Warn("launchd: failed to close plist file", "err", err)
		}
	}()

	if _, err := f.WriteString(plist); err != nil {
		return fmt.Errorf("failed to write plist: %w", err)
	}

	// Unload any previous instance so re-install picks up the new plist.
	// Ignore errors: the agent is usually not loaded yet.
	if err := osProvider.Command("launchctl", "bootout", serviceTarget()).Run(); err != nil {
		logger.Info("No previous instance to unload")
	}

	logger.Info("Enabling LaunchAgent", "target", serviceTarget())
	if err := osProvider.Command("launchctl", "enable", serviceTarget()).Run(); err != nil {
		return fmt.Errorf("failed to enable agent: %w", err)
	}

	logger.Info("Loading LaunchAgent", "domain", guiDomain())
	if err := osProvider.Command("launchctl", "bootstrap", guiDomain(), path).Run(); err != nil {
		return fmt.Errorf("failed to bootstrap agent (is a GUI session active? headless Macs need automatic login): %w", err)
	}

	logger.Info("Installation successful")
	return nil
}
