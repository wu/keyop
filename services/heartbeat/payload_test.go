package heartbeat

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHeartbeatEvent_Creation(t *testing.T) {
	heartbeatVal := HeartbeatEvent{
		Now:           time.Now().Round(time.Second),
		Uptime:        "1h2m3s",
		UptimeSeconds: 3723,
	}

	// Verify the heartbeat can be created and has expected values
	assert.Equal(t, "1h2m3s", heartbeatVal.Uptime)
	assert.Equal(t, int64(3723), heartbeatVal.UptimeSeconds)
	assert.NotNil(t, heartbeatVal.Now)
}

func TestHeartbeat_TypeAssertion(t *testing.T) {
	// Test that HeartbeatEvent works as a regular Go struct

	now := time.Now().Round(time.Second)
	heartbeatVal := HeartbeatEvent{
		Now:           now,
		Uptime:        "10s",
		UptimeSeconds: 10,
	}

	// Verify struct fields
	assert.Equal(t, "10s", heartbeatVal.Uptime)
	assert.Equal(t, int64(10), heartbeatVal.UptimeSeconds)
	assert.True(t, now.Equal(heartbeatVal.Now))
}
