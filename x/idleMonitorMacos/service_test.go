package idleMonitorMacos

import (
	"context"
	"fmt"
	"keyop/core"
	"runtime"
	"testing"
	"time"
)

type mockMessenger struct {
	messages []core.Message
}

func (m *mockMessenger) Send(msg core.Message) error {
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockMessenger) Subscribe(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
	return nil
}

func (m *mockMessenger) SubscribeExtended(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message, string, int64) error) error {
	return nil
}

func (m *mockMessenger) SetReaderState(channelName string, readerName string, fileName string, offset int64) error {
	return nil
}

func (m *mockMessenger) SeekToEnd(channelName string, readerName string) error {
	return nil
}

func (m *mockMessenger) SetDataDir(dir string) {}

func (m *mockMessenger) GetStats() core.MessengerStats {
	return core.MessengerStats{}
}

type mockStateStore struct {
	data map[string]interface{}
}

func (m *mockStateStore) Save(key string, value interface{}) error {
	m.data[key] = value
	return nil
}

func (m *mockStateStore) Load(key string, value interface{}) error {
	v, ok := m.data[key]
	if !ok {
		return fmt.Errorf("key not found")
	}
	// Simple copy for the test, assuming value is a pointer to the correct type
	switch val := value.(type) {
	case *ServiceState:
		*val = v.(ServiceState)
	default:
		return fmt.Errorf("unsupported type")
	}
	return nil
}

func TestCheck(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping test on non-darwin platform")
	}

	fakeOs := &core.FakeOsProvider{
		Host: "test-mac",
	}

	idleNanos := int64(0)
	fakeOs.CommandFunc = func(name string, arg ...string) core.CommandApi {
		return &core.FakeCommand{
			CombinedOutputFunc: func() ([]byte, error) {
				return []byte(fmt.Sprintf(`    | | |   "HIDIdleTime" = %d`, idleNanos)), nil
			},
		}
	}

	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	messenger := &mockMessenger{}
	deps.SetMessenger(messenger)
	stateStore := &mockStateStore{data: make(map[string]interface{})}
	deps.SetStateStore(stateStore)

	cfg := core.ServiceConfig{
		Name: "idle_test",
		Type: "idleMonitorMacos",
		Pubs: map[string]core.ChannelInfo{
			"metrics": {Name: "metrics_channel"},
			"alerts":  {Name: "alerts_channel"},
			"events":  {Name: "events_channel"},
		},
		Config: map[string]interface{}{
			"threshold": "10s",
		},
	}

	svc := NewService(deps, cfg)
	err := svc.Initialize()
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// 1. Initial check - Active
	idleNanos = 0
	err = svc.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	// Verify event and metrics
	assertMessage(t, messenger.messages, "events_channel", "active")
	assertEventData(t, messenger.messages, 0, true) // active duration might be small but > 0
	assertMetric(t, messenger.messages, "idle_test.idle_duration", 0)

	messenger.messages = nil // reset

	// 2. Still active, below threshold
	idleNanos = 5 * 1e9 // 5 seconds
	err = svc.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	assertMetric(t, messenger.messages, "idle_test.idle_duration", 5)
	// No alert should be sent
	for _, msg := range messenger.messages {
		if msg.ChannelName == "alerts_channel" {
			t.Errorf("Unexpected alert sent")
		}
	}

	messenger.messages = nil

	// 3. Become Idle - exceeds 10s threshold
	idleNanos = 15 * 1e9 // 15 seconds
	err = svc.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	assertMessage(t, messenger.messages, "alerts_channel", "idle")
	assertMetric(t, messenger.messages, "idle_test.idle_duration", 15)

	// Verify state saved
	if stateStore.data["idle_test"] == nil {
		t.Errorf("State not saved after transition to idle")
	} else {
		state := stateStore.data["idle_test"].(ServiceState)
		if !state.IsIdle {
			t.Errorf("Saved state should be idle")
		}
	}

	messenger.messages = nil

	// 4. Stay Idle
	idleNanos = 20 * 1e9 // 20 seconds
	err = svc.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	assertMetric(t, messenger.messages, "idle_test.idle_duration", 20)
	// No new alert
	for _, msg := range messenger.messages {
		if msg.ChannelName == "alerts_channel" {
			t.Errorf("Unexpected alert sent while already idle")
		}
	}

	messenger.messages = nil

	// 5. Become Active again
	idleNanos = 0
	err = svc.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	assertMessage(t, messenger.messages, "alerts_channel", "active")
	assertMetric(t, messenger.messages, "idle_test.idle_duration", 0)

	// Verify state saved
	state := stateStore.data["idle_test"].(ServiceState)
	if state.IsIdle {
		t.Errorf("Saved state should be active")
	}

	// 6. Verify initialization from state
	svc2 := NewService(deps, cfg)
	err = svc2.Initialize()
	if err != nil {
		t.Fatalf("Initialize failed for second service: %v", err)
	}
	// We need to access private fields or check behavior
	// Let's check if it starts as active (from state)
	messenger.messages = nil
	idleNanos = 0
	err = svc2.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	// Should not trigger transition alert if it was already active in state
	for _, msg := range messenger.messages {
		if msg.ChannelName == "alerts_channel" {
			t.Errorf("Unexpected alert sent after initialization from state")
		}
	}
}

