package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// sqlRecorder is a database/sql driver that runs nothing and records what it
// is asked, in order: each transaction's begin (with its options and whether
// its context carries a deadline), every statement's text byte for byte,
// and each commit and rollback that reaches the driver. It answers a query
// that mints a ref (ensureRef's counter upsert) with one row holding 1, any
// other query with no rows, and an exec with one row affected. It fails the
// statement whose text contains failOn, the begin when failBegin is set, and
// the commit when failCommit is. It needs no server.
type sqlRecorder struct {
	mu         sync.Mutex
	events     []string
	failOn     string
	failBegin  bool
	failCommit bool
	// answers makes a query whose text contains a recAnswer's contains answer
	// one row of its values, ahead of the defaults.
	answers []recAnswer
}

// recAnswer is one row the recorder answers a query with.
type recAnswer struct {
	contains string
	values   []driver.Value
}

var (
	errRecordedStatement = errors.New("the recorder fails this statement")
	errRecordedBegin     = errors.New("the recorder fails the begin")
	errRecordedCommit    = errors.New("the recorder fails the commit")
)

func (r *sqlRecorder) record(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

// take answers what was recorded since the last take, and forgets it.
func (r *sqlRecorder) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	events := r.events
	r.events = nil
	return events
}

// recorderDB opens a handle on a new recorder, closed when the test ends.
func recorderDB(t *testing.T) (*sql.DB, *sqlRecorder) {
	t.Helper()
	rec := &sqlRecorder{}
	db := sql.OpenDB(recConnector{rec})
	t.Cleanup(func() { _ = db.Close() })
	return db, rec
}

type recConnector struct{ rec *sqlRecorder }

func (c recConnector) Connect(context.Context) (driver.Conn, error) { return &recConn{c.rec}, nil }
func (c recConnector) Driver() driver.Driver                        { return recDriver{} }

type recDriver struct{}

func (recDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("open the recorder through its connector")
}

type recConn struct{ rec *sqlRecorder }

func (c *recConn) Prepare(query string) (driver.Stmt, error) {
	return nil, fmt.Errorf("the recorder prepares nothing: %q", query)
}
func (c *recConn) Close() error { return nil }
func (c *recConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *recConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	_, deadline := ctx.Deadline()
	c.rec.record(fmt.Sprintf("BEGIN isolation=%d read-only=%t deadline=%t", opts.Isolation, opts.ReadOnly, deadline))
	if c.rec.failBegin {
		return nil, errRecordedBegin
	}
	return recTx{c.rec}, nil
}

func (c *recConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.rec.record("EXEC " + query)
	if c.rec.failOn != "" && strings.Contains(query, c.rec.failOn) {
		return nil, errRecordedStatement
	}
	return driver.RowsAffected(1), nil
}

func (c *recConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.rec.record("QUERY " + query)
	if c.rec.failOn != "" && strings.Contains(query, c.rec.failOn) {
		return nil, errRecordedStatement
	}
	for _, a := range c.rec.answers {
		if strings.Contains(query, a.contains) {
			return &recRows{values: [][]driver.Value{a.values}}, nil
		}
	}
	if strings.Contains(query, "RETURNING next_num - 1") {
		return &recRows{values: [][]driver.Value{{int64(1)}}}, nil
	}
	return &recRows{}, nil
}

type recTx struct{ rec *sqlRecorder }

func (tx recTx) Commit() error {
	tx.rec.record("COMMIT")
	if tx.rec.failCommit {
		return errRecordedCommit
	}
	return nil
}

func (tx recTx) Rollback() error {
	tx.rec.record("ROLLBACK")
	return nil
}

type recRows struct{ values [][]driver.Value }

func (r *recRows) Columns() []string {
	if len(r.values) == 0 {
		return nil
	}
	return make([]string, len(r.values[0]))
}
func (r *recRows) Close() error { return nil }
func (r *recRows) Next(dest []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(dest, r.values[0])
	r.values = r.values[1:]
	return nil
}

// wantEvents fails unless the recorder recorded exactly want, in order.
func wantEvents(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s sent\n  %q\nwant\n  %q", what, got, want)
	}
}

const (
	beginDefault         = "BEGIN isolation=0 read-only=false deadline=false"
	beginDefaultDeadline = "BEGIN isolation=0 read-only=false deadline=true"
)

