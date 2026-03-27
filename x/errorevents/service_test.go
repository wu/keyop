package errorevents

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"keyop/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// openTestErrorsDB creates a temp SQLite DB with the errors schema applied.
func openTestErrorsDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
	require.NoError(t, err)

	svc := &Service{}
	// Execute CREATE TABLE and ALTER TABLE separately; ALTER TABLE may fail on
	// a fresh DB because seen is already defined in CREATE TABLE — that's fine.
	stmts := strings.SplitN(svc.SQLiteSchema(), ";", 2)
	_, err = db.Exec(stmts[0])
	require.NoError(t, err)
	// Ignore error from the ALTER TABLE migration statement on fresh DBs.
	if len(stmts) > 1 {
		_, _ = db.Exec(stmts[1])
	}

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTestDeps(t *testing.T) core.Dependencies {
	t.Helper()
	var deps core.Dependencies
	deps.SetLogger(&core.FakeLogger{})
	return deps
}

// insertError inserts a row directly and returns its id.
func insertError(t *testing.T, db *sql.DB, serviceName string, seen int) int64 {
	t.Helper()
	res, err := db.Exec(
		`INSERT INTO errors (timestamp, service_name, service_type, hostname, event, severity, summary, text, data, seen)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339), serviceName, "test-type", "host1",
		"event1", "error", "summary1", "text1", "{}", seen,
	)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

// ---------- non-DB tests ----------

func TestNewService(t *testing.T) {
	deps := newTestDeps(t)
	cfg := core.ServiceConfig{Name: "errorevents"}
	svc := NewService(deps, cfg)
	require.NotNil(t, svc)
	_, ok := svc.(*Service)
	assert.True(t, ok, "NewService should return *Service")
}

func TestCheck(t *testing.T) {
	svc := &Service{}
	assert.NoError(t, svc.Check())
}

func TestValidateConfig(t *testing.T) {
	svc := &Service{}
	errs := svc.ValidateConfig()
	assert.Nil(t, errs)
}

func TestPayloadTypes(t *testing.T) {
	svc := &Service{}
	types := svc.PayloadTypes()
	assert.Equal(t, []string{"core.error.v1", "error"}, types)
}

func TestSQLiteSchema(t *testing.T) {
	svc := &Service{}
	schema := svc.SQLiteSchema()
	assert.Contains(t, schema, "errors")
	assert.Contains(t, schema, "seen")
}

func TestSQLiteInsert(t *testing.T) {
	deps := newTestDeps(t)
	svc := &Service{Deps: deps}

	errEvent := core.ErrorEvent{
		Summary: "connection refused",
		Text:    "could not connect to DB",
		Level:   "error",
	}
	msg := core.Message{
		Timestamp:   time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		ServiceName: "db-monitor",
		ServiceType: "monitor",
		Hostname:    "server1",
		Event:       "db.connect",
		Text:        "db error",
		Data:        errEvent,
	}

	sqlStr, args := svc.SQLiteInsert(msg)

	assert.NotEmpty(t, sqlStr)
	assert.Contains(t, strings.ToUpper(sqlStr), "INSERT INTO ERRORS")
	require.Len(t, args, 9)

	// Data should be marshalled as JSON.
	dataArg, ok := args[8].(string)
	require.True(t, ok, "data arg should be a string")
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(dataArg), &decoded))
	assert.Equal(t, "connection refused", decoded["summary"])

	// Summary and severity extracted from ErrorEvent.
	assert.Equal(t, "connection refused", args[6]) // summary
	assert.Equal(t, "error", args[5])              // severity
}

func TestSQLiteInsert_NonErrorEventData(t *testing.T) {
	// SQLiteInsert always generates an INSERT; for non-ErrorEvent data
	// severity and summary will be empty strings.
	deps := newTestDeps(t)
	svc := &Service{Deps: deps}

	msg := core.Message{
		Timestamp:   time.Now(),
		ServiceName: "other-service",
		ServiceType: "other",
		Hostname:    "host",
		Event:       "something",
		Data:        map[string]string{"key": "value"},
	}

	sqlStr, args := svc.SQLiteInsert(msg)
	assert.NotEmpty(t, sqlStr)
	require.Len(t, args, 9)
	// severity and summary should be empty since data is not an ErrorEvent
	assert.Equal(t, "", args[5]) // severity
	assert.Equal(t, "", args[6]) // summary
}

func TestWebUITab(t *testing.T) {
	svc := &Service{}
	tab := svc.WebUITab()
	assert.Equal(t, "errors", tab.ID)
}

// ---------- DB-backed WebUI tests ----------

func TestHandleWebUIAction_FetchErrors(t *testing.T) {
	db := openTestErrorsDB(t)
	svc := &Service{Deps: newTestDeps(t)}
	svc.db = &db

	// Insert one unseen and one seen error.
	insertError(t, db, "service-a", 0)
	insertError(t, db, "service-b", 1)

	result, err := svc.HandleWebUIAction("fetch-errors", nil)
	require.NoError(t, err)

	m, ok := result.(map[string]any)
	require.True(t, ok)

	errorsRaw := m["errors"]
	require.NotNil(t, errorsRaw)

	// Verify via re-marshalling to get stable map access.
	raw, err := json.Marshal(errorsRaw)
	require.NoError(t, err)
	var decoded []map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Len(t, decoded, 1, "only unseen error should be returned")
	assert.Equal(t, float64(0), decoded[0]["seen"])
}

func TestHandleWebUIAction_MarkSeen(t *testing.T) {
	db := openTestErrorsDB(t)
	svc := &Service{Deps: newTestDeps(t)}
	svc.db = &db

	id := insertError(t, db, "service-a", 0)

	result, err := svc.HandleWebUIAction("mark-seen", map[string]any{
		"errorID": float64(id),
	})
	require.NoError(t, err)
	m, ok := result.(map[string]string)
	require.True(t, ok)
	assert.Equal(t, "ok", m["status"])

	var seen int
	require.NoError(t, db.QueryRow("SELECT seen FROM errors WHERE id = ?", id).Scan(&seen))
	assert.Equal(t, 1, seen)
}

func TestHandleWebUIAction_UnknownAction(t *testing.T) {
	svc := &Service{Deps: newTestDeps(t)}
	_, err := svc.HandleWebUIAction("nonexistent", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown action")
}