func TestInitialize_NoState(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping test on non-darwin platform")
	}

	fakeOs := &core.FakeOsProvider{
		Host: "test-mac",
	}

	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	messenger := &mockMessenger{}
	deps.SetMessenger(messenger)
	stateStore := &mockStateStore{data: make(map[string]interface{})}
	deps.SetStateStore(stateStore)

	cfg := core.ServiceConfig{
		Name: "idle_test_no_state",
		Type: "idleMonitorMacos",
		Pubs: map[string]core.ChannelInfo{
			"metrics": {Name: "metrics_channel"},
			"alerts":  {Name: "alerts_channel"},
			"events":  {Name: "events_channel"},
		},
		Config: map[string]interface{}{
			"threshold": "10s",
		},
	}

	before := time.Now()
	svc := NewService(deps, cfg).(*Service)
	err := svc.Initialize()
	after := time.Now()

	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if svc.lastTransition.Before(before) || svc.lastTransition.After(after) {
		t.Errorf("lastTransition should be between %v and %v, but got %v", before, after, svc.lastTransition)
	}
}

func TestFormatHumanDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{0, "0s"},
		{5 * time.Second, "5s"},
		{65 * time.Second, "1m 5s"},
		{3600 * time.Second, "1h"},
		{3661 * time.Second, "1h 1m"},
		{24 * time.Hour, "1d"},
		{25 * time.Hour, "1d 1h"},
		{25*time.Hour + 30*time.Minute, "1d 1h"},
		{2*time.Hour + 30*time.Minute + 15*time.Second, "2h 30m"},
		{1*time.Minute + 15*time.Second, "1m 15s"},
	}

	for _, tt := range tests {
		t.Run(tt.duration.String(), func(t *testing.T) {
			actual := formatHumanDuration(tt.duration)
			if actual != tt.expected {
				t.Errorf("formatHumanDuration(%v) = %v; want %v", tt.duration, actual, tt.expected)
			}
		})
	}
}

func assertMessage(t *testing.T, messages []core.Message, channel string, status string) {
	t.Helper()
	found := false
	for _, msg := range messages {
		if msg.ChannelName == channel && msg.Status == status {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Message with status %s not found in channel %s", status, channel)
	}
}

func assertMetric(t *testing.T, messages []core.Message, metricName string, value float64) {
	t.Helper()
	found := false
	for _, msg := range messages {
		if msg.MetricName == metricName && msg.Metric == value {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Metric %s with value %v not found", metricName, value)
	}
}

func assertEventData(t *testing.T, messages []core.Message, idleSeconds float64, checkActive bool) {
	t.Helper()
	found := false
	for _, msg := range messages {
		if msg.ChannelName == "events_channel" {
			data, ok := msg.Data.(map[string]interface{})
			if !ok {
				continue
			}
			if data["idle_duration_seconds"] == idleSeconds {
				if checkActive {
					if _, ok := data["active_duration_seconds"].(float64); ok {
						found = true
						break
					}
				} else {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Errorf("Event data with idle_duration_seconds %v (checkActive: %v) not found", idleSeconds, checkActive)
	}
}