// withTx commits what fn did when fn answers nil, and answers the commit's
// error as it came; it rolls back, and answers fn's error as it came, when fn
// fails; it rolls back and lets the panic go on when fn panics; and a begin
// that fails is answered as it came, with fn never called. The context and
// options reach the begin as given.
func TestWithTxCommitsOrRollsBack(t *testing.T) {
	db, rec := recorderDB(t)
	exec := func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE t SET x = 1`)
		return err
	}

	if err := withTx(context.Background(), db, nil, exec); err != nil {
		t.Fatalf("withTx: %v", err)
	}
	wantEvents(t, "a transaction whose fn succeeds", rec.take(), beginDefault, "EXEC UPDATE t SET x = 1", "COMMIT")

	ctx, cancel := context.WithTimeout(context.Background(), stmtTimeout)
	defer cancel()
	opts := &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}
	if err := withTx(ctx, db, opts, exec); err != nil {
		t.Fatalf("withTx with options: %v", err)
	}
	wantEvents(t, "a transaction with a deadline and options", rec.take(),
		fmt.Sprintf("BEGIN isolation=%d read-only=true deadline=true", sql.LevelRepeatableRead), "EXEC UPDATE t SET x = 1", "COMMIT")

	failed := errors.New("fn failed")
	if err := withTx(context.Background(), db, nil, func(tx *sql.Tx) error {
		if err := exec(tx); err != nil {
			return err
		}
		return failed
	}); err != failed {
		t.Errorf("withTx whose fn fails: %v, want fn's error as it came", err)
	}
	wantEvents(t, "a transaction whose fn fails", rec.take(), beginDefault, "EXEC UPDATE t SET x = 1", "ROLLBACK")

	rec.failOn = "UPDATE t"
	if err := withTx(context.Background(), db, nil, exec); err != errRecordedStatement {
		t.Errorf("withTx whose statement fails: %v, want the statement's error as it came", err)
	}
	wantEvents(t, "a transaction whose statement fails", rec.take(), beginDefault, "EXEC UPDATE t SET x = 1", "ROLLBACK")
	rec.failOn = ""

	func() {
		defer func() {
			if p := recover(); p != "fn panicked" {
				t.Errorf("withTx whose fn panics recovered %v, want the panic to go on", p)
			}
		}()
		_ = withTx(context.Background(), db, nil, func(tx *sql.Tx) error {
			if err := exec(tx); err != nil {
				return err
			}
			panic("fn panicked")
		})
	}()
	wantEvents(t, "a transaction whose fn panics", rec.take(), beginDefault, "EXEC UPDATE t SET x = 1", "ROLLBACK")

	rec.failCommit = true
	if err := withTx(context.Background(), db, nil, exec); err != errRecordedCommit {
		t.Errorf("withTx whose commit fails: %v, want the commit's error as it came", err)
	}
	// database/sql marks the transaction done before the driver's commit,
	// so the deferred rollback never reaches the driver.
	wantEvents(t, "a transaction whose commit fails", rec.take(), beginDefault, "EXEC UPDATE t SET x = 1", "COMMIT")
	rec.failCommit = false

	rec.failBegin = true
	called := false
	if err := withTx(context.Background(), db, nil, func(*sql.Tx) error { called = true; return nil }); err != errRecordedBegin {
		t.Errorf("withTx whose begin fails: %v, want the begin's error as it came", err)
	}
	if called {
		t.Error("withTx whose begin fails called fn")
	}
	wantEvents(t, "a transaction whose begin fails", rec.take(), beginDefault)
}

// db.Begin() is db.BeginTx(context.Background(), nil), so withTx with those
// begins a transaction exactly as the repositories' db.Begin() calls do.
func TestWithTxBeginsAsBeginDoes(t *testing.T) {
	db, rec := recorderDB(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	begin := rec.take()[0]
	if err := withTx(context.Background(), db, nil, func(*sql.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got := rec.take()[0]; got != begin {
		t.Errorf("withTx(context.Background(), db, nil, fn) began %q, db.Begin() %q", got, begin)
	}
}

// TestPrefixColumns qualifies each column of a list by the alias and joins
// them with the list's own separator, one column and several alike.
func TestPrefixColumns(t *testing.T) {
	for _, c := range []struct{ columns, alias, want string }{
		{"id", "x", "x.id"},
		{"id, title, created_at", "a", "a.id, a.title, a.created_at"},
	} {
		if got := prefixColumns(c.columns, c.alias); got != c.want {
			t.Errorf("prefixColumns(%q, %q) = %q, want %q", c.columns, c.alias, got, c.want)
		}
	}
}

// The literal tests the class E trailers name (link, interview and V&V)
// call the three helpers X13 folded into prefixColumns by their old names.
// Those tests are the characterization and stay byte for byte, so the names
// live here, in test code only, as one-line wrappers over prefixColumns.
func prefixedLinkColumns(alias string) string     { return prefixColumns(linkColumns, alias) }
func prefixedLiveLinkColumns(alias string) string { return prefixColumns(liveLinkColumns, alias) }
func prefixedInterviewSessionColumns(alias string) string {
	return prefixColumns(interviewSessionColumns, alias)
}
func prefixedResultColumns(alias string) string { return prefixColumns(resultColumns, alias) }
