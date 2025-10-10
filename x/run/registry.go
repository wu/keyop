package run

import (
	"keyop/core"
	"keyop/x/heartbeat"
	"keyop/x/temp"
)

var NewObjectMethods = map[string]func(deps core.Dependencies) core.Service{
	"heartbeat": heartbeat.NewService,
	"temp":      temp.NewService,
}
