package core

import (
	"context"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// QueryTimerFunc receives one call per statement execution. db names the
// database the statement ran against (the DSN's file name, e.g. "notes.db"),
// op is "exec" or "query", stmt reports how the statement reached the driver
// ("direct" for the one-shot Execer/Queryer path, "prepared" for the
// prepared-statement path), query is the SQL text, d is the wall time spent
// inside the driver, and err is the driver's error, if any.
//
// Implementations run on the calling goroutine and must not block: every
// statement the application issues waits on them.
type QueryTimerFunc func(db, op, stmt, query string, d time.Duration, err error)

// queryTimer holds the installed timer, if any. An atomic pointer keeps the
// hot path lock-free; the nil case costs a single atomic load per statement.
var queryTimer atomic.Pointer[QueryTimerFunc]

// SetSQLiteQueryTimer installs fn as the timing sink for drivers wrapped by
// InstrumentDriver. Passing nil disables timing. It is safe to call at any
// time, including while queries are in flight.
func SetSQLiteQueryTimer(fn QueryTimerFunc) {
	if fn == nil {
		queryTimer.Store(nil)
		return
	}
	queryTimer.Store(&fn)
}

// record reports one statement execution to the installed timer.
func record(db, op, stmt, query string, start time.Time, err error) {
	fn := queryTimer.Load()
	if fn == nil {
		return
	}
	(*fn)(db, op, stmt, query, time.Since(start), err)
}

// dbNameFromDSN reduces a DSN to the database file's name, which is what
// identifies a database in a timing line — the directory is the same for every
// keyop database and the query parameters are identical. Non-file DSNs
// (":memory:", an empty name) are returned as they are.
func dbNameFromDSN(dsn string) string {
	name := strings.TrimPrefix(dsn, "file:")
	if i := strings.IndexByte(name, '?'); i >= 0 {
		name = name[:i]
	}
	if name == "" || !strings.ContainsRune(name, filepath.Separator) {
		return name
	}
	return filepath.Base(name)
}

// InstrumentDriver wraps base so that every statement it executes is reported
// to the function installed by SetSQLiteQueryTimer. The wrapper is driver
// agnostic — it forwards every optional database/sql/driver interface that base
// implements and emulates the rest — so the application decides which SQLite
// driver (or which database entirely) it is applied to:
//
//	sql.Register("keyop-sqlite", core.InstrumentDriver(base))
//
// Wrapping hides base's own connection type from sql.Conn.Raw. Callers that
// need the underlying connection can type-assert the wrapper to
// interface{ Unwrap() driver.Conn }.
func InstrumentDriver(base driver.Driver) driver.Driver {
	return &timingDriver{base: base}
}

type timingDriver struct {
	base driver.Driver
}

var _ driver.Driver = (*timingDriver)(nil)

func (d *timingDriver) Open(name string) (driver.Conn, error) {
	c, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return &timingConn{base: c, db: dbNameFromDSN(name)}, nil
}

type timingConn struct {
	base driver.Conn
	db   string
}

var (
	_ driver.Conn               = (*timingConn)(nil)
	_ driver.ConnBeginTx        = (*timingConn)(nil)
	_ driver.ConnPrepareContext = (*timingConn)(nil)
	_ driver.ExecerContext      = (*timingConn)(nil)
	_ driver.NamedValueChecker  = (*timingConn)(nil)
	_ driver.Pinger             = (*timingConn)(nil)
	_ driver.QueryerContext     = (*timingConn)(nil)
	_ driver.SessionResetter    = (*timingConn)(nil)
	_ driver.Validator          = (*timingConn)(nil)
)

// Unwrap returns the wrapped connection so callers can reach driver-specific
// extensions (backup, serialize, hooks) through sql.Conn.Raw.
func (c *timingConn) Unwrap() driver.Conn { return c.base }

func (c *timingConn) Prepare(query string) (driver.Stmt, error) {
	s, err := c.base.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &timingStmt{base: s, db: c.db, query: query}, nil
}

func (c *timingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	p, ok := c.base.(driver.ConnPrepareContext)
	if !ok {
		return c.Prepare(query)
	}
	s, err := p.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return &timingStmt{base: s, db: c.db, query: query}, nil
}

func (c *timingConn) Close() error { return c.base.Close() }

func (c *timingConn) Begin() (driver.Tx, error) { //nolint:staticcheck // required by driver.Conn
	return c.base.Begin() //nolint:staticcheck // forwarding the legacy method
}

func (c *timingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if b, ok := c.base.(driver.ConnBeginTx); ok {
		return b.BeginTx(ctx, opts)
	}
	// The wrapper implements ConnBeginTx unconditionally, so it has to reject
	// what a Begin-only driver cannot honor rather than silently downgrading.
	if opts.ReadOnly {
		return nil, errors.New("core: driver does not support read-only transactions")
	}
	if opts.Isolation != 0 { // 0 is sql.LevelDefault
		return nil, errors.New("core: driver does not support non-default isolation levels")
	}
	return c.Begin()
}

// ExecContext times the one-shot exec path. When the wrapped driver has no
// ExecerContext the wrapper reports ErrSkip, which sends database/sql down the
// prepared-statement path — still timed, by timingStmt.
func (c *timingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	e, ok := c.base.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	start := time.Now()
	res, err := e.ExecContext(ctx, query, args)
	record(c.db, "exec", "direct", query, start, err)
	return res, err
}

// QueryContext times the one-shot query path. The measurement ends when the
// driver returns its Rows, which for SQLite is after the statement is prepared
// and stepped to the first row — it does not include the caller's row scan.
func (c *timingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	q, ok := c.base.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	start := time.Now()
	rows, err := q.QueryContext(ctx, query, args)
	record(c.db, "query", "direct", query, start, err)
	return rows, err
}

func (c *timingConn) CheckNamedValue(nv *driver.NamedValue) error {
	if ck, ok := c.base.(driver.NamedValueChecker); ok {
		return ck.CheckNamedValue(nv)
	}
	return driver.ErrSkip // fall back to the default converter
}

func (c *timingConn) Ping(ctx context.Context) error {
	if p, ok := c.base.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

func (c *timingConn) ResetSession(ctx context.Context) error {
	if r, ok := c.base.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}

func (c *timingConn) IsValid() bool {
	if v, ok := c.base.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

type timingStmt struct {
	base  driver.Stmt
	db    string
	query string
}

var (
	_ driver.Stmt              = (*timingStmt)(nil)
	_ driver.StmtExecContext   = (*timingStmt)(nil)
	_ driver.StmtQueryContext  = (*timingStmt)(nil)
	_ driver.NamedValueChecker = (*timingStmt)(nil)
)

func (s *timingStmt) Close() error { return s.base.Close() }

func (s *timingStmt) NumInput() int { return s.base.NumInput() }

func (s *timingStmt) Exec(args []driver.Value) (driver.Result, error) { //nolint:staticcheck // required by driver.Stmt
	start := time.Now()
	res, err := s.base.Exec(args) //nolint:staticcheck // forwarding the legacy method
	record(s.db, "exec", "prepared", s.query, start, err)
	return res, err
}

func (s *timingStmt) Query(args []driver.Value) (driver.Rows, error) { //nolint:staticcheck // required by driver.Stmt
	start := time.Now()
	rows, err := s.base.Query(args) //nolint:staticcheck // forwarding the legacy method
	record(s.db, "query", "prepared", s.query, start, err)
	return rows, err
}

func (s *timingStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	e, ok := s.base.(driver.StmtExecContext)
	if !ok {
		vals, err := namedToValues(args)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return s.Exec(vals)
	}
	start := time.Now()
	res, err := e.ExecContext(ctx, args)
	record(s.db, "exec", "prepared", s.query, start, err)
	return res, err
}

func (s *timingStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	q, ok := s.base.(driver.StmtQueryContext)
	if !ok {
		vals, err := namedToValues(args)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return s.Query(vals)
	}
	start := time.Now()
	rows, err := q.QueryContext(ctx, args)
	record(s.db, "query", "prepared", s.query, start, err)
	return rows, err
}

func (s *timingStmt) CheckNamedValue(nv *driver.NamedValue) error {
	if ck, ok := s.base.(driver.NamedValueChecker); ok {
		return ck.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

// namedToValues supports the legacy Stmt path for drivers without the context
// variants. Named parameters cannot be expressed there.
func namedToValues(named []driver.NamedValue) ([]driver.Value, error) {
	vals := make([]driver.Value, 0, len(named))
	for _, nv := range named {
		if nv.Name != "" {
			return nil, errors.New("core: driver does not support named parameters")
		}
		vals = append(vals, nv.Value)
	}
	return vals, nil
}
