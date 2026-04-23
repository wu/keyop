// Package cmd: blank imports cause each service's init() to register itself.
package cmd

import (
	_ "github.com/wu/keyop/services/heartbeat"
)
