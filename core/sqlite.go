package core

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// SQLiteDriverName is the database/sql driver name OpenSQLite connects with.
// It defaults to the name modernc.org/sqlite registers itself under. An
// application that registers a different or wrapped driver — for example the
// timing wrapper from InstrumentDriver — sets this during startup, before any
// service opens a database.
var SQLiteDriverName = "sqlite"

// ExpandHome replaces a leading ~ in path with the user's home directory.
// The path is returned unchanged if it does not start with ~ or the home
// directory cannot be determined.
func ExpandHome(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

// SQLiteDSN builds a connection string for modernc.org/sqlite that enables a
// busy timeout, WAL journaling, and NORMAL synchronous mode so concurrent
// writers wait for locks instead of failing immediately with SQLITE_BUSY.
// A leading ~ in dbPath is expanded, so callers may pass raw or
// pre-expanded paths interchangeably.
// modernc.org/sqlite only honors _pragma=name(value) query parameters;
// mattn-style parameters (_journal_mode, _timeout, ...) are silently ignored.
func SQLiteDSN(dbPath string) string {
	return fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", ExpandHome(dbPath))
}

// OpenSQLite opens dbPath with SQLiteDriverName and the standard DSN. Services
// should use it instead of calling sql.Open directly so that a single startup
// decision — plain driver or instrumented — applies to every database in the
// process. As with sql.Open, no connection is made until first use.
func OpenSQLite(dbPath string) (*sql.DB, error) {
	return sql.Open(SQLiteDriverName, SQLiteDSN(dbPath))
}
