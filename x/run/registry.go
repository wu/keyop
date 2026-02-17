package run

import (
	"keyop/core"
	"keyop/x/anomalyDetector"
	"keyop/x/cpuMonitor"
	"keyop/x/githubNotification"
	"keyop/x/graphite"
	"keyop/x/heartbeat"
	"keyop/x/httpPostClient"
	"keyop/x/httpPostServer"
	"keyop/x/kodi"
	"keyop/x/logManager"
	"keyop/x/macosMessages"
	"keyop/x/memoryMonitor"
	"keyop/x/metricsMonitor"
	"keyop/x/moon"
	"keyop/x/notifyMacos"
	"keyop/x/nwsWeather"
	"keyop/x/ollama"
	"keyop/x/owntracks"
	"keyop/x/pingMonitor"
	"keyop/x/process"
	"keyop/x/slack"
	"keyop/x/speakMacos"
	"keyop/x/statusMonitor"
	"keyop/x/sun"
	"keyop/x/temp"
	"keyop/x/thermostat"
	"keyop/x/webSocketClient"
	"keyop/x/webSocketServer"
)

var ServiceRegistry = map[string]func(deps core.Dependencies, cfg core.ServiceConfig) core.Service{
	"anomalyDetector": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return anomalyDetector.NewService(deps, cfg)
	},
	"graphite": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return graphite.NewService(deps, cfg)
	},
	"githubNotification": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return githubNotification.NewService(deps, cfg)
	},
	"cpuMonitor": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return cpuMonitor.NewService(deps, cfg)
	},
	"heartbeat": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return heartbeat.NewService(deps, cfg)
	},
	"httpPostClient": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return httpPostClient.NewService(deps, cfg)
	},
	"httpPostServer": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return httpPostServer.NewService(deps, cfg)
	},
	"kodi": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return kodi.NewService(deps, cfg)
	},
	"logManager": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return logManager.NewService(deps, cfg)
	},
	"memoryMonitor": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return memoryMonitor.NewService(deps, cfg)
	},
	"metricsMonitor": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return metricsMonitor.NewService(deps, cfg)
	},
	"moon": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return moon.NewService(deps, cfg)
	},
	"pingMonitor": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return pingMonitor.NewService(deps, cfg)
	},
	"notifyMacos": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return notifyMacos.NewService(deps, cfg)
	},
	"macosMessages": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return macosMessages.NewService(deps, cfg)
	},
	"owntracks": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return owntracks.NewService(deps, cfg)
	},
	"ollama": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return ollama.NewService(deps, cfg)
	},
	"slack": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return slack.NewService(deps, cfg)
	},
	"process": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return process.NewService(deps, cfg)
	},
	"speakMacos": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return speakMacos.NewService(deps, cfg)
	},
	"statusMonitor": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return statusMonitor.NewService(deps, cfg)
	},
	"sun": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return sun.NewService(deps, cfg)
	},
	"weather": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return nwsWeather.NewService(deps, cfg)
	},

	"temp": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service { return temp.NewService(deps, cfg) },
	"thermostat": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return thermostat.NewService(deps, cfg)
	},
	"webSocketClient": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return webSocketClient.NewService(deps, cfg)
	},
	"webSocketServer": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return webSocketServer.NewService(deps, cfg)
	},
}
