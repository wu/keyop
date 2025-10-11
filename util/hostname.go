package util

import (
	"keyop/core"
	"strings"
)

// GetShortHostname returns the system hostname without the domain suffix.
// For example, "host.example.com" becomes "host".
// It returns an error if the hostname cannot be determined.
func GetShortHostname(osProvider core.OsProviderApi) (string, error) {
	shortHostname, err := osProvider.Hostname()
	if err != nil {
		return shortHostname, err
	}
	if idx := strings.Index(shortHostname, "."); idx != -1 {
		shortHostname = shortHostname[:idx]
	}
	return shortHostname, nil
}
