package heartbeat

import (
	"encoding/json"
	"keyop/core"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func isolateRegistry(t *testing.T) core.PayloadRegistry {
	oldReg := core.GetPayloadRegistry()
	testReg := core.NewPayloadRegistry(nil)
	core.SetPayloadRegistry(testReg)
	t.Cleanup(func() {
		core.SetPayloadRegistry(oldReg)
	})
	return testReg
}

func TestHeartbeatEvent_RoundTrip(t *testing.T) {
	isolateRegistry(t)

	heartbeatVal := HeartbeatEvent{
		Now:           time.Now().Round(time.Second),
		Uptime:        "1h2m3s",
		UptimeSeconds: 3723,
	}

	// Register heartbeat type for this test
	err := core.RegisterPayload("service.heartbeat.v1", func() any { return &HeartbeatEvent{} })
	require.NoError(t, err, "Failed to register heartbeat payload")

	msg := core.Message{
		ChannelName: "heartbeat-chan",
		Hostname:    "test-source",
		DataType:    "service.heartbeat.v1",
		Data:        heartbeatVal,
	}

	// Marshal to JSON
	data, err := json.Marshal(msg)
	require.NoError(t, err)

	// Unmarshal back to Message
	msg2, err := core.UnmarshalMessage(data)
	require.NoError(t, err)

	// Decode typed payload
	reg := core.GetPayloadRegistry()
	typed, err := reg.Decode(msg2.DataType, msg2.Data)
	require.NoError(t, err)

	// Assert exact type
	typedHeartbeat, ok := typed.(*HeartbeatEvent)
	assert.True(t, ok, "Expected *HeartbeatEvent, got %T", typed)
	assert.Equal(t, heartbeatVal.Uptime, typedHeartbeat.Uptime)
	assert.Equal(t, heartbeatVal.UptimeSeconds, typedHeartbeat.UptimeSeconds)
	assert.True(t, heartbeatVal.Now.Equal(typedHeartbeat.Now))
}

func TestHeartbeat_AliasCompatibility(t *testing.T) {
	isolateRegistry(t)

	heartbeatVal := HeartbeatEvent{
		Now:           time.Now().Round(time.Second),
		Uptime:        "alias-test",
		UptimeSeconds: 42,
	}

	// Register heartbeat type for alias
	err := core.RegisterPayload("heartbeat", func() any { return &HeartbeatEvent{} })
	require.NoError(t, err, "Failed to register heartbeat alias")

	msg := core.Message{
		ChannelName: "heartbeat-chan",
		Hostname:    "test-source",
		DataType:    "heartbeat",
		Data:        heartbeatVal,
	}

	data, _ := json.Marshal(msg)
	msg2, _ := core.UnmarshalMessage(data)
	reg := core.GetPayloadRegistry()
	typed, err := reg.Decode(msg2.DataType, msg2.Data)
	require.NoError(t, err)

	// Assert exact type
	typedHeartbeat, ok := typed.(*HeartbeatEvent)
	assert.True(t, ok, "Expected *HeartbeatEvent from alias, got %T", typed)
	assert.Equal(t, "alias-test", typedHeartbeat.Uptime)
}

func TestHeartbeat_LegacyCompatibility(t *testing.T) {
	// Simulate a consumer receiving a heartbeat event via Message.Data.

	now := time.Now().Round(time.Second)
	heartbeatVal := HeartbeatEvent{
		Now:           now,
		Uptime:        "10s",
		UptimeSeconds: 10,
	}

	msg := core.Message{
		ChannelName: "heartbeat",
		Hostname:    "source",
		Data:        heartbeatVal,
	}

	assert.NotNil(t, msg.Data)

	h, ok := msg.Data.(HeartbeatEvent)
	if !ok {
		hp, ok := msg.Data.(*HeartbeatEvent)
		if ok {
			h = *hp
		} else {
			t.Fatalf("Expected HeartbeatEvent in msg.Data, got %T", msg.Data)
		}
	}

	assert.Equal(t, "10s", h.Uptime)
}
