package macosNotes

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"runtime"
	"strings"
)

type Service struct {
	Deps     core.Dependencies
	Cfg      core.ServiceConfig
	noteName string
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"events"}, logger)

	noteName, _ := svc.Cfg.Config["note_name"].(string)
	if noteName == "" {
		err := fmt.Errorf("required config 'note_name' is missing or empty")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	return errs
}

func (svc *Service) Initialize() error {
	svc.noteName, _ = svc.Cfg.Config["note_name"].(string)
	return nil
}

func (svc *Service) Check() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("macosNotes service is only supported on macOS")
	}

	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()
	osProvider := svc.Deps.MustGetOsProvider()

	// AppleScript to get the content of the note
	appleScript := fmt.Sprintf(`tell application "Notes" to get body of note "%s"`, svc.noteName)
	cmd := osProvider.Command("osascript", "-e", appleScript)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error("failed to get note content", "note", svc.noteName, "error", err, "output", string(output))
		return fmt.Errorf("failed to get note content: %w", err)
	}

	content := strings.TrimSpace(string(output))
	content = strings.ReplaceAll(content, "<br>", "")
	content = strings.ReplaceAll(content, "<li></li>", "")

	err = messenger.Send(core.Message{
		ChannelName: svc.Cfg.Pubs["events"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Text:        content,
	})
	if err != nil {
		logger.Error("failed to send note content", "error", err)
		return err
	}

	return nil
}
