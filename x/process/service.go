// Package process monitors and manages external processes for services.
package process

import (
	"errors"
	"fmt"
	"keyop/core"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"strconv"
	"strings"

	"github.com/google/uuid"
)

var startTime time.Time

func init() {
	// capture the service start time for reporting uptime
	startTime = time.Now()
}

// Service manages a single configured external process for this service.
// It tracks the process id in the state store and handles restart/reap logic.
type Service struct {
	Deps     core.Dependencies
	Cfg      core.ServiceConfig
	stateKey string
	mu       sync.Mutex
	pid      int
}

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps:     deps,
		Cfg:      cfg,
		stateKey: fmt.Sprintf("process_%s_pid", cfg.Name),
	}
}

// Name satisfies the core.PayloadProvider interface.
func (svc *Service) Name() string { return "process" }

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	var errs []error

	if _, ok := svc.Cfg.Config["command"].(string); !ok {
		err := fmt.Errorf("config field 'command' is required and must be a string")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	return errs
}

// Initialize performs a one-time startup required by the service (resource loading or connectivity checks).
func (svc *Service) Initialize() error {
	logger := svc.Deps.MustGetLogger()
	// Load persisted pid once at startup into memory
	var pid int
	if err := svc.Deps.MustGetStateStore().Load(svc.stateKey, &pid); err != nil {
		logger.Debug("process: failed to load pid from state store at init", "error", err)
	}

	// Verify the pid corresponds to a running process using the unified liveness probe; clear if not.
	if pid != 0 {
		alive, lErr := svc.isProcessRunning(pid)
		if lErr != nil {
			logger.Debug("process: liveness probe error at init; keeping pid conservatively", "pid", pid, "error", lErr)
		} else if !alive {
			logger.Info("process: pid from state store not running, clearing", "pid", pid)
			pid = 0
			if saveErr := svc.Deps.MustGetStateStore().Save(svc.stateKey, 0); saveErr != nil {
				logger.Debug("process: failed to clear pid in state store at init", "error", saveErr)
			}
		}
	}

	svc.mu.Lock()
	svc.pid = pid
	svc.mu.Unlock()

	// This service monitors external processes (not children managed by this service).
	// It does not attempt to reap children or handle SIGCHLD; the monitored process is
	// intentionally disowned and runs independently (ppid == 1). Liveness is probed via
	// os.FindProcess + signal 0 checks in isProcessRunning().
	return nil
}

// Event represents a process lifecycle event (started, running, restarted, stopped) published by the process service.
type Event struct {
	Now           time.Time
	Uptime        string
	UptimeSeconds int64
	Pid           int
	Command       string
	Status        string
}

// PayloadType returns the canonical payload type for process events.
func (e Event) PayloadType() string { return "service.process.v1" }

// RegisterPayloads registers the process payload types with the provided registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	if err := reg.Register("process", func() any { return &Event{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register process alias: %w", err)
		}
	}
	if err := reg.Register("service.process.v1", func() any { return &Event{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register service.process.v1: %w", err)
		}
	}
	return nil
}

func (svc *Service) startProcess() error {
	logger := svc.Deps.MustGetLogger()
	command := svc.Cfg.Config["command"].(string)

	// Use shell + nohup to launch the command as a truly external background process
	// The shell echoes the backgrounded child's pid so we can persist and monitor it.
	shCmd := fmt.Sprintf("nohup %s >/dev/null 2>&1 & echo $!", command)
	cmd := exec.Command("sh", "-c", shCmd) //nolint:gosec // executing configured command intentionally
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to start process via shell: %w", err)
	}
	// parse pid from shell output
	pidStr := strings.TrimSpace(string(out))
	pid, pErr := strconv.Atoi(pidStr)
	if pErr != nil {
		return fmt.Errorf("failed to parse pid from shell output '%s': %w", pidStr, pErr)
	}
	logger.Info("started process", "pid", pid, "command", command)

	// persist pid to state store so we can track/discover it across restarts
	if state := svc.Deps.MustGetStateStore(); state != nil {
		if err := state.Save(svc.stateKey, pid); err != nil {
			logger.Debug("process: failed to save pid to state store", "error", err)
		}
	}

	// update in-memory pid
	svc.mu.Lock()
	svc.pid = pid
	svc.mu.Unlock()

	// do not keep cmd to avoid reaping; we intentionally disown
	return nil
}

// makeEvent builds an Event for the given pid and status.
func (svc *Service) makeEvent(p int, st string) Event {
	up := time.Since(startTime)
	return Event{Now: time.Now(), Uptime: up.Round(time.Second).String(), UptimeSeconds: int64(up / time.Second), Pid: p, Command: svc.Cfg.Config["command"].(string), Status: st}
}

// sendMessage emits a core.Message via the configured messenger, logging errors.
func (svc *Service) sendMessage(correlation string, eventName string, status string, text string, e Event) {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()
	msg := core.Message{
		Correlation: correlation,
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       eventName,
		Status:      status,
		Text:        text,
		MetricName:  "process." + svc.Cfg.Name,
		Metric:      float64(e.UptimeSeconds),
		Data:        e,
	}
	if err := messenger.Send(msg); err != nil {
		logger.Error("process: failed to send message", "error", err)
	}
}

