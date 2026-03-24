//go:build linux

package diskspace

import (
	"bufio"
	"os"
	"strings"
)

// readMounts returns all mount points from /proc/mounts, skipping virtual filesystems.
func readMounts() ([]string, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	skipFSTypes := map[string]bool{
		"sysfs": true, "proc": true, "devtmpfs": true, "devpts": true,
		"tmpfs": true, "cgroup": true, "cgroup2": true, "pstore": true,
		"bpf": true, "tracefs": true, "debugfs": true, "mqueue": true,
		"hugetlbfs": true, "fusectl": true, "configfs": true, "securityfs": true,
		"efivarfs": true, "autofs": true, "overlay": true,
	}

	var mounts []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		fsType := fields[2]
		mountPoint := fields[1]
		if skipFSTypes[fsType] {
			continue
		}
		mounts = append(mounts, mountPoint)
	}
	return mounts, scanner.Err()
}
