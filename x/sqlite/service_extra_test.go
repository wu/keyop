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

// testProvider is a flexible SchemaProvider for tests. Set query/args to control
// what SQLiteInsert returns; leave them empty to simulate "nothing to insert".
type testProvider struct {
	schema       string
	query        string
	args         []any
	capturedMsgs []core.Message
}

func (p *testProvider) SQLiteSchema() string { return p.schema }
func (p *testProvider) SQLiteInsert(msg core.Message) (string, []any) {
	p.capturedMsgs = append(p.capturedMsgs, msg)
	return p.query, p.args
}

// newTestDeps builds a minimal Dependencies suitable for most tests.
// dbPath must be an absolute path; it is injected into the config.
func newTestDeps(t *testing.T, dbPath string) (core.Dependencies, core.ServiceConfig) {
	t.Helper()
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	deps.SetOsProvider(core.FakeOsProvider{})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	var capturedHandler func(core.Message) error
	messenger := testutil.NewFakeMessenger(
		testutil.WithSubscribeHook(func(_ context.Context, _, _, _, _ string, _ time.Duration, h func(core.Message) error) error {
			capturedHandler = h
			_ = capturedHandler // captured for later use; individual tests access via *Service.handleMessage directly
			return nil
		}),
	)
	deps.SetMessenger(messenger)

	cfg := core.ServiceConfig{
		Name: "sqlite-test",
		Type: "sqlite",
		Subs: map[string]core.ChannelInfo{
			"events": {Name: "test-events"},
		},
		Config: map[string]any{
			"dbPath": dbPath,
		},
	}
	return deps, cfg
}

// initService creates, initialises and registers cleanup for a Service backed by a
// temporary SQLite file. Returns the service and the captured message handler.
func initService(t *testing.T) (*Service, func(core.Message) error) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	deps.SetOsProvider(core.FakeOsProvider{})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	var capturedHandler func(core.Message) error
	messenger := testutil.NewFakeMessenger(
		testutil.WithSubscribeHook(func(_ context.Context, _, _, _, _ string, _ time.Duration, h func(core.Message) error) error {
			capturedHandler = h
			return nil
		}),
	)
	deps.SetMessenger(messenger)

	cfg := core.ServiceConfig{
		Name: "sqlite-test",
		Type: "sqlite",
		Subs: map[string]core.ChannelInfo{
			"events": {Name: "test-events"},
		},
		Config: map[string]any{"dbPath": dbPath},
	}

	svc := NewService(deps, cfg).(*Service)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	t.Cleanup(func() { _ = svc.db.Close() })
	return svc, capturedHandler
}

// ─── ValidateConfig ───────────────────────────────────────────────────────────

func TestValidateConfig_NoSubs(t *testing.T) {
	svc := &Service{
		Cfg:       core.ServiceConfig{},
		providers: make(map[string]SchemaProvider),
	}
	errs := svc.ValidateConfig()
	if len(errs) == 0 {
		t.Fatal("expected error when no subs configured, got none")
	}
}

func TestValidateConfig_WithSubs(t *testing.T) {
	svc := &Service{
		Cfg: core.ServiceConfig{
			Subs: map[string]core.ChannelInfo{
				"events": {Name: "all-events"},
			},
		},
		providers: make(map[string]SchemaProvider),
	}
	errs := svc.ValidateConfig()
	if len(errs) != 0 {
		t.Fatalf("expected no errors with valid config, got: %v", errs)
	}
}

// ─── AcceptsPayloadType ───────────────────────────────────────────────────────

func TestAcceptsPayloadType_NoConfig(t *testing.T) {
	svc := &Service{Cfg: core.ServiceConfig{}, providers: make(map[string]SchemaProvider)}
	if !svc.AcceptsPayloadType("anything.goes") {
		t.Error("expected true with nil config")
	}
}

func TestAcceptsPayloadType_StringFormat(t *testing.T) {
	svc := &Service{
		Cfg:       core.ServiceConfig{Config: map[string]any{"payloadTypes": "type.a,type.b"}},
		providers: make(map[string]SchemaProvider),
	}
	if !svc.AcceptsPayloadType("type.a") {
		t.Error("expected type.a to be accepted")
	}
	if !svc.AcceptsPayloadType("type.b") {
		t.Error("expected type.b to be accepted")
	}
	if svc.AcceptsPayloadType("type.c") {
		t.Error("expected type.c to be rejected")
	}
}

