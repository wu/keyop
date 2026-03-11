package idle

import (
	"fmt"
	"keyop/core"
	"keyop/core/testutil"
	"runtime"
	"strings"
	"testing"
	"time"

	"database/sql"

	"github.com/stretchr/testify/assert"
	_ "modernc.org/sqlite"
)

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
	fakeOs.CommandFunc = func(_ string, _ ...string) core.CommandApi {
		return &core.FakeCommand{
			CombinedOutputFunc: func() ([]byte, error) {
				return []byte(fmt.Sprintf(`    | | |   "HIDIdleTime" = %d`, idleNanos)), nil
			},
		}
	}

	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	messenger := testutil.NewFakeMessenger()
	deps.SetMessenger(messenger)
	stateStore := &mockStateStore{data: make(map[string]interface{})}
	deps.SetStateStore(stateStore)

	cfg := core.ServiceConfig{
		Name: "idle_test",
		Type: "idle",
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
	assertMessage(t, messenger.SentMessages, "idle_test", "active")
	assertEventData(t, messenger.SentMessages, 0, true) // active duration might be small but > 0
	assertMetric(t, messenger.SentMessages, "idle_test.idle_duration", 0)

	messenger.SentMessages = nil // reset

	// 2. Still active, below threshold
	idleNanos = 5 * 1e9 // 5 seconds
	err = svc.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	assertMetric(t, messenger.SentMessages, "idle_test.idle_duration", 5)
	// No alert should be sent
	for _, msg := range messenger.SentMessages {
		if msg.Event == "idle_alert" || msg.Event == "active_alert" {
			t.Errorf("Unexpected alert sent")
		}
	}

	messenger.SentMessages = nil

	// 3. Become Idle - exceeds 10s threshold
	idleNanos = 15 * 1e9 // 15 seconds
	err = svc.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	assertMessage(t, messenger.SentMessages, "idle_test", "idle")
	assertMetric(t, messenger.SentMessages, "idle_test.idle_duration", 15)

	// Verify state saved
	if stateStore.data["idle_test"] == nil {
		t.Errorf("State not saved after transition to idle")
	} else {
		state := stateStore.data["idle_test"].(ServiceState)
		if !state.IsIdle {
			t.Errorf("Saved state should be idle")
		}
	}

	messenger.SentMessages = nil

	// 4. Stay Idle
	idleNanos = 20 * 1e9 // 20 seconds
	err = svc.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	assertMetric(t, messenger.SentMessages, "idle_test.idle_duration", 20)
	// No new alert
	for _, msg := range messenger.SentMessages {
		if msg.Event == "idle_alert" {
			t.Errorf("Unexpected alert sent while already idle")
		}
	}

	messenger.SentMessages = nil

	// 5. Become Active again
	idleNanos = 0
	err = svc.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	assertMessage(t, messenger.SentMessages, "idle_test", "active")
	assertMetric(t, messenger.SentMessages, "idle_test.idle_duration", 0)

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
	messenger.SentMessages = nil
	idleNanos = 0
	err = svc2.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	// Should not trigger transition alert if it was already active in state
	for _, msg := range messenger.SentMessages {
		if msg.Event == "active_alert" {
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
	messenger := testutil.NewFakeMessenger()
	deps.SetMessenger(messenger)
	stateStore := &mockStateStore{data: make(map[string]interface{})}
	deps.SetStateStore(stateStore)

	cfg := core.ServiceConfig{
		Name: "idle_test_no_state",
		Type: "idle",
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
		if msg.Event == "idle_status" {
			var idleDur float64
			var activeDur float64

			if event, ok := core.AsType[*Event](msg.Data); ok {
				idleDur = event.IdleDurationSeconds
				activeDur = event.ActiveDurationSeconds
			} else if data, ok := msg.Data.(map[string]any); ok {
				if v, ok := data["idleDurationSeconds"].(float64); ok {
					idleDur = v
				} else if v, ok := data["idle_duration_seconds"].(float64); ok {
					idleDur = v
				}

				if v, ok := data["activeDurationSeconds"].(float64); ok {
					activeDur = v
				} else if v, ok := data["active_duration_seconds"].(float64); ok {
					activeDur = v
				}
			}

			if idleDur == idleSeconds {
				if checkActive {
					if activeDur >= 0 {
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

func TestSQLiteInsert(t *testing.T) {
	svc := &Service{hostname: "test-host"}

	t.Run("TypedPayload", func(t *testing.T) {
		msg := core.Message{
			Event:    "idle_status",
			Hostname: "test-host",
			Status:   "active",
			Data: &Event{
				IdleDurationSeconds:   1.2,
				ActiveDurationSeconds: 3.4,
			},
		}
		query, args := svc.SQLiteInsert(msg)
		assert.Contains(t, query, "INSERT INTO idle_events")
		assert.Equal(t, 5, len(args))
		assert.Equal(t, 1.2, args[3])
		assert.Equal(t, 3.4, args[4])
	})

	t.Run("MapPayload_CamelCase", func(t *testing.T) {
		msg := core.Message{
			Event:    "idle_status",
			Hostname: "test-host",
			Status:   "active",
			Data: map[string]any{
				"idleDurationSeconds":   5.6,
				"activeDurationSeconds": 7.8,
			},
		}
		query, args := svc.SQLiteInsert(msg)
		assert.Contains(t, query, "INSERT INTO idle_events")
		assert.Equal(t, 5.6, args[3])
		assert.Equal(t, 7.8, args[4])
	})

	t.Run("MapPayload_SnakeCase", func(t *testing.T) {
		msg := core.Message{
			Event:    "idle_status",
			Hostname: "test-host",
			Status:   "active",
			Data: map[string]any{
				"idle_duration_seconds":   9.1,
				"active_duration_seconds": 2.3,
			},
		}
		query, args := svc.SQLiteInsert(msg)
		assert.Contains(t, query, "INSERT INTO idle_events")
		assert.Equal(t, 9.1, args[3])
		assert.Equal(t, 2.3, args[4])
	})
}

func TestMaybeSendIdleReport(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("failed to close in-memory db: %v", err)
		}
	}()

	svc := &Service{
		db:       &db,
		hostname: "test-host",
		Deps:     core.Dependencies{
			// Set minimal deps if needed by logger/stateStore
		},
	}
	svc.Deps.SetLogger(&core.FakeLogger{})
	svc.Deps.SetStateStore(&mockStateStore{data: make(map[string]any)})

	schema := svc.SQLiteSchema()
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	now := time.Now()
	// Insert some data
	_, err = db.Exec(`INSERT INTO idle_events (timestamp, hostname, status, idle_seconds, active_seconds) VALUES (?, ?, ?, ?, ?)`,
		now.Add(-10*time.Minute), "test-host", "active", 0.0, 600.0)
	assert.NoError(t, err)
	_, err = db.Exec(`INSERT INTO idle_events (timestamp, hostname, status, idle_seconds, active_seconds) VALUES (?, ?, ?, ?, ?)`,
		now.Add(-5*time.Minute), "test-host", "idle", 300.0, 0.0)
	assert.NoError(t, err)

	messenger := testutil.NewFakeMessenger()

	t.Run("Last24Hours", func(t *testing.T) {
		md, err := svc.generateIdleReport(messenger, now, time.Time{}, time.Time{}, true)
		assert.NoError(t, err)
		assert.NotEmpty(t, md)
		assert.Contains(t, md, "test-host")
		assert.Contains(t, md, "Active periods")

		// Reports are now web-only; messenger should not receive an idle_report
		assert.Empty(t, messenger.SentMessages)
	})

	t.Run("CustomRange", func(t *testing.T) {
		messenger.Reset()

		// Insert data for the custom range
		start := time.Date(2026, 3, 9, 10, 0, 0, 0, time.Local)
		end := time.Date(2026, 3, 9, 12, 0, 0, 0, time.Local)
		_, err = db.Exec(`INSERT INTO idle_events (timestamp, hostname, status, idle_seconds, active_seconds) VALUES (?, ?, ?, ?, ?)`,
			start.Add(30*time.Minute), "test-host", "active", 0.0, 1800.0)
		assert.NoError(t, err)

		md, err := svc.generateIdleReport(messenger, now, start, end, true)
		assert.NoError(t, err)
		assert.NotEmpty(t, md)

		// Verify hourly activity labels and order
		label11 := "03-09 11:00"
		label12 := "03-09 12:00"
		assert.Contains(t, md, label11)
		assert.Contains(t, md, label12)

		// Most recent hour (12:00) should be before older hour (11:00)
		idx11 := strings.Index(md, label11)
		idx12 := strings.Index(md, label12)
		assert.True(t, idx12 < idx11, "12:00 label should appear before 11:00 label")
	})

	t.Run("NoDataInRange", func(t *testing.T) {
		// Use a fresh in-memory database to ensure no prior subtest data contaminates this range.
		messenger.Reset()
		db2, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("failed to open in-memory sqlite: %v", err)
		}
		defer func() {
			if err := db2.Close(); err != nil {
				t.Fatalf("failed to close in-memory db: %v", err)
			}
		}()

		svc2 := &Service{
			db:       &db2,
			hostname: "test-host",
			Deps:     core.Dependencies{},
		}
		svc2.Deps.SetLogger(&core.FakeLogger{})
		svc2.Deps.SetStateStore(&mockStateStore{data: make(map[string]any)})

		schema := svc2.SQLiteSchema()
		if _, err := db2.Exec(schema); err != nil {
			t.Fatalf("failed to create schema: %v", err)
		}

		start := now.Add(-100 * time.Hour)
		end := now.Add(-50 * time.Hour)
		md, err := svc2.generateIdleReport(messenger, now, start, end, true)
		assert.NoError(t, err)
		assert.Empty(t, md) // Should return empty if no data found
	})
}
