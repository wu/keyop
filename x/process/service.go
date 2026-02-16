package process

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"os"
	"os/exec"
	"syscall"
	"time"
)

var startTime time.Time

func init() {
	// capture the service start time for reporting uptime
	startTime = time.Now()
}

type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig
	cmd  *exec.Cmd
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"errors", "events"}, logger)

	if _, ok := svc.Cfg.Config["command"].(string); !ok {
		err := fmt.Errorf("config field 'command' is required and must be a string")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	if _, ok := svc.Cfg.Config["pidFile"].(string); !ok {
		err := fmt.Errorf("config field 'pidFile' is required and must be a string")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	return errs
}

func (svc *Service) Initialize() error {
	return nil
}

type Event struct {
	Now           time.Time
	Uptime        string
	UptimeSeconds int64
	Pid           int
	Command       string
	Status        string
}

func (svc *Service) startProcess() error {
	logger := svc.Deps.MustGetLogger()
	command := svc.Cfg.Config["command"].(string)
	pidFile := svc.Cfg.Config["pidFile"].(string)

	svc.cmd = exec.Command("sh", "-c", command)
	if err := svc.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	logger.Info("started process", "pid", svc.cmd.Process.Pid, "command", command)

	osProvider := svc.Deps.MustGetOsProvider()
	f, err := osProvider.OpenFile(pidFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open pid file: %w", err)
	}
	//goland:noinspection GoUnhandledErrorResult
	defer f.Close()

	if _, err := f.WriteString(fmt.Sprintf("%d", svc.cmd.Process.Pid)); err != nil {
		return fmt.Errorf("failed to write to pid file: %w", err)
	}

	return nil
}

func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	var err error
	status := "running"
	if svc.cmd == nil || svc.cmd.Process == nil {
		logger.Info("process not started, starting now")
		err = svc.startProcess()
		status = "started"
	} else {
		// Check if process is still running
		if pErr := svc.cmd.Process.Signal(syscall.Signal(0)); pErr != nil {
			logger.Info("process died, restarting", "error", pErr)
			// Reap the process to avoid defunct state
			_ = svc.cmd.Wait()
			err = svc.startProcess()
			status = "restarted"
		}
	}

	if err != nil {
		return err
	}

	uptime := time.Since(startTime)
	event := Event{
		Now:           time.Now(),
		Uptime:        uptime.Round(time.Second).String(),
		UptimeSeconds: int64(uptime / time.Second),
		Pid:           svc.cmd.Process.Pid,
		Command:       svc.Cfg.Config["command"].(string),
		Status:        status,
	}

	logger.Debug("process check", "data", event)

	return messenger.Send(core.Message{
		ChannelName: svc.Cfg.Pubs["events"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Text:        fmt.Sprintf("process %s: pid %d, status %s", svc.Cfg.Name, event.Pid, event.Status),
		Data:        event,
	})
}