func TestAcceptsPayloadType_StringWithSpaces(t *testing.T) {
	svc := &Service{
		Cfg:       core.ServiceConfig{Config: map[string]any{"payloadTypes": "type.a, type.b"}},
		providers: make(map[string]SchemaProvider),
	}
	if !svc.AcceptsPayloadType("type.b") {
		t.Error("expected type.b to be accepted after trimming whitespace")
	}
}

func TestAcceptsPayloadType_SliceInterface(t *testing.T) {
	svc := &Service{
		Cfg: core.ServiceConfig{Config: map[string]any{
			"payloadTypes": []interface{}{"type.a", "type.b"},
		}},
		providers: make(map[string]SchemaProvider),
	}
	if !svc.AcceptsPayloadType("type.a") {
		t.Error("expected type.a to be accepted")
	}
	if svc.AcceptsPayloadType("type.c") {
		t.Error("expected type.c to be rejected")
	}
}

func TestAcceptsPayloadType_SliceString(t *testing.T) {
	svc := &Service{
		Cfg: core.ServiceConfig{Config: map[string]any{
			"payloadTypes": []string{"type.x", "type.y"},
		}},
		providers: make(map[string]SchemaProvider),
	}
	if !svc.AcceptsPayloadType("type.x") {
		t.Error("expected type.x to be accepted")
	}
	if svc.AcceptsPayloadType("type.z") {
		t.Error("expected type.z to be rejected")
	}
}

func TestAcceptsPayloadType_EmptyList(t *testing.T) {
	// Empty comma-separated string: split produces one empty token, no match.
	svc := &Service{
		Cfg:       core.ServiceConfig{Config: map[string]any{"payloadTypes": ""}},
		providers: make(map[string]SchemaProvider),
	}
	if svc.AcceptsPayloadType("type.a") {
		t.Error("expected type.a to be rejected for empty payloadTypes string")
	}

	// Empty []interface{}: no match.
	svc2 := &Service{
		Cfg: core.ServiceConfig{Config: map[string]any{
			"payloadTypes": []interface{}{},
		}},
		providers: make(map[string]SchemaProvider),
	}
	if svc2.AcceptsPayloadType("type.a") {
		t.Error("expected type.a to be rejected for empty []interface{} payloadTypes")
	}
}

func TestAcceptsPayloadType_UnknownType(t *testing.T) {
	// An integer is not a recognised payloadTypes format — fall back to permissive.
	svc := &Service{
		Cfg:       core.ServiceConfig{Config: map[string]any{"payloadTypes": 42}},
		providers: make(map[string]SchemaProvider),
	}
	if !svc.AcceptsPayloadType("anything") {
		t.Error("expected true for unknown config type (permissive fallback)")
	}
}

// ─── Check ────────────────────────────────────────────────────────────────────

func TestCheck_NilDB(t *testing.T) {
	svc := &Service{providers: make(map[string]SchemaProvider)}
	if err := svc.Check(); err == nil {
		t.Error("expected error from Check() before Initialize")
	}
}

func TestCheck_AfterInitialize(t *testing.T) {
	svc, _ := initService(t)
	if err := svc.Check(); err != nil {
		t.Errorf("expected nil from Check() after Initialize, got: %v", err)
	}
}

// ─── DB / GetSQLiteDB accessors ───────────────────────────────────────────────

func TestDB_ReturnsNil_BeforeInit(t *testing.T) {
	svc := &Service{providers: make(map[string]SchemaProvider)}
	if svc.DB() != nil {
		t.Error("expected DB() to return nil before Initialize")
	}
}

func TestDB_ReturnsDB_AfterInit(t *testing.T) {
	svc, _ := initService(t)
	if svc.DB() == nil {
		t.Error("expected DB() to return non-nil after Initialize")
	}
}

func TestGetSQLiteDB_ReturnsPointer(t *testing.T) {
	svc, _ := initService(t)
	ptr := svc.GetSQLiteDB()
	if ptr == nil {
		t.Fatal("expected GetSQLiteDB() to return non-nil")
	}
	if *ptr == nil {
		t.Error("expected **sql.DB to point to a non-nil *sql.DB after Initialize")
	}
}

// ─── handleMessage edge cases ─────────────────────────────────────────────────

func TestHandleMessage_EmptyDataType(t *testing.T) {
	svc, _ := initService(t)
	msg := core.Message{DataType: ""}
	if err := svc.handleMessage(msg); err != nil {
		t.Errorf("expected nil for empty DataType, got: %v", err)
	}
}

func TestHandleMessage_UnknownDataType(t *testing.T) {
	svc, _ := initService(t)
	msg := core.Message{DataType: "no.such.type"}
	if err := svc.handleMessage(msg); err != nil {
		t.Errorf("expected nil for unregistered DataType, got: %v", err)
	}
}

