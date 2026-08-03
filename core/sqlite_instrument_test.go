package core

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// The fakes below stand in for a SQL driver so the wrapper can be tested
// without depending on any particular database. legacyConn implements only the
// pre-context interfaces; ctxConn implements the context ones.

type fakeResult struct{}

func (fakeResult) LastInsertId() (int64, error) { return 1, nil }
func (fakeResult) RowsAffected() (int64, error) { return 1, nil }

type fakeRows struct{ done bool }

func (r *fakeRows) Columns() []string { return []string{"c"} }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = int64(42)
	return nil
}

// legacyStmt implements driver.Stmt and nothing else.
type legacyStmt struct{ fail bool }

func (s *legacyStmt) Close() error  { return nil }
func (s *legacyStmt) NumInput() int { return -1 }
func (s *legacyStmt) Exec([]driver.Value) (driver.Result, error) {
	if s.fail {
		return nil, errors.New("boom")
	}
	return fakeResult{}, nil
}
func (s *legacyStmt) Query([]driver.Value) (driver.Rows, error) { return &fakeRows{}, nil }

type legacyConn struct{ failExec bool }

func (c *legacyConn) Prepare(query string) (driver.Stmt, error) {
	return &legacyStmt{fail: c.failExec}, nil
}
func (c *legacyConn) Close() error              { return nil }
func (c *legacyConn) Begin() (driver.Tx, error) { return nil, errors.New("no transactions") }

// ctxStmt implements the context-aware statement interfaces.
type ctxStmt struct{}

func (s *ctxStmt) Close() error                               { return nil }
func (s *ctxStmt) NumInput() int                              { return -1 }
func (s *ctxStmt) Exec([]driver.Value) (driver.Result, error) { return fakeResult{}, nil }
func (s *ctxStmt) Query([]driver.Value) (driver.Rows, error)  { return &fakeRows{}, nil }
func (s *ctxStmt) ExecContext(context.Context, []driver.NamedValue) (driver.Result, error) {
	return fakeResult{}, nil
}
func (s *ctxStmt) QueryContext(context.Context, []driver.NamedValue) (driver.Rows, error) {
	return &fakeRows{}, nil
}

type ctxConn struct{}

func (c *ctxConn) Prepare(string) (driver.Stmt, error) { return &ctxStmt{}, nil }
func (c *ctxConn) Close() error                        { return nil }
func (c *ctxConn) Begin() (driver.Tx, error)           { return nil, errors.New("no transactions") }
func (c *ctxConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return fakeResult{}, nil
}
func (c *ctxConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &fakeRows{}, nil
}

type fakeDriver struct{ conn func() driver.Conn }

func (d *fakeDriver) Open(string) (driver.Conn, error) { return d.conn(), nil }

// timerRecord is one captured call to the query timer.
type timerRecord struct {
	db    string
	op    string
	stmt  string
	query string
	dur   time.Duration
	err   error
}

// captureTimer installs a recording timer and removes it when the test ends.
func captureTimer(t *testing.T) *[]timerRecord {
	t.Helper()
	var mu sync.Mutex
	var got []timerRecord
	SetSQLiteQueryTimer(func(db, op, stmt, query string, d time.Duration, err error) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, timerRecord{db: db, op: op, stmt: stmt, query: query, dur: d, err: err})
	})
	t.Cleanup(func() { SetSQLiteQueryTimer(nil) })
	return &got
}

func TestInstrumentDriverTimesContextPath(t *testing.T) {
	sql.Register("fake-ctx", InstrumentDriver(&fakeDriver{conn: func() driver.Conn { return &ctxConn{} }}))
	got := captureTimer(t)

	db, err := sql.Open("fake-ctx", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec("INSERT INTO t VALUES (?)", 1); err != nil {
		t.Fatalf("exec: %v", err)
	}
	rows, err := db.Query("SELECT c FROM t")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("rows close: %v", err)
	}

	if len(*got) != 2 {
		t.Fatalf("expected 2 timings, got %d: %+v", len(*got), *got)
	}
	if (*got)[0].op != "exec" || (*got)[0].stmt != "direct" {
		t.Errorf("exec: got op=%q stmt=%q, want exec/direct", (*got)[0].op, (*got)[0].stmt)
	}
	if (*got)[0].query != "INSERT INTO t VALUES (?)" {
		t.Errorf("exec query = %q", (*got)[0].query)
	}
	if (*got)[1].op != "query" || (*got)[1].stmt != "direct" {
		t.Errorf("query: got op=%q stmt=%q, want query/direct", (*got)[1].op, (*got)[1].stmt)
	}
}

