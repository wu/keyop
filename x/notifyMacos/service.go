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

	logger.Info("Sending notification", "text", msg.Text)
	// osascript -e 'display notification "message" with title "KeyOp"'
	title := fmt.Sprintf(":keyop: %s - %s", msg.ServiceName, msg.ServiceType)
	script := fmt.Sprintf("display notification %q with title %q", msg.Text, title)
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
