package heartbeat

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"time"
)

var startTime time.Time

func init() {
	startTime = time.Now()
}

type Service struct {
	Deps          core.Dependencies
	Cfg           core.ServiceConfig
	ShortHostname string
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	logger := deps.MustGetLogger()
	os := deps.MustGetOsProvider()
	hostname, err := util.GetShortHostname(os)
	if err != nil {
		logger.Error("Error getting hostname", "error", err)
	}
	if hostname == "" {
		logger.Error("Error getting hostname", "error", "hostname was empty")
	}

	svc := &Service{Deps: deps, Cfg: cfg, ShortHostname: hostname}
	svc.validateConfig()
	return svc
}

func (svc *Service) validateConfig() {
	if svc.Cfg.Name == "" {
		svc.Cfg.Name = "heartbeat"
	}
	if svc.Cfg.Type == "" {
		svc.Cfg.Type = "heartbeat"
	}
	if svc.Cfg.Pubs == nil {
		svc.Cfg.Pubs = make(map[string]core.ChannelInfo)
	}
	_, ok := svc.Cfg.Pubs["events"]
	if !ok {
		svc.Cfg.Pubs["events"] = core.ChannelInfo{
			Name:        "events",
			Description: "General event channel",
		}
	}
}

type Event struct {
	Now           time.Time
	Uptime        string
	UptimeSeconds int64
	Hostname      string
}

func (svc Service) Check() error {
	logger := svc.Deps.MustGetLogger()

	// todo: get messenger at startup
	messenger := svc.Deps.MustGetMessenger()

	uptime := time.Since(startTime)

	heartbeat := Event{
		Now:           time.Now(),
		Uptime:        uptime.Round(time.Second).String(),
		UptimeSeconds: int64(uptime / time.Second),
		Hostname:      svc.ShortHostname,
	}
	logger.Info("heartbeat", "data", heartbeat)

	eventsChan, ok := svc.Cfg.Pubs["events"]
	if ok {
		logger.Info("Sending to events channel", "channel", eventsChan.Name)
		msg := core.Message{
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Text:        fmt.Sprintf("heartbeat: uptime %s", heartbeat.Uptime),
			Value:       float64(heartbeat.UptimeSeconds),
		}
		logger.Info("Sending to events channel", "message", msg)
		messenger.Send(eventsChan.Name, msg, heartbeat)
	}

	return nil
}
