package idle

import (
	"database/sql"
	"fmt"
	"keyop/core"
	"keyop/core/testutil"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// --- helpers shared by extra tests ---

func newTestDeps(t *testing.T) (core.Dependencies, *testutil.FakeMessenger, *mockStateStore) {
	t.Helper()
	fakeOs := &core.FakeOsProvider{Host: "test-host"}
	messenger := testutil.NewFakeMessenger()
	store := &mockStateStore{data: make(map[string]interface{})}

	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	deps.SetMessenger(messenger)
	deps.SetStateStore(store)
	return deps, messenger, store
}

func newTestCfg(name string) core.ServiceConfig {
	return core.ServiceConfig{
		Name: name,
		Type: "idle",
		Config: map[string]interface{}{
			"threshold": "10s",
		},
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// --- ValidateConfig ---

func TestValidateConfig_NoThreshold(t *testing.T) {
	deps, _, _ := newTestDeps(t)
	cfg := core.ServiceConfig{
		Name:   "idle_vc",
		Type:   "idle",
		Config: map[string]interface{}{}, // no threshold
	}
	svc := NewService(deps, cfg).(*Service)
	errs := svc.ValidateConfig()
	// ValidateConfig only warns; it always returns nil errors
	assert.Nil(t, errs)
}

func TestValidateConfig_WithThreshold(t *testing.T) {
	deps, _, _ := newTestDeps(t)
	cfg := newTestCfg("idle_vc2")
	svc := NewService(deps, cfg).(*Service)
	errs := svc.ValidateConfig()
	assert.Nil(t, errs)
}

// --- SQLite provider methods ---

func TestSQLiteSchema(t *testing.T) {
	svc := &Service{}
	schema := svc.SQLiteSchema()
	assert.Contains(t, schema, "idle")
	assert.Contains(t, schema, "idle_events")
	assert.Contains(t, schema, "timestamp")
	assert.Contains(t, schema, "hostname")
	assert.Contains(t, schema, "status")
	assert.Contains(t, schema, "idle_seconds")
	assert.Contains(t, schema, "active_seconds")
}

func TestSQLiteSchema_Valid(t *testing.T) {
	db := openTestDB(t)
	svc := &Service{}
	_, err := db.Exec(svc.SQLiteSchema())
	assert.NoError(t, err, "schema should be valid SQL")
}

func TestPayloadTypes(t *testing.T) {
	svc := &Service{}
	types := svc.PayloadTypes()
	assert.Contains(t, types, "service.idle.v1")
	assert.Contains(t, types, "idle")
	assert.Len(t, types, 2)
}

func TestSetSQLiteDB(t *testing.T) {
	db := openTestDB(t)
	svc := &Service{}
	assert.Nil(t, svc.db)
	svc.SetSQLiteDB(&db)
	assert.NotNil(t, svc.db)
	assert.Equal(t, db, *svc.db)
}

func TestSQLiteInsert_NonIdleStatusEvent(t *testing.T) {
	svc := &Service{hostname: "test-host"}
	msg := core.Message{
		Event:  "active_alert", // not "idle_status"
		Status: "active",
	}
	query, args := svc.SQLiteInsert(msg)
	assert.Empty(t, query)
	assert.Nil(t, args)
}

func TestSQLiteInsert_UnrecognisedDataType(t *testing.T) {
	svc := &Service{hostname: "test-host"}
	msg := core.Message{
		Event:  "idle_status",
		Status: "active",
		Data:   "unexpected string type",
	}
	query, args := svc.SQLiteInsert(msg)
	assert.Empty(t, query)
	assert.Nil(t, args)
}

// --- WebUI methods ---

func TestWebUITab(t *testing.T) {
	svc := &Service{}
	tab := svc.WebUITab()
	assert.Equal(t, "idle", tab.ID)
	assert.NotEmpty(t, tab.Title)
	assert.NotEmpty(t, tab.Content)
	assert.Contains(t, tab.JSPath, "idle")
}

func TestWebUIPanels(t *testing.T) {
	svc := &Service{Cfg: newTestCfg("idle_panels")}
	panels := svc.WebUIPanels()
	require.Len(t, panels, 1)
	assert.Equal(t, "idle", panels[0].ID)
}

func TestHandleWebUIAction_Unknown(t *testing.T) {
	deps, _, _ := newTestDeps(t)
	svc := &Service{Deps: deps, Cfg: newTestCfg("idle_webui")}
	_, err := svc.HandleWebUIAction("no-such-action", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown action")
}

func newServiceWithDB(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	deps, _, _ := newTestDeps(t)
	db := openTestDB(t)
	svc := &Service{
		Deps:     deps,
		Cfg:      newTestCfg("idle_webui_db"),
		hostname: "test-host",
		db:       &db,
	}
	_, err := db.Exec(svc.SQLiteSchema())
	require.NoError(t, err)
	return svc, db
}

func TestHandleWebUIAction_FetchIdleReport(t *testing.T) {
	svc, db := newServiceWithDB(t)
	now := time.Now()
	_, err := db.Exec(
		`INSERT INTO idle_events (timestamp, hostname, status, idle_seconds, active_seconds) VALUES (?, ?, ?, ?, ?)`,
		now.Add(-5*time.Minute), "test-host", "active", 0.0, 300.0,
	)
	require.NoError(t, err)

	result, err := svc.HandleWebUIAction("fetch-idle-report", map[string]any{})
	assert.NoError(t, err)
	m, ok := result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ok", m["status"])
}

func TestHandleWebUIAction_FetchIdleDashboard(t *testing.T) {
	svc, _ := newServiceWithDB(t)
	svc.lastTransition = time.Now().Add(-1 * time.Minute)
	svc.isIdle = false

	result, err := svc.HandleWebUIAction("fetch-idle-dashboard", nil)
	assert.NoError(t, err)
	m, ok := result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ok", m["status"])
	data, ok := m["data"]
	require.True(t, ok)
	assert.NotNil(t, data)
}

func TestHandleWebUIAction_RefreshReport(t *testing.T) {
	svc, db := newServiceWithDB(t)
	now := time.Now()
	_, err := db.Exec(
		`INSERT INTO idle_events (timestamp, hostname, status, idle_seconds, active_seconds) VALUES (?, ?, ?, ?, ?)`,
		now.Add(-3*time.Minute), "test-host", "idle", 180.0, 0.0,
	)
	require.NoError(t, err)

	result, err := svc.HandleWebUIAction("refresh-report", map[string]any{})
	assert.NoError(t, err)
	m, ok := result.(map[string]string)
	require.True(t, ok)
	assert.Equal(t, "ok", m["status"])
}

func TestHandleWebUIAction_FetchIdleReport_WithTimeRange(t *testing.T) {
	svc, db := newServiceWithDB(t)
	ts := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	_, err := db.Exec(
		`INSERT INTO idle_events (timestamp, hostname, status, idle_seconds, active_seconds) VALUES (?, ?, ?, ?, ?)`,
		ts, "test-host", "active", 0.0, 600.0,
	)
	require.NoError(t, err)

	params := map[string]any{
		"start": ts.Add(-1 * time.Hour).Format(time.RFC3339),
		"end":   ts.Add(1 * time.Hour).Format(time.RFC3339),
	}
	result, err := svc.HandleWebUIAction("fetch-idle-report", params)
	assert.NoError(t, err)
	m, ok := result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ok", m["status"])
}

// --- localMidnight helper ---

func TestLocalMidnight_UTC(t *testing.T) {
	loc := time.UTC
	t1 := time.Date(2024, 6, 15, 14, 30, 45, 999, loc)
	result := localMidnight(t1)
	assert.Equal(t, time.Date(2024, 6, 15, 0, 0, 0, 0, loc), result)
}

func TestLocalMidnight_AlreadyMidnight(t *testing.T) {
	loc := time.UTC
	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)
	result := localMidnight(t1)
	assert.Equal(t, t1, result)
}

func TestLocalMidnight_FixedZone(t *testing.T) {
	// UTC-8 (PST-like offset)
	loc := time.FixedZone("PST", -8*3600)
	t1 := time.Date(2024, 11, 5, 23, 59, 59, 0, loc)
	result := localMidnight(t1)
	expected := time.Date(2024, 11, 5, 0, 0, 0, 0, loc)
	assert.Equal(t, expected, result)
}

func TestLocalMidnight_PreservesLocation(t *testing.T) {
	loc := time.FixedZone("UTC+5", 5*3600)
	t1 := time.Date(2024, 3, 20, 8, 15, 0, 0, loc)
	result := localMidnight(t1)
	assert.Equal(t, loc, result.Location())
	assert.Equal(t, 0, result.Hour())
	assert.Equal(t, 0, result.Minute())
	assert.Equal(t, 0, result.Second())
}

// --- Check() edge cases ---

func TestCheck_NonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Skipping non-darwin test on darwin platform")
	}
	deps, _, _ := newTestDeps(t)
	svc := NewService(deps, newTestCfg("idle_check_nd"))
	err := svc.Check()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "macOS")
}

