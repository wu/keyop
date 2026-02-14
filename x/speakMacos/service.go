package speakMacos

import (
	"keyop/core"
	"keyop/util"
)

// At this time, this service only works on MacOS, as it relies on the 'say' command to speak text.
//
// NOTE: To use a different siri voice, choose the voice in System Preferences > Accessibility.
//       The exact location varies by MacOS version.  Try to search in Preferences for 'voice',
//       update the preference for Spoken Content.

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

	var text string
	if msg.Summary != "" {
		text = msg.Summary
	} else if msg.Text != "" {
		text = msg.Text
	} else {
		return nil
	}

	logger.Info("Speaking text", "text", text)
	osProvider := svc.Deps.MustGetOsProvider()
	cmd := osProvider.Command("say", text)
	err := cmd.Run()
	if err != nil {
		logger.Error("Failed to execute say command", "error", err)
		return err
	}

	return nil
}

func (svc *Service) Check() error {
	return nil
}
