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

func TestEnvelope_HeartbeatEvent_RoundTrip(t *testing.T) {
	isolateRegistry(t)

	heartbeatVal := HeartbeatEvent{
		Now:           time.Now().Round(time.Second),
		Uptime:        "1h2m3s",
		UptimeSeconds: 3723,
	}

	env := core.NewEnvelope("heartbeat-chan", "test-source", heartbeatVal)
	if env.Headers == nil {
		env.Headers = make(map[string]string)
	}
	// Use canonical versioned type
	env.Headers["payload-type"] = "service.heartbeat.v1"

	// Marshal to JSON
	data, err := json.Marshal(env)
	require.NoError(t, err)

	// Unmarshal back to Envelope
	env2, err := core.UnmarshalEnvelope(data)
	require.NoError(t, err)

	// Register heartbeat type for this test
	err = core.RegisterPayload("service.heartbeat.v1", func() any { return &HeartbeatEvent{} })
	require.NoError(t, err, "Failed to register heartbeat payload")

	// Unmarshal payload
	typed, err := env2.UnmarshalPayload()
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

	env := core.NewEnvelope("heartbeat-chan", "test-source", heartbeatVal)
	if env.Headers == nil {
		env.Headers = make(map[string]string)
	}
	// Use legacy alias
	env.Headers["payload-type"] = "heartbeat"

	// Register heartbeat type for alias
	err := core.RegisterPayload("heartbeat", func() any { return &HeartbeatEvent{} })
	require.NoError(t, err, "Failed to register heartbeat alias")

	// Marshal/Unmarshal
	data, _ := json.Marshal(env)
	env2, _ := core.UnmarshalEnvelope(data)
	typed, err := env2.UnmarshalPayload()
	require.NoError(t, err)

	// Assert exact type
	typedHeartbeat, ok := typed.(*HeartbeatEvent)
	assert.True(t, ok, "Expected *HeartbeatEvent from alias, got %T", typed)
	assert.Equal(t, "alias-test", typedHeartbeat.Uptime)
}

func TestHeartbeat_LegacyCompatibility(t *testing.T) {
	// Simulate a legacy consumer that only knows about Message.Data
	// and expects it to be the heartbeat data.

	now := time.Now().Round(time.Second)
	heartbeatVal := HeartbeatEvent{
		Now:           now,
		Uptime:        "10s",
		UptimeSeconds: 10,
	}

	env := core.NewEnvelope("heartbeat", "source", heartbeatVal)

	// Convert to Message (this is what old consumers get)
	msg := env.ToMessage()

	// In the new system, if we wrap a struct in an envelope,
	// ToMessage might put it in Message.Data if it's not a Message struct itself.
	// NewEnvelopeFromMessage wraps m in Envelope.Payload.

	// Let's check what msg.Data contains.
	assert.NotNil(t, msg.Data)

	// If msg.Data is from json unmarshal of the envelope, it might be a map.
	// But here it's still the struct because we didn't marshal/unmarshal.

	h, ok := msg.Data.(HeartbeatEvent)
	if !ok {
		// Try pointer
		hp, ok := msg.Data.(*HeartbeatEvent)
		if ok {
			h = *hp
		} else {
			t.Fatalf("Expected HeartbeatEvent in msg.Data, got %T", msg.Data)
		}
	}

	assert.Equal(t, "10s", h.Uptime)
}
