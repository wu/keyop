package run

import (
	"keyop/core"
	"keyop/x/alerts"
	"keyop/x/aurora"
	"keyop/x/condition"
	"keyop/x/cpuMonitor"
	"keyop/x/githubNotification"
	"keyop/x/graphite"
	"keyop/x/heartbeat"
	"keyop/x/httpPostClient"
	"keyop/x/httpPostServer"
	"keyop/x/idle"
	"keyop/x/kodi"
	"keyop/x/logManager"
	"keyop/x/macosBluetoothBattery"
	"keyop/x/macosReminders"
	"keyop/x/memoryMonitor"
	"keyop/x/messengerStats"
	"keyop/x/metricmon"
	"keyop/x/moon"
	"keyop/x/notify"
	"keyop/x/nwsWeather"
	"keyop/x/ollama"
	"keyop/x/owntracks"
	"keyop/x/pingMonitor"
	"keyop/x/process"
	"keyop/x/slack"
	"keyop/x/speak"
	"keyop/x/sqlite"
	"keyop/x/sslMonitor"
	"keyop/x/statusmon"
	"keyop/x/sun"
	"keyop/x/temp"
	"keyop/x/thermostat"
	"keyop/x/tides"
	"keyop/x/txtmsg"
	"keyop/x/versionControlGit"
	"keyop/x/weatherWs2902c"
	"keyop/x/webSocketClient"
	"keyop/x/webSocketServer"
	"keyop/x/webui"
)

// ServiceRegistry maps service type names to constructors used by the run command and plugin loader.
var ServiceRegistry = map[string]func(deps core.Dependencies, cfg core.ServiceConfig) core.Service{
	"aurora": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return aurora.NewService(deps, cfg)
	},
	"macosBluetoothBattery": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return macosBluetoothBattery.NewService(deps, cfg)
	},
	"condition": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return condition.NewService(deps, cfg)
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
	"idle": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return idle.NewService(deps, cfg)
	},
	"logManager": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return logManager.NewService(deps, cfg)
	},
	"memoryMonitor": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return memoryMonitor.NewService(deps, cfg)
	},
	"messengerStats": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return messengerStats.NewService(deps, cfg)
	},
	"metricmon": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return metricmon.NewService(deps, cfg)
	},
	"moon": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return moon.NewService(deps, cfg)
	},
	"tides": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return tides.NewService(deps, cfg)
	},
	"pingMonitor": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return pingMonitor.NewService(deps, cfg)
	},
	"notify": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return notify.NewService(deps, cfg)
	},
	"alerts": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return alerts.NewService(deps, cfg)
	},
	"txtmsg": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return txtmsg.NewService(deps, cfg)
	},
	"macosReminders": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return macosReminders.NewService(deps, cfg)
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
	"sqlite": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return sqlite.NewService(deps, cfg)
	},
	"process": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return process.NewService(deps, cfg)
	},
	"speak": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return speak.NewService(deps, cfg)
	},
	"sslMonitor": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return sslMonitor.NewService(deps, cfg)
	},
	"statusmon": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return statusmon.NewService(deps, cfg)
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
	"versionControlGit": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return versionControlGit.NewService(deps, cfg)
	},
	"webSocketClient": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return webSocketClient.NewService(deps, cfg)
	},
	"webSocketServer": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return webSocketServer.NewService(deps, cfg)
	},
	"weatherWs2902c": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return weatherWs2902c.NewService(deps, cfg)
	},
	"webui": func(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
		return webui.NewService(deps, cfg)
	},
}