func TestHandleMessage_EmptyQuery(t *testing.T) {
	svc, _ := initService(t)

	// Provider returns empty query → no DB exec should be attempted.
	provider := &testProvider{
		schema: "CREATE TABLE noop (id INTEGER PRIMARY KEY)",
		query:  "",
		args:   nil,
	}
	svc.RegisterProvider("test.noop.v1", provider)

	// Run schema manually so the table exists.
	_, err := svc.db.Exec(provider.schema)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	msg := core.Message{DataType: "test.noop.v1", Event: "ignored"}
	if err := svc.handleMessage(msg); err != nil {
		t.Errorf("expected nil when provider returns empty query, got: %v", err)
	}

	var count int
	_ = svc.db.QueryRow("SELECT COUNT(*) FROM noop").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 rows after empty-query handling, got %d", count)
	}
}

func TestHandleMessage_TimestampNormalization(t *testing.T) {
	svc, _ := initService(t)

	// Create table for this test
	_, err := svc.db.Exec("CREATE TABLE ts_test (ts TEXT)")
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	provider := &testProvider{
		schema: "", // already created
		query:  "INSERT INTO ts_test (ts) VALUES (?)",
	}
	svc.RegisterProvider("test.ts.v1", provider)

	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("timezone unavailable, skipping")
	}
	nonUTC := time.Date(2024, 1, 15, 10, 0, 0, 0, loc)

	provider.args = []any{nonUTC.UTC().Format(time.RFC3339)}
	msg := core.Message{
		DataType:  "test.ts.v1",
		Timestamp: nonUTC,
	}

	if err := svc.handleMessage(msg); err != nil {
		t.Fatalf("handleMessage failed: %v", err)
	}

	// The provider should have received a UTC-normalised timestamp.
	if len(provider.capturedMsgs) == 0 {
		t.Fatal("provider did not receive message")
	}
	captured := provider.capturedMsgs[0]
	if captured.Timestamp.Location() != time.UTC {
		t.Errorf("expected UTC timestamp, got location: %s", captured.Timestamp.Location())
	}
}

// ─── Initialize error paths ───────────────────────────────────────────────────

func TestInitialize_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "new-subdir")
	dbPath := filepath.Join(subDir, "test.db")

	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	// Use a real OsProvider so MkdirAll actually creates the directory.
	deps.SetOsProvider(core.OsProvider{})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	messenger := testutil.NewFakeMessenger()
	deps.SetMessenger(messenger)

	cfg := core.ServiceConfig{
		Name:   "sqlite-test",
		Type:   "sqlite",
		Subs:   map[string]core.ChannelInfo{"ev": {Name: "ev"}},
		Config: map[string]any{"dbPath": dbPath},
	}
	svc := NewService(deps, cfg).(*Service)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	t.Cleanup(func() { _ = svc.db.Close() })

	if _, err := os.Stat(subDir); os.IsNotExist(err) {
		t.Error("expected Initialize to create the DB directory")
	}
}

func TestInitialize_SchemaError(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	deps, cfg := newTestDeps(t, dbPath)

	svc := NewService(deps, cfg).(*Service)
	// Register a provider with deliberately invalid SQL.
	svc.RegisterProvider("bad.schema.v1", &testProvider{
		schema: "THIS IS NOT VALID SQL !!!",
		query:  "",
	})

	err := svc.Initialize()
	if err == nil {
		_ = svc.db.Close()
		t.Fatal("expected Initialize to fail on invalid schema SQL, but it succeeded")
	}
}

// ─── RegisterProvider then HandleMessage ─────────────────────────────────────

func TestRegisterProvider_ThenHandleMessage(t *testing.T) {
	svc, _ := initService(t)

	provider := &testProvider{
		schema: "CREATE TABLE late_table (val TEXT)",
		query:  "INSERT INTO late_table (val) VALUES (?)",
		args:   []any{"registered-late"},
	}
	svc.RegisterProvider("late.type.v1", provider)

	// Manually run the schema since Initialize was called before registration.
	if _, err := svc.db.Exec(provider.schema); err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	msg := core.Message{DataType: "late.type.v1", Event: "insert"}
	if err := svc.handleMessage(msg); err != nil {
		t.Fatalf("handleMessage failed: %v", err)
	}

	var val string
	if err := svc.db.QueryRow("SELECT val FROM late_table").Scan(&val); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if val != "registered-late" {
		t.Errorf("expected 'registered-late', got %q", val)
	}
}
