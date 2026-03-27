package owntracks

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

func setupOwntracksServiceWithDB(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
	require.NoError(t, err)

	svc := &Service{}
	for _, stmt := range strings.Split(svc.SQLiteSchema(), ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
	svc.SetSQLiteDB(&db)

	t.Cleanup(func() { _ = db.Close() })
	return svc, db
}

func TestGetCurrentGPS_NilDB(t *testing.T) {
	svc := &Service{}
	result, err := svc.getCurrentGPS()
	require.NoError(t, err)
	assert.Equal(t, "ok", result["status"])
	assert.Nil(t, result["location"])
}

func TestGetCurrentGPS_NoData(t *testing.T) {
	svc, _ := setupOwntracksServiceWithDB(t)
	result, err := svc.getCurrentGPS()
	require.NoError(t, err)
	assert.Equal(t, "ok", result["status"])
	assert.Nil(t, result["location"])
	events, ok := result["events"].([]map[string]any)
	assert.True(t, ok)
	assert.Empty(t, events)
}

func TestGetCurrentGPS_WithData(t *testing.T) {
	svc, db := setupOwntracksServiceWithDB(t)

	ts := time.Now().UTC().Truncate(time.Second)
	_, err := db.Exec(
		`INSERT INTO gps_locations (timestamp, device, lat, lon, alt, acc, batt, raw) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ts, "myphone", 37.7749, -122.4194, 10.5, 5.0, 80.0, "{}",
	)
	require.NoError(t, err)

	result, err := svc.getCurrentGPS()
	require.NoError(t, err)
	assert.Equal(t, "ok", result["status"])
	location, ok := result["location"].(map[string]any)
	require.True(t, ok, "location should be a map")
	assert.Equal(t, "myphone", location["device"])
	assert.InDelta(t, 37.7749, location["lat"], 0.0001)
	assert.InDelta(t, -122.4194, location["lon"], 0.0001)
	assert.InDelta(t, 80.0, location["batt"], 0.0001)
}

func TestGetCurrentGPS_WithRegionEvents(t *testing.T) {
	svc, db := setupOwntracksServiceWithDB(t)

	ts := time.Now().UTC().Truncate(time.Second)
	_, err := db.Exec(
		`INSERT INTO gps_locations (timestamp, device, lat, lon, alt, acc, batt, raw) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ts, "myphone", 37.7749, -122.4194, 0, 0, 0, "{}",
	)
	require.NoError(t, err)

	_, err = db.Exec(
		`INSERT INTO gps_region_events (timestamp, device, event_type, region, lat, lon) VALUES (?, ?, ?, ?, ?, ?)`,
		ts, "myphone", "enter", "home", 37.7749, -122.4194,
	)
	require.NoError(t, err)

	result, err := svc.getCurrentGPS()
	require.NoError(t, err)
	assert.Equal(t, "ok", result["status"])

	events, ok := result["events"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, events, 1)
	assert.Equal(t, "home", events[0]["region"])
	assert.Equal(t, "enter", events[0]["event_type"])
	assert.Equal(t, "myphone", events[0]["device"])
}

func TestHandleWebUIAction_GetCurrent(t *testing.T) {
	svc, _ := setupOwntracksServiceWithDB(t)
	result, err := svc.HandleWebUIAction("get-current", nil)
	require.NoError(t, err)
	m, ok := result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ok", m["status"])
}

func TestHandleWebUIAction_GetMap_NilDB(t *testing.T) {
	svc := &Service{}
	result, err := svc.HandleWebUIAction("get-map", nil)
	require.NoError(t, err)
	m, ok := result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "", m["map"])
}

func TestHandleWebUIAction_GetHistoryMap_NilDB(t *testing.T) {
	svc := &Service{}
	result, err := svc.HandleWebUIAction("get-history-map", nil)
	require.NoError(t, err)
	m, ok := result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "", m["map"])
}

func TestHandleWebUIAction_Unknown(t *testing.T) {
	svc := &Service{}
	_, err := svc.HandleWebUIAction("unknown-action", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown action")
}

func TestGetHistoryMapData_NilDB(t *testing.T) {
	svc := &Service{}
	data, err := svc.getHistoryMapData()
	require.NoError(t, err)
	assert.Nil(t, data)
}

func TestGetHistoryMapData_NoPoints(t *testing.T) {
	svc, _ := setupOwntracksServiceWithDB(t)
	data, err := svc.getHistoryMapData()
	require.NoError(t, err)
	assert.Nil(t, data)
}

func TestIsTableNotFoundError(t *testing.T) {
	assert.False(t, isTableNotFoundError(nil))
}
