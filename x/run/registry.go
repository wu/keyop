package run

import (
	"keyop/core"
	"keyop/x/githubNotification"
	"keyop/x/graphite"
	"keyop/x/heartbeat"
	"keyop/x/httpPost"
	"keyop/x/httpPostServer"
	"keyop/x/metricsMonitor"
	"keyop/x/notify"
	"keyop/x/owntracks"
	"keyop/x/process"
	"keyop/x/slack"
	"keyop/x/speak"
	"keyop/x/temp"
	"keyop/x/thermostat"
)

var ServiceRegistry = map[string]func(deps core.Dependencies, cfg core.ServiceConfig) core.Service{
	"graphite": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return graphite.NewService(deps, cfg)
	},
	"githubNotification": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return githubNotification.NewService(deps, cfg)
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
	"metricsMonitor": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return metricsMonitor.NewService(deps, cfg)
	},
	"notify": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return notify.NewService(deps, cfg)
	},
	"owntracks": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return owntracks.NewService(deps, cfg)
	},
	"slack": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return slack.NewService(deps, cfg)
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
