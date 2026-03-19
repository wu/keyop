package tides

import (
	"database/sql"
	"fmt"
	"keyop/core"
	"keyop/x/sqlite"
	"keyop/x/webui"
	"os"
	"path/filepath"
	"time"

	yaml "gopkg.in/yaml.v3"
)

// Compile-time interface assertions.
var (
	_ core.PayloadProvider  = (*Service)(nil)
	_ sqlite.SchemaProvider = (*Service)(nil)
	_ sqlite.Consumer       = (*Service)(nil)
	_ webui.TabProvider     = (*Service)(nil)
	_ webui.ActionProvider  = (*Service)(nil)
)

// SQLiteSchema returns the SQL DDL for the tables needed by the tides service.
func (svc *Service) SQLiteSchema() string {
	return `CREATE TABLE IF NOT EXISTS tide_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME,
		station_id TEXT,
		value REAL,
		state TEXT,
		next_peak_time TEXT,
		next_peak_value REAL,
		next_peak_type TEXT
	);
	CREATE TABLE IF NOT EXISTS tide_records (
		station_id TEXT,
		time TEXT,
		value REAL,
		fetched_at DATETIME,
		PRIMARY KEY (station_id, time)
	);`
}

// SQLiteInsert returns the SQL INSERT statement and the arguments to be used for a given message.
// This implementation requires a typed TideEvent payload and does not attempt
// to decode legacy map-based payloads.
func (svc *Service) SQLiteInsert(msg core.Message) (string, []any) {
	if msg.Event != "tide" {
		return "", nil
	}

	// Require typed TideEvent payloads only.
	if tePtr, ok := core.AsType[*TideEvent](msg.Data); ok && tePtr != nil {
		stationID := tePtr.StationID
		state := tePtr.State
		value := tePtr.Current.Value
		if value == 0 {
			value = msg.Metric
		}
		var npTime, npType string
		var npValue float64
		if tePtr.NextPeak != nil {
			npTime = tePtr.NextPeak.Time
			npValue = tePtr.NextPeak.Value
			npType = tePtr.NextPeak.Type
		}
		return `INSERT INTO tide_events (timestamp, station_id, value, state, next_peak_time, next_peak_value, next_peak_type) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			[]any{msg.Timestamp, stationID, value, state, npTime, npValue, npType}
	}

	// Support non-pointer typed value as well.
	if teVal, ok := core.AsType[TideEvent](msg.Data); ok {
		stationID := teVal.StationID
		state := teVal.State
		value := teVal.Current.Value
		if value == 0 {
			value = msg.Metric
		}
		var npTime, npType string
		var npValue float64
		if teVal.NextPeak != nil {
			npTime = teVal.NextPeak.Time
			npValue = teVal.NextPeak.Value
			npType = teVal.NextPeak.Type
		}
		return `INSERT INTO tide_events (timestamp, station_id, value, state, next_peak_time, next_peak_value, next_peak_type) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			[]any{msg.Timestamp, stationID, value, state, npTime, npValue, npType}
	}

	// Not a typed TideEvent — do not attempt legacy decoding.
	return "", nil
}

// SetSQLiteDB sets the database pointer to be used by the service.
func (svc *Service) SetSQLiteDB(db **sql.DB) {
	svc.db = db
}

// PayloadTypes returns the payload type names that this provider handles.
func (svc *Service) PayloadTypes() []string {
	return []string{"service.tide.v1", "tide"}
}

// loadDayFile reads the records for the given day from SQLite.
func (svc *Service) loadDayFile(day time.Time) (*TideDayFile, error) {
	if svc.db == nil || *svc.db == nil {
		// Fallback to legacy file logic if DB not available (optional, but good for transition)
		return svc.loadDayFileLegacy(day)
	}

	db := *svc.db
	dateStr := day.Format(fileDateFormat)
	rows, err := db.Query("SELECT time, value, fetched_at FROM tide_records WHERE station_id = ? AND time LIKE ?", svc.stationID, dateStr+"%")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			svc.Deps.MustGetLogger().Warn("tides: failed to close rows", "error", err)
		}
	}()

	var f TideDayFile
	f.StationID = svc.stationID
	f.Date = dateStr

	for rows.Next() {
		var r TideRecord
		var fetchedAt time.Time
		if err := rows.Scan(&r.Time, &r.Value, &fetchedAt); err != nil {
			return nil, err
		}
		if f.FetchedAt.Before(fetchedAt) {
			f.FetchedAt = fetchedAt
		}
		f.Records = append(f.Records, r)
	}

	if len(f.Records) == 0 {
		return nil, fmt.Errorf("no records found for %s", dateStr)
	}

	// NOAA records are usually sorted, but let's be sure.
	return &f, nil
}

// storeDayFile inserts records into SQLite.
func (svc *Service) storeDayFile(day time.Time, records []TideRecord, now time.Time) error {
	if svc.db == nil || *svc.db == nil {
		return svc.storeDayFileLegacy(day, records, now)
	}

	db := *svc.db
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			svc.Deps.MustGetLogger().Warn("tides: failed to rollback tx", "error", err)
		}
	}()

	stmt, err := tx.Prepare("INSERT OR REPLACE INTO tide_records (station_id, time, value, fetched_at) VALUES (?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer func() {
		if err := stmt.Close(); err != nil {
			svc.Deps.MustGetLogger().Warn("tides: failed to close stmt", "error", err)
		}
	}()

	for _, r := range records {
		if _, err := stmt.Exec(svc.stationID, r.Time, r.Value, now); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (svc *Service) loadDayFileLegacy(day time.Time) (*TideDayFile, error) {
	path := svc.dayFilePath(day)
	data, err := svc.Deps.MustGetOsProvider().ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f TideDayFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	return &f, nil
}

func (svc *Service) storeDayFileLegacy(day time.Time, records []TideRecord, now time.Time) error {
	logger := svc.Deps.MustGetLogger()

	f := TideDayFile{
		StationID: svc.stationID,
		Date:      day.Format(fileDateFormat),
		FetchedAt: now,
		Records:   records,
	}

	out, err := yaml.Marshal(f)
	if err != nil {
		return fmt.Errorf("tides: failed to marshal day file: %w", err)
	}

	path := svc.dayFilePath(day)
	// Ensure directory exists
	if err := svc.Deps.MustGetOsProvider().MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	file, err := svc.Deps.MustGetOsProvider().OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("tides: failed to open %s for writing: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			logger.Warn("tides: failed to close day file", "path", path, "error", closeErr)
		}
	}()

	if _, err := file.Write(out); err != nil {
		return fmt.Errorf("tides: failed to write %s: %w", path, err)
	}

	return nil
}
