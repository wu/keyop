package diskspace

import (
	"fmt"
	"keyop/core"
)

// FilesystemUsage holds disk space metrics for a single filesystem.
type FilesystemUsage struct {
	Filesystem  string  `json:"filesystem"`  // Mount point or device name
	TotalBytes  uint64  `json:"totalBytes"`  // Total capacity in bytes
	UsedBytes   uint64  `json:"usedBytes"`   // Bytes in use
	FreeBytes   uint64  `json:"freeBytes"`   // Bytes available
	UsedPercent float64 `json:"usedPercent"` // Percentage used (0–100)
	FreePercent float64 `json:"freePercent"` // Percentage free (0–100)
	Level       string  `json:"level"`       // "ok", "warning", or "critical"
}

// Event holds disk space metrics for all monitored filesystems.
type Event struct {
	Filesystems []FilesystemUsage `json:"filesystems"`
}

// PayloadType returns the canonical payload type identifier for Event.
func (e Event) PayloadType() string {
	return "diskspace.event.v1"
}

// Name returns the service name for the PayloadProvider interface.
func (svc *Service) Name() string {
	return svc.Cfg.Name
}

// RegisterPayloads registers the diskspace payload types with the provided registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	if err := reg.Register("diskspace.event.v1", func() any { return &Event{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("failed to register diskspace.event.v1: %w", err)
		}
	}
	return nil
}