// sendStartAndOk sends process_start and process_status(ok) messages.
func (svc *Service) sendStartAndOk(correlation string, p int) {
	e := svc.makeEvent(p, "ok")
	svc.sendMessage(correlation, "process_start", "ok", fmt.Sprintf("process %s started, pid %d", svc.Cfg.Name, p), e)
	svc.sendMessage(correlation, "process_status", "ok", fmt.Sprintf("process %s: pid %d, status ok", svc.Cfg.Name, p), e)
}

// sendStatusOk sends a single process_status ok message.
func (svc *Service) sendStatusOk(correlation string, p int) {
	e := svc.makeEvent(p, "ok")
	svc.sendMessage(correlation, "process_status", "ok", fmt.Sprintf("process %s: pid %d, status ok", svc.Cfg.Name, p), e)
}

// sendStatusCritical sends a process_status critical message with the provided text.
func (svc *Service) sendStatusCritical(correlation string, p int, text string) {
	e := svc.makeEvent(p, "critical")
	svc.sendMessage(correlation, "process_status", "critical", text, e)
}

// sendNotRunning emits process_not_running and a critical process_status.
func (svc *Service) sendNotRunning(correlation string, p int) {
	e := svc.makeEvent(p, "critical")
	svc.sendMessage(correlation, "process_not_running", "critical", fmt.Sprintf("process %s is not running, pid %d", svc.Cfg.Name, p), e)
	svc.sendMessage(correlation, "process_status", "critical", fmt.Sprintf("process %s: pid %d, status critical", svc.Cfg.Name, p), e)
}

// isProcessRunning probes process liveness using os.FindProcess + signal 0.
// Returns (true, nil) if the process exists (signal 0 returned nil) or if signaling
// returned EPERM (the process exists but cannot be signaled). Returns (false, nil)
// if the process does not exist (ESRCH). Any other error is returned for the
// caller to handle conservatively (do not restart on ambiguous errors).
func (svc *Service) isProcessRunning(pid int) (bool, error) {
	logger := svc.Deps.MustGetLogger()
	proc, err := os.FindProcess(pid)
	if err != nil {
		logger.Debug("isProcessRunning: FindProcess failed", "pid", pid, "error", err)
		return false, err
	}
	sigErr := proc.Signal(syscall.Signal(0))
	if sigErr == nil {
		return true, nil
	}
	if errors.Is(sigErr, syscall.ESRCH) {
		return false, nil
	}
	// On some platforms Signal may return non-ESRCH errors for a finished process
	// (e.g., "os: process already finished" on Darwin). Treat that as not running.
	if sigErr != nil && strings.Contains(sigErr.Error(), "process already finished") {
		return false, nil
	}
	if errors.Is(sigErr, syscall.EPERM) {
		// The process exists, but we lack permission to signal it; treat as running.
		return true, nil
	}
	// Unexpected error: log and return it so the caller can decide conservatively.
	logger.Info("isProcessRunning: unexpected signal error", "pid", pid, "error", sigErr)
	return false, sigErr
}

// Check performs the service's periodic work: collect data, evaluate state, and publish messages/metrics.
func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()

	// create a correlation id for messages in this Check
	correlation := uuid.NewString()

	var err error
	status := "running"

	// helper methods moved to Service to avoid allocating closures on each Check invocation

	// use svc.sendMessage(correlation, ...) to emit messages

	// Use in-memory pid loaded at Initialize; avoid repeated state store reads.
	var pid int
	svc.mu.Lock()
	pid = svc.pid
	svc.mu.Unlock()

	// helper methods moved to Service to avoid allocating closures on each Check invocation
	// See methods: makeEvent, sendMessage, sendStartAndOk, sendStatusOk, sendStatusCritical, sendNotRunning

	if pid == 0 {
		logger.Info("process not started (no pid), starting now")
		err = svc.startProcess()
		status = "started"
		if err == nil {
			// refresh in-memory pid
			svc.mu.Lock()
			pid = svc.pid
			svc.mu.Unlock()
			svc.sendStartAndOk(correlation, pid)
		} else {
			logger.Error("process: failed to start", "error", err)
			svc.sendStatusCritical(correlation, 0, fmt.Sprintf("process %s failed to start", svc.Cfg.Name))
			return err
		}
	} else {
		// Probe liveness via signal 0
		alive, lErr := svc.isProcessRunning(pid)
		if lErr != nil {
			logger.Debug("process: liveness probe error; not restarting", "pid", pid, "error", lErr)
			// do not restart on ambiguous probe errors
			return nil
		}
		if !alive {
			// process confirmed not running; restart
			logger.Info("process: not running (probe), restarting", "pid", pid)
			svc.sendNotRunning(correlation, pid)
			err = svc.startProcess()
			status = "restarted"
			if err != nil {
				logger.Error("process: failed to restart after liveness probe", "error", err)
				svc.sendStatusCritical(correlation, pid, fmt.Sprintf("process %s: pid %d not running", svc.Cfg.Name, pid))
				return err
			}
			// refresh in-memory pid
			svc.mu.Lock()
			pid = svc.pid
			svc.mu.Unlock()
			svc.sendStartAndOk(correlation, pid)
		} else {
			// process is running; send status ok
			svc.sendStatusOk(correlation, pid)
		}
	}

	// final event for logging
	event := svc.makeEvent(pid, status)

	logger.Debug("process check", "data", event)

	// final send suppressed; messages already emitted above
	return nil
}
