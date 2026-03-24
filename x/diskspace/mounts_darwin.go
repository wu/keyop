//go:build darwin

package diskspace

import (
	"os/exec"
	"strings"
)

// readMounts returns real mount points on macOS using the `mount` command.
func readMounts() ([]string, error) {
	out, err := exec.Command("mount").Output()
	if err != nil {
		return nil, err
	}

	skipFSTypes := map[string]bool{
		"devfs": true, "autofs": true, "map": true,
	}

	var mounts []string
	for _, line := range strings.Split(string(out), "\n") {
		// Format: device on mountpoint (fstype, options)
		parts := strings.Fields(line)
		if len(parts) < 4 {
			continue
		}
		mountPoint := parts[2]
		// Extract fstype from "(fstype," section
		fsType := ""
		for _, p := range parts[3:] {
			fsType = strings.TrimLeft(p, "(")
			fsType = strings.TrimRight(fsType, ",)")
			break
		}
		if skipFSTypes[fsType] {
			continue
		}
		mounts = append(mounts, mountPoint)
	}
	return mounts, nil
}