func TestCheck_IoregsError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping test on non-darwin platform")
	}
	fakeOs := &core.FakeOsProvider{Host: "test-host"}
	fakeOs.CommandFunc = func(_ string, _ ...string) core.CommandApi {
		return &core.FakeCommand{
			CombinedOutputFunc: func() ([]byte, error) {
				return nil, fmt.Errorf("ioreg failed")
			},
		}
	}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	deps.SetMessenger(testutil.NewFakeMessenger())
	deps.SetStateStore(&mockStateStore{data: make(map[string]interface{})})

	svc := NewService(deps, newTestCfg("idle_check_err"))
	err := svc.Initialize()
	require.NoError(t, err)

	err = svc.Check()
	assert.Error(t, err)
}

func TestCheck_IoregsNoMatch(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping test on non-darwin platform")
	}
	fakeOs := &core.FakeOsProvider{Host: "test-host"}
	fakeOs.CommandFunc = func(_ string, _ ...string) core.CommandApi {
		return &core.FakeCommand{
			CombinedOutputFunc: func() ([]byte, error) {
				return []byte("no matching output here"), nil
			},
		}
	}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	deps.SetMessenger(testutil.NewFakeMessenger())
	deps.SetStateStore(&mockStateStore{data: make(map[string]interface{})})

	svc := NewService(deps, newTestCfg("idle_check_nm"))
	err := svc.Initialize()
	require.NoError(t, err)

	err = svc.Check()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HIDIdleTime")
}

