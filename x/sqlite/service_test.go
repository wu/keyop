package sqlite

import (
	"context"
	"keyop/core"
	"keyop/core/testutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type mockProvider struct {
	schema string
}

func (m *mockProvider) SQLiteSchema() string {
	return m.schema
}

func (m *mockProvider) SQLiteInsert(msg core.Message) (string, []any) {
	if msg.Event == "test_event" {
		if msg.Status == "from chan1" || msg.Status == "from chan2" {
			return "INSERT INTO test_table_multi (val) VALUES (?)", []any{msg.Status}
		}
		return "INSERT INTO test_table (val) VALUES (?)", []any{msg.Status}
	}
	return "", nil
}

func TestSQLiteService(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sqlite-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	dbPath := filepath.Join(tmpDir, "test.db")

	deps := core.Dependencies{}
	logger := &core.FakeLogger{}
	deps.SetLogger(logger)

	fakeOs := core.FakeOsProvider{}
	deps.SetOsProvider(fakeOs)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	var capturedHandler func(core.Message) error
	messenger := testutil.NewFakeMessenger(testutil.WithSubscribeHook(func(_ context.Context, _, _, _, _ string, _ time.Duration, messageHandler func(core.Message) error) error {
		capturedHandler = messageHandler
		return nil
	}))
	deps.SetMessenger(messenger)

	cfg := core.ServiceConfig{
		Name: "sqlite-svc",
		Type: "sqlite",
		Subs: map[string]core.ChannelInfo{
			"events": {Name: "all-events"},
		},
		Config: map[string]any{
			"dbPath": dbPath,
		},
	}

	svc := NewService(deps, cfg).(*Service)

	provider := &mockProvider{
		schema: "CREATE TABLE test_table (id INTEGER PRIMARY KEY, val TEXT)",
	}
	svc.RegisterProvider("test-service", provider)

	if err := svc.Initialize(); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}
	defer func() { _ = svc.db.Close() }()

	if capturedHandler == nil {
		t.Fatal("handler not captured")
	}

	// Test message handling
	msg := core.Message{
		ServiceType: "test-service",
		Event:       "test_event",
		Status:      "hello sqlite",
	}

	if err := capturedHandler(msg); err != nil {
		t.Fatalf("handler failed: %v", err)
	}

	// Verify data in DB
	var val string
	err = svc.db.QueryRow("SELECT val FROM test_table").Scan(&val)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if val != "hello sqlite" {
		t.Errorf("expected 'hello sqlite', got '%s'", val)
	}

	// Test skip unregistered service
	msgSkip := core.Message{
		ServiceType: "unknown-service",
		Event:       "test_event",
		Status:      "skip me",
	}
	if err := capturedHandler(msgSkip); err != nil {
		t.Fatalf("handler failed on unknown service: %v", err)
	}

	var count int
	err = svc.db.QueryRow("SELECT COUNT(*) FROM test_table").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

func TestSQLiteService_MultipleSubs(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_multi.db")

	deps := core.Dependencies{}
	logger := &core.FakeLogger{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.FakeOsProvider{})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	handlers := make(map[string]func(core.Message) error)
	messenger := testutil.NewFakeMessenger(testutil.WithSubscribeHook(func(_ context.Context, _, channelName, _, _ string, _ time.Duration, messageHandler func(core.Message) error) error {
		handlers[channelName] = messageHandler
		return nil
	}))
	deps.SetMessenger(messenger)

	cfg := core.ServiceConfig{
		Name: "sqlite-svc",
		Type: "sqlite",
		Subs: map[string]core.ChannelInfo{
			"chan1": {Name: "channel-one"},
			"chan2": {Name: "channel-two"},
		},
		Config: map[string]any{
			"dbPath": dbPath,
		},
	}

	svc := NewService(deps, cfg).(*Service)
	provider := &mockProvider{
		schema: "CREATE TABLE test_table_multi (val TEXT)",
	}
	svc.RegisterProvider("test-service", provider)

	if err := svc.Initialize(); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}
	defer func() { _ = svc.db.Close() }()

	if len(handlers) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(handlers))
	}
	if handlers["channel-one"] == nil || handlers["channel-two"] == nil {
		t.Fatal("missing handler for one of the channels")
	}

	// Send message to first channel
	msg1 := core.Message{ServiceType: "test-service", Event: "test_event", Status: "from chan1"}
	if err := handlers["channel-one"](msg1); err != nil {
		t.Fatal(err)
	}

	// Send message to second channel
	msg2 := core.Message{ServiceType: "test-service", Event: "test_event", Status: "from chan2"}
	if err := handlers["channel-two"](msg2); err != nil {
		t.Fatal(err)
	}

	var count int
	_ = svc.db.QueryRow("SELECT COUNT(*) FROM test_table_multi").Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 rows, got %d", count)
	}
}
