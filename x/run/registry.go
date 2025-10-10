package run

import (
	"keyop/core"
	"keyop/x/heartbeat"
	"keyop/x/temp"
)

// map the X value in config to the actual check function
var checkMethods = map[string]func(core.Dependencies) error{
	"heartbeat": heartbeat.Check,
	"temp":      temp.Check,
}
