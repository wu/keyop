package util

import (
	"os"
	"strings"
)

// GetShortHostname returns the system hostname without the domain suffix.
// For example, "host.example.com" becomes "host".
// It returns an error if the hostname cannot be determined.
func GetShortHostname() (string, error) {
	shortHostname, err := os.Hostname()
	if err != nil {
		return shortHostname, err
	}
	if idx := strings.Index(shortHostname, "."); idx != -1 {
		shortHostname = shortHostname[:idx]
	}
	return shortHostname, nil
}
