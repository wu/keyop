package weatherstation

import (
	"database/sql"
	"keyop/core"
	"keyop/x/sqlite"
)

// Compile-time interface assertions.
var (
	_ sqlite.SchemaProvider    = (*Service)(nil)
	_ sqlite.Consumer          = (*Service)(nil)
	_ core.PayloadTypeProvider = (*Service)(nil)
)

// SQLiteSchema returns the DDL for storing weather readings.
func (svc *Service) SQLiteSchema() string {
	return `CREATE TABLE IF NOT EXISTS weather_readings (
		id               INTEGER PRIMARY KEY AUTOINCREMENT,
		recorded_at      DATETIME NOT NULL,
		barometer        REAL,
		barometer_rel    REAL,
		daily_rain       REAL,
		date_utc         TEXT,
		event_rain       REAL,
		frequency        TEXT,
		hourly_rain      REAL,
		out_humidity     INTEGER,
		in_humidity      INTEGER,
		max_daily_gust   REAL,
		model            TEXT,
		monthly_rain     REAL,
		rain_rate        REAL,
		solar_radiation  REAL,
		station_type     TEXT,
		out_temp         REAL,
		in_temp          REAL,
		total_rain       REAL,
		uv               INTEGER,
		weekly_rain      REAL,
		wh65_batt        INTEGER,
		wind_dir         INTEGER,
		wind_gust        REAL,
		wind_speed       REAL
	);`
}

// SQLiteInsert returns an INSERT statement for weatherstation messages.
func (svc *Service) SQLiteInsert(msg core.Message) (string, []any) {
	if msg.Event != "weatherstation" {
		return "", nil
	}

	ev, ok := core.AsType[core.WeatherStationEvent](msg.Data)
	if !ok {
		if evPtr, ok2 := core.AsType[*core.WeatherStationEvent](msg.Data); ok2 && evPtr != nil {
			ev = *evPtr
		} else {
			return "", nil
		}
	}

	return `INSERT INTO weather_readings (
		recorded_at, barometer, barometer_rel, daily_rain, date_utc,
		event_rain, frequency, hourly_rain, out_humidity, in_humidity,
		max_daily_gust, model, monthly_rain, rain_rate, solar_radiation,
		station_type, out_temp, in_temp, total_rain, uv,
		weekly_rain, wh65_batt, wind_dir, wind_gust, wind_speed
	) VALUES (
		?, ?, ?, ?, ?,
		?, ?, ?, ?, ?,
		?, ?, ?, ?, ?,
		?, ?, ?, ?, ?,
		?, ?, ?, ?, ?
	)`, []any{
			msg.Timestamp,
			ev.Barometer, ev.BarometerRel, ev.DailyRain, ev.DateUTC,
			ev.EventRain, ev.Frequency, ev.HourlyRain, ev.OutHumidity, ev.InHumidity,
			ev.MaxDailyGust, ev.Model, ev.MonthlyRain, ev.RainRate, ev.SolarRadiation,
			ev.StationType, ev.OutTemp, ev.InTemp, ev.TotalRain, ev.UV,
			ev.WeeklyRain, ev.Wh65Batt, ev.WindDir, ev.WindGust, ev.WindSpeed,
		}
}

// SetSQLiteDB receives the DB pointer from the runtime.
func (svc *Service) SetSQLiteDB(db **sql.DB) {
	svc.db = db
}

// PayloadTypes returns the payload types this provider handles.
func (svc *Service) PayloadTypes() []string {
	return []string{"weatherstation.event.v1", "weatherstation"}
}