// --- Initialize() with existing state ---

func TestInitialize_WithExistingState(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping test on non-darwin platform")
	}

	deps, _, store := newTestDeps(t)
	cfg := newTestCfg("idle_init_state")

	fixedTime := time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)
	fixedReportDay := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)
	savedState := ServiceState{
		IsIdle:         true,
		LastTransition: fixedTime,
		LastAlertHours: 3,
		LastReportDay:  fixedReportDay,
	}
	store.data[cfg.Name] = savedState

	svc := NewService(deps, cfg).(*Service)
	err := svc.Initialize()
	require.NoError(t, err)

	assert.True(t, svc.isIdle)
	assert.Equal(t, fixedTime, svc.lastTransition)
	assert.Equal(t, 3, svc.lastAlertHours)
	assert.Equal(t, fixedReportDay, svc.lastReportDay)
}

// --- Initialize() threshold defaults ---

func TestInitialize_DefaultThreshold(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping test on non-darwin platform")
	}
	deps, _, _ := newTestDeps(t)
	cfg := core.ServiceConfig{
		Name:   "idle_thresh_default",
		Type:   "idle",
		Config: map[string]interface{}{}, // no threshold
	}
	svc := NewService(deps, cfg).(*Service)
	err := svc.Initialize()
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, svc.threshold)
}

func TestInitialize_InvalidThreshold(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping test on non-darwin platform")
	}
	deps, _, _ := newTestDeps(t)
	cfg := core.ServiceConfig{
		Name: "idle_thresh_invalid",
		Type: "idle",
		Config: map[string]interface{}{
			"threshold": "not-a-duration",
		},
	}
	svc := NewService(deps, cfg).(*Service)
	err := svc.Initialize()
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, svc.threshold)
}

func TestInitialize_CustomMetricNames(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping test on non-darwin platform")
	}
	deps, _, _ := newTestDeps(t)
	cfg := core.ServiceConfig{
		Name: "idle_metric_names",
		Type: "idle",
		Config: map[string]interface{}{
			"threshold":          "10s",
			"idle_metric_name":   "custom.idle",
			"active_metric_name": "custom.active",
		},
	}
	svc := NewService(deps, cfg).(*Service)
	err := svc.Initialize()
	require.NoError(t, err)
	assert.Equal(t, "custom.idle", svc.idleMetricName)
	assert.Equal(t, "custom.active", svc.activeMetricName)
}

