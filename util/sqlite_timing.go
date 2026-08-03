package util

import (
	"log/slog"
	"strings"
	"time"

	"github.com/wu/keyop/core"
)

// sqliteQueryLogPrefix names the timing log file: keyop-sqlite.YYYYMMDD.log.
const sqliteQueryLogPrefix = "keyop-sqlite"

// maxLoggedQueryLen caps the SQL text in a log line. Statements longer than
// this are truncated — the leading clauses identify the query well enough, and
// an unbounded field makes the file hard to read and grep.
const maxLoggedQueryLen = 500

// InitSQLiteQueryLog routes SQLite query timings to their own daily-rotated
// file in logDir, separate from the main keyop log, and installs the timer that
// feeds it. Timings are only produced for databases opened through a driver
// wrapped by core.InstrumentDriver; without such a driver this writer stays
// empty. The returned writer must be closed at shutdown.
func InitSQLiteQueryLog(logDir string) (*RotatingFileWriter, error) {
	rfw, err := NewRotatingFileWriterWithPrefix(logDir, sqliteQueryLogPrefix)
	if err != nil {
		return nil, err
	}

	// A dedicated handler, not a child of the application logger: that is what
	// keeps these lines out of the main log file. Level is fixed at Info so the
	// timing log is unaffected by KEYOP_LOG_DEBUG.
	logger := slog.New(slog.NewTextHandler(rfw, &slog.HandlerOptions{Level: slog.LevelInfo}))

	core.SetSQLiteQueryTimer(func(db, op, stmt, query string, d time.Duration, err error) {
		args := []any{
			"db", db,
			"op", op,
			"path", stmt,
			"duration_ms", float64(d.Microseconds()) / 1000,
			"query", flattenQuery(query),
		}
		if err != nil {
			args = append(args, "error", err)
		}
		logger.Info("sqlite query", args...)
	})

	return rfw, nil
}

// flattenQuery collapses the whitespace in a SQL statement so each timing entry
// occupies a single line, and truncates very long statements.
func flattenQuery(query string) string {
	flat := strings.Join(strings.Fields(query), " ")
	if len(flat) > maxLoggedQueryLen {
		return flat[:maxLoggedQueryLen] + "…"
	}
	return flat
}
