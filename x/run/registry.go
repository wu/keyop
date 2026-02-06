package run

import (
	"keyop/core"
	"keyop/x/github"
	"keyop/x/graphite"
	"keyop/x/heartbeat"
	"keyop/x/httpPost"
	"keyop/x/httpPostServer"
	"keyop/x/notify"
	"keyop/x/process"
	"keyop/x/speak"
	"keyop/x/temp"
	"keyop/x/thermostat"
)

var ServiceRegistry = map[string]func(deps core.Dependencies, cfg core.ServiceConfig) core.Service{
	"graphite": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return graphite.NewService(deps, cfg)
	},
	"github": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return github.NewService(deps, cfg)
	},
	"heartbeat": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return heartbeat.NewService(deps, cfg)
	},
	"httpPost": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return httpPost.NewService(deps, cfg)
	},
	"httpPostServer": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return httpPostServer.NewService(deps, cfg)
	},
	"notify": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return notify.NewService(deps, cfg)
	},
	"process": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return process.NewService(deps, cfg)
	},
	"speak": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return speak.NewService(deps, cfg)
	},

	"temp": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service { return temp.NewService(deps, cfg) },
	"thermostat": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return thermostat.NewService(deps, cfg)
	},
}