// --- Name and RegisterPayloads ---

func TestName(t *testing.T) {
	svc := &Service{}
	assert.Equal(t, "idle", svc.Name())
}

func TestRegisterPayloads(t *testing.T) {
	svc := &Service{}
	reg := core.NewPayloadRegistry(nil)
	err := svc.RegisterPayloads(reg)
	assert.NoError(t, err)
}

func TestRegisterPayloads_Duplicate(t *testing.T) {
	svc := &Service{}
	reg := core.NewPayloadRegistry(nil)
	// Register once, then again; should not return an error
	require.NoError(t, svc.RegisterPayloads(reg))
	err := svc.RegisterPayloads(reg)
	assert.NoError(t, err)
}

// --- Event PayloadType ---

func TestEventPayloadType(t *testing.T) {
	e := Event{}
	assert.Equal(t, "service.idle.v1", e.PayloadType())
}

// --- fetchIdleDashboard without DB ---

func TestFetchIdleDashboard_NoDB(t *testing.T) {
	deps, _, _ := newTestDeps(t)
	svc := &Service{
		Deps:           deps,
		Cfg:            newTestCfg("idle_dash_nodb"),
		isIdle:         false,
		lastTransition: time.Now().Add(-30 * time.Second),
	}
	result, err := svc.fetchIdleDashboard()
	assert.NoError(t, err)
	m, ok := result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ok", m["status"])
}

// --- generateIdleReport without DB ---

func TestGenerateIdleReport_NoDB(t *testing.T) {
	deps, messenger, _ := newTestDeps(t)
	svc := &Service{
		Deps: deps,
		Cfg:  newTestCfg("idle_report_nodb"),
	}
	md, report, err := svc.generateIdleReport(messenger, time.Now(), time.Time{}, time.Time{}, true)
	assert.NoError(t, err)
	assert.Empty(t, md)
	assert.Nil(t, report)
}

// --- formatHumanDuration edge cases ---

func TestFormatHumanDuration_SubSecond(t *testing.T) {
	// 400ms rounds down to 0s; 500ms rounds up to 1s (Round to nearest second)
	assert.Equal(t, "0s", formatHumanDuration(400*time.Millisecond))
	assert.Equal(t, "1s", formatHumanDuration(500*time.Millisecond))
}

func TestFormatHumanDuration_NegativeDuration(t *testing.T) {
	// Negative durations round toward zero; result should not panic
	result := formatHumanDuration(-5 * time.Second)
	assert.NotEmpty(t, result)
}

// --- WebUIAssets ---

func TestWebUIAssets(t *testing.T) {
	svc := &Service{}
	assets := svc.WebUIAssets()
	assert.NotNil(t, assets)
}

// --- localMidnight boundary ---

func TestLocalMidnight_EndOfYear(t *testing.T) {
	loc := time.UTC
	t1 := time.Date(2023, 12, 31, 23, 59, 59, 0, loc)
	result := localMidnight(t1)
	assert.Equal(t, time.Date(2023, 12, 31, 0, 0, 0, 0, loc), result)
}

// --- markdown output sanity check ---

func TestGenerateIdleReport_MarkdownContents(t *testing.T) {
	deps, messenger, _ := newTestDeps(t)
	db := openTestDB(t)
	svc := &Service{
		Deps:      deps,
		Cfg:       newTestCfg("idle_report_md"),
		hostname:  "test-host",
		db:        &db,
		threshold: 5 * time.Minute,
	}
	_, err := db.Exec(svc.SQLiteSchema())
	require.NoError(t, err)

	now := time.Now()
	_, err = db.Exec(
		`INSERT INTO idle_events (timestamp, hostname, status, idle_seconds, active_seconds) VALUES (?, ?, ?, ?, ?)`,
		now.Add(-10*time.Minute), "test-host", "active", 0.0, 600.0,
	)
	require.NoError(t, err)

	md, report, err := svc.generateIdleReport(messenger, now, time.Time{}, time.Time{}, true)
	assert.NoError(t, err)
	assert.NotEmpty(t, md)
	assert.NotNil(t, report)
	assert.True(t, strings.Contains(md, "Active periods"), "should contain 'Active periods'")
	assert.True(t, strings.Contains(md, "test-host"), "should contain hostname")
}
