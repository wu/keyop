package heartbeat

import (
	"fmt"
	"github.com/wu/keyop/core"
	"time"
)

// HeartbeatEvent represents a heartbeat from a service.
//
// Note: type name is intentionally HeartbeatEvent for clarity when used across tests
// and payload registration.
//
//nolint:revive
//goland:noinspection GoNameStartsWithPackageName
type HeartbeatEvent struct {
	Hostname      string    `json:"hostname"`
	Now           time.Time `json:"now"`
	Uptime        string    `json:"uptime"`
	UptimeSeconds int64     `json:"uptimeSeconds"`
}

// PayloadType returns the canonical payload type for heartbeat events.
func (h HeartbeatEvent) PayloadType() string { return "service.heartbeat.v1" }

// RegisterPayloadTypes registers heartbeat payload types with the new messenger.
func RegisterPayloadTypes(msgr core.MessengerApi, logger core.Logger) error {
	heartbeatProto := &HeartbeatEvent{}
	if err := msgr.RegisterPayloadType("service.heartbeat.v1", heartbeatProto); err != nil {
		if core.IsDuplicatePayloadRegistration(err) {
			logger.Warn("heartbeat: payload type already registered (skipping)", "type", "service.heartbeat.v1", "error", err)
			return nil
		}
		return fmt.Errorf("heartbeat: failed to register service.heartbeat.v1: %w", err)
	}

	return nil
}