// A driver without the context interfaces must still be timed: the wrapper
// reports ErrSkip and database/sql falls back to the prepared-statement path.
func TestInstrumentDriverTimesLegacyPath(t *testing.T) {
	sql.Register("fake-legacy", InstrumentDriver(&fakeDriver{conn: func() driver.Conn { return &legacyConn{} }}))
	got := captureTimer(t)

	db, err := sql.Open("fake-legacy", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec("DELETE FROM t"); err != nil {
		t.Fatalf("exec: %v", err)
	}

	if len(*got) != 1 {
		t.Fatalf("expected 1 timing, got %d: %+v", len(*got), *got)
	}
	if (*got)[0].op != "exec" || (*got)[0].stmt != "prepared" {
		t.Errorf("got op=%q stmt=%q, want exec/prepared", (*got)[0].op, (*got)[0].stmt)
	}
	if (*got)[0].query != "DELETE FROM t" {
		t.Errorf("query = %q", (*got)[0].query)
	}
}

func TestInstrumentDriverReportsErrors(t *testing.T) {
	sql.Register("fake-fail", InstrumentDriver(&fakeDriver{conn: func() driver.Conn {
		return &legacyConn{failExec: true}
	}}))
	got := captureTimer(t)

	db, err := sql.Open("fake-fail", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec("DELETE FROM t"); err == nil {
		t.Fatal("expected exec to fail")
	}

	if len(*got) != 1 {
		t.Fatalf("expected 1 timing, got %d", len(*got))
	}
	if (*got)[0].err == nil {
		t.Error("timing did not record the driver error")
	}
}

// With no timer installed the wrapper must stay transparent.
func TestInstrumentDriverWithoutTimer(t *testing.T) {
	sql.Register("fake-notimer", InstrumentDriver(&fakeDriver{conn: func() driver.Conn { return &ctxConn{} }}))
	SetSQLiteQueryTimer(nil)

	db, err := sql.Open("fake-notimer", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec("DELETE FROM t"); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

// Every timing must name the database it came from, on both the direct and the
// prepared path, so lines for the different service databases can be told apart.
func TestInstrumentDriverReportsDatabaseName(t *testing.T) {
	sql.Register("fake-dbname-ctx", InstrumentDriver(&fakeDriver{conn: func() driver.Conn { return &ctxConn{} }}))
	sql.Register("fake-dbname-legacy", InstrumentDriver(&fakeDriver{conn: func() driver.Conn { return &legacyConn{} }}))
	got := captureTimer(t)

	dsn := SQLiteDSN("/home/someone/.keyop/data/notes.db")
	for _, driverName := range []string{"fake-dbname-ctx", "fake-dbname-legacy"} {
		db, err := sql.Open(driverName, dsn)
		if err != nil {
			t.Fatalf("open %s: %v", driverName, err)
		}
		if _, err := db.Exec("DELETE FROM t"); err != nil {
			t.Fatalf("exec on %s: %v", driverName, err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close %s: %v", driverName, err)
		}
	}

	if len(*got) != 2 {
		t.Fatalf("expected 2 timings, got %d: %+v", len(*got), *got)
	}
	for _, rec := range *got {
		if rec.db != "notes.db" {
			t.Errorf("db = %q, want notes.db (stmt=%q)", rec.db, rec.stmt)
		}
	}
}

func TestDBNameFromDSN(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{"keyop dsn", SQLiteDSN("/home/someone/.keyop/data/notes.db"), "notes.db"},
		{"bare path", "/var/lib/keyop/tasks.db", "tasks.db"},
		{"relative path", "tasks.db", "tasks.db"},
		{"memory", ":memory:", ":memory:"},
		{"shared memory", "file::memory:?cache=shared", ":memory:"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dbNameFromDSN(tt.dsn); got != tt.want {
				t.Errorf("dbNameFromDSN(%q) = %q, want %q", tt.dsn, got, tt.want)
			}
		})
	}
}

func TestUnwrapExposesBaseConn(t *testing.T) {
	base := &ctxConn{}
	d := InstrumentDriver(&fakeDriver{conn: func() driver.Conn { return base }})
	c, err := d.Open("")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	u, ok := c.(interface{ Unwrap() driver.Conn })
	if !ok {
		t.Fatal("wrapped conn does not implement Unwrap")
	}
	if u.Unwrap() != base {
		t.Error("Unwrap did not return the base connection")
	}
}
