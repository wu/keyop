package diskspace

import (
	"fmt"
	"keyop/core"
	"strings"
	"syscall"

	"github.com/google/uuid"
)

// Service monitors disk space across configured filesystems and emits usage and status events.
type Service struct {
	Deps              core.Dependencies
	Cfg               core.ServiceConfig
	includes          []string
	excludes          []string
	warningThreshold  float64
	criticalThreshold float64
}

// NewService creates a new diskspace service.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps:              deps,
		Cfg:               cfg,
		warningThreshold:  80.0,
		criticalThreshold: 90.0,
	}

	if v, ok := cfg.Config["warningThreshold"].(float64); ok {
		svc.warningThreshold = v
	} else if v, ok := cfg.Config["warningThreshold"].(int); ok {
		svc.warningThreshold = float64(v)
	}
	if v, ok := cfg.Config["criticalThreshold"].(float64); ok {
		svc.criticalThreshold = v
	} else if v, ok := cfg.Config["criticalThreshold"].(int); ok {
		svc.criticalThreshold = float64(v)
	}

	if inc, ok := cfg.Config["include"].([]interface{}); ok {
		for _, v := range inc {
			if s, ok := v.(string); ok {
				svc.includes = append(svc.includes, s)
			}
		}
	}
	if exc, ok := cfg.Config["exclude"].([]interface{}); ok {
		for _, v := range exc {
			if s, ok := v.(string); ok {
				svc.excludes = append(svc.excludes, s)
			}
		}
	}

	return svc
}

// ValidateConfig validates the service configuration.
func (svc *Service) ValidateConfig() []error {
	var errs []error

	if svc.warningThreshold >= svc.criticalThreshold {
		err := fmt.Errorf("diskspace: warningThreshold (%.1f) must be less than criticalThreshold (%.1f)", svc.warningThreshold, svc.criticalThreshold)
		svc.Deps.MustGetLogger().Error(err.Error())
		errs = append(errs, err)
	}
	return errs
}

// Initialize is a no-op; all work is done in Check.
func (svc *Service) Initialize() error {
	return nil
}

// Check reads disk space for all monitored filesystems and emits a DiskSpaceEvent and a StatusEvent.
func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	filesystems, err := svc.gatherFilesystems()
	if err != nil {
		logger.Error("diskspace: failed to gather filesystem data", "error", err)
		return err
	}

	correlationID := uuid.NewString()

	// Determine overall status level
	overallLevel := "ok"
	var problemDetails []string
	for i := range filesystems {
		fs := &filesystems[i]
		switch {
		case fs.UsedPercent >= svc.criticalThreshold:
			fs.Level = "critical"
		case fs.UsedPercent >= svc.warningThreshold:
			fs.Level = "warning"
		default:
			fs.Level = "ok"
		}
		if fs.Level == "critical" && overallLevel != "critical" {
			overallLevel = "critical"
		} else if fs.Level == "warning" && overallLevel == "ok" {
			overallLevel = "warning"
		}
		if fs.Level != "ok" {
			problemDetails = append(problemDetails, fmt.Sprintf("%s %.1f%% used", fs.Filesystem, fs.UsedPercent))
		}
	}

	// Emit the diskspace event with metrics for all filesystems
	event := Event{Filesystems: filesystems}
	if err := messenger.Send(core.Message{
		Correlation: correlationID,
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "diskspace_event",
		Text:        fmt.Sprintf("disk space check: %d filesystem(s) monitored, overall level: %s", len(filesystems), overallLevel),
		Data:        &event,
	}); err != nil {
		logger.Warn("diskspace: failed to send diskspace_event", "err", err)
	}

	// Emit a status event summarising the overall health
	statusDetails := "all filesystems ok"
	if len(problemDetails) > 0 {
		statusDetails = strings.Join(problemDetails, "; ")
	}
	statusEvent := core.StatusEvent{
		Name:    svc.Cfg.Name,
		Status:  overallLevel,
		Details: statusDetails,
		Level:   overallLevel,
	}
	if err := messenger.Send(core.Message{
		Correlation: correlationID,
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "status_event",
		Text:        fmt.Sprintf("diskspace status: %s — %s", overallLevel, statusDetails),
		State:       overallLevel,
		Data:        statusEvent,
	}); err != nil {
		logger.Warn("diskspace: failed to send status_event", "err", err)
	}

	return nil
}

// gatherFilesystems returns usage data for all monitored mount points.
func (svc *Service) gatherFilesystems() ([]FilesystemUsage, error) {
	mounts, err := readMounts()
	if err != nil {
		return nil, fmt.Errorf("diskspace: failed to read mounts: %w", err)
	}

	var results []FilesystemUsage
	seen := map[string]bool{}

	for _, mount := range mounts {
		if seen[mount] {
			continue
		}
		if !svc.isIncluded(mount) {
			continue
		}
		seen[mount] = true

		var stat syscall.Statfs_t
		if err := syscall.Statfs(mount, &stat); err != nil {
			svc.Deps.MustGetLogger().Warn("diskspace: statfs failed", "mount", mount, "err", err)
			continue
		}

		total := stat.Blocks * uint64(stat.Bsize) //nolint:unconvert
		free := stat.Bavail * uint64(stat.Bsize)  //nolint:unconvert
		used := total - free

		var usedPct, freePct float64
		if total > 0 {
			usedPct = float64(used) / float64(total) * 100
			freePct = float64(free) / float64(total) * 100
		}

		results = append(results, FilesystemUsage{
			Filesystem:  mount,
			TotalBytes:  total,
			UsedBytes:   used,
			FreeBytes:   free,
			UsedPercent: usedPct,
			FreePercent: freePct,
		})
	}

	return results, nil
}

// isIncluded returns true if the mount point should be monitored.
func (svc *Service) isIncluded(mount string) bool {
	for _, exc := range svc.excludes {
		if strings.EqualFold(mount, exc) || strings.HasPrefix(mount, exc) {
			return false
		}
	}
	if len(svc.includes) == 0 {
		return true
	}
	for _, inc := range svc.includes {
		if strings.EqualFold(mount, inc) {
			return true
		}
	}
	return false
}
