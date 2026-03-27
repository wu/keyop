package owntracks

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"keyop/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

func openTestOwntracksDB(t *testing.T) *sql.DB {
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

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSQLiteSchema(t *testing.T) {
	svc := &Service{}
	schema := svc.SQLiteSchema()
	assert.Contains(t, schema, "gps_locations")
	assert.Contains(t, schema, "gps_region_events")
}

func TestPayloadTypes(t *testing.T) {
	svc := &Service{}
	types := svc.PayloadTypes()
	assert.Len(t, types, 3)
	assert.Contains(t, types, "service.owntracks.location.v1")
	assert.Contains(t, types, "service.owntracks.enter.v1")
	assert.Contains(t, types, "service.owntracks.exit.v1")
}

func TestSetSQLiteDB(t *testing.T) {
	db := openTestOwntracksDB(t)
	svc := &Service{}
	svc.SetSQLiteDB(&db)
	assert.Equal(t, &db, svc.db)
}

func TestSQLiteInsert_GPS(t *testing.T) {
	svc := &Service{}
	msg := core.Message{
		Event:     "gps",
		Timestamp: time.Now(),
		Data: &LocationEvent{
			Device: "myphone",
			Lat:    37.7749,
			Lon:    -122.4194,
			Alt:    10.5,
			Acc:    5.0,
			Batt:   80.0,
		},
	}
	query, args := svc.SQLiteInsert(msg)
	assert.Contains(t, query, "INSERT INTO gps_locations")
	assert.Contains(t, query, "timestamp")
	require.Len(t, args, 8)
	assert.Equal(t, "myphone", args[1])
	assert.Equal(t, 37.7749, args[2])
	assert.Equal(t, -122.4194, args[3])
	assert.Equal(t, 10.5, args[4])
	assert.Equal(t, 5.0, args[5])
	assert.Equal(t, 80.0, args[6])

	// Verify against a real DB
	db := openTestOwntracksDB(t)
	_, err := db.Exec(query, args...)
	assert.NoError(t, err)
}

func TestSQLiteInsert_RegionEnter(t *testing.T) {
	svc := &Service{}
	msg := core.Message{
		Event:     "region_enter",
		Timestamp: time.Now(),
		Data: &LocationEnterEvent{
			Device: "myphone",
			Region: "home",
			Lat:    37.7749,
			Lon:    -122.4194,
		},
	}
	query, args := svc.SQLiteInsert(msg)
	assert.Contains(t, query, "INSERT INTO gps_region_events")
	assert.Contains(t, query, "'enter'")
	require.Len(t, args, 5)
	assert.Equal(t, "myphone", args[1])
	assert.Equal(t, "home", args[2])

	db := openTestOwntracksDB(t)
	_, err := db.Exec(query, args...)
	assert.NoError(t, err)
}

func TestSQLiteInsert_RegionExit(t *testing.T) {
	svc := &Service{}
	msg := core.Message{
		Event:     "region_exit",
		Timestamp: time.Now(),
		Data: &LocationExitEvent{
			Device: "myphone",
			Region: "work",
			Lat:    37.77,
			Lon:    -122.41,
		},
	}
	query, args := svc.SQLiteInsert(msg)
	assert.Contains(t, query, "INSERT INTO gps_region_events")
	assert.Contains(t, query, "'exit'")
	require.Len(t, args, 5)
	assert.Equal(t, "myphone", args[1])
	assert.Equal(t, "work", args[2])

	db := openTestOwntracksDB(t)
	_, err := db.Exec(query, args...)
	assert.NoError(t, err)
}

func TestSQLiteInsert_UnknownEvent(t *testing.T) {
	svc := &Service{}
	msg := core.Message{
		Event: "unknown_event",
		Data:  map[string]any{"foo": "bar"},
	}
	query, args := svc.SQLiteInsert(msg)
	assert.Equal(t, "", query)
	assert.Nil(t, args)
}

func TestSQLiteInsert_GPS_NilPayload(t *testing.T) {
	svc := &Service{}
	msg := core.Message{
		Event: "gps",
		Data:  nil,
	}
	query, args := svc.SQLiteInsert(msg)
	assert.Equal(t, "", query)
	assert.Nil(t, args)
}

func TestSQLiteInsert_RegionEnter_NilPayload(t *testing.T) {
	svc := &Service{}
	msg := core.Message{
		Event: "region_enter",
		Data:  nil,
	}
	query, args := svc.SQLiteInsert(msg)
	assert.Equal(t, "", query)
	assert.Nil(t, args)
}

func TestSQLiteInsert_RegionExit_NilPayload(t *testing.T) {
	svc := &Service{}
	msg := core.Message{
		Event: "region_exit",
		Data:  nil,
	}
	query, args := svc.SQLiteInsert(msg)
	assert.Equal(t, "", query)
	assert.Nil(t, args)
}
