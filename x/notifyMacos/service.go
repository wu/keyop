package notifyMacos

import (
	"fmt"
	"keyop/core"
	"keyop/util"
)

// At this time, this service only works on MacOS, as it relies on the 'osascript' command to display notifications.

type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	return util.ValidateConfig("subs", svc.Cfg.Subs, []string{"alerts"}, logger)
}

func (svc *Service) Initialize() error {
	messenger := svc.Deps.MustGetMessenger()
	return messenger.Subscribe(svc.Deps.MustGetContext(), svc.Cfg.Name, svc.Cfg.Subs["alerts"].Name, svc.Cfg.Type, svc.Cfg.Name, svc.Cfg.Subs["alerts"].MaxAge, svc.messageHandler)
}

func (svc *Service) messageHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()
	if msg.Text == "" {
		return nil
	}

	text := msg.Text
	logger.Info("Sending notification", "text", text)
	// osascript -e 'display notification "message" with title "KeyOp"'
	title := fmt.Sprintf("%s - %s", msg.ServiceName, msg.Hostname)
	text = fmt.Sprintf("[%s] %s", msg.Timestamp.Format("3:04pm"), text)
	script := fmt.Sprintf("display notification %q with title %q", text, title)
	logger.Warn("Executing osascript command", "script", script)
	osProvider := svc.Deps.MustGetOsProvider()
	cmd := osProvider.Command("osascript", "-e", script)
	err := cmd.Run()
	if err != nil {
		logger.Error("Failed to execute osascript command", "error", err)
		return err
	}

	return nil
}

func (svc *Service) Check() error {
	return nil
}
