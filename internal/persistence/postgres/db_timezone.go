package postgres

import (
	"context"
	"database/sql/driver"
	"errors"

	"github.com/lib/pq"
)

// The session time zone every connection the server opens runs in (#379 bug
// 156). The schema keeps its times in TIMESTAMP (without time zone) columns
// holding UTC wall clocks, and the SQL stamps and compares them with NOW():
// a column's DEFAULT NOW(), SET heartbeat_at = NOW(), hosted_after <= NOW(),
// LEAST(NOW(), expires_at) and the like. NOW() is a TIMESTAMPTZ, and
// Postgres converts it to and from a TIMESTAMP in the session's TimeZone, so
// all of that was UTC only while the session's zone was: on a database
// server, database or role whose timezone setting is another zone, or with
// PGTZ set where the server runs, every such stamp was that zone's wall clock
// and every such comparison was off by its offset (in the agent runs alone:
// the claim's heartbeat, the retry's backoff and the launcher's grace window,
// the reaper's cutoff and a cancel's or a failure's finished_at, the queue's
// age and a runner lease's minutes). The session's zone is not left to
// those settings: it is set when the connection opens, after the role's and
// the database's defaults and PGTZ have applied, and overrides them.
const sessionTimeZone = `SET TIME ZONE 'UTC'`

// utcConnector opens the connections of Connect's pool: each is lib/pq's
// connection to dsn, opened as sql.Open("postgres", dsn) opens one, with its
// session time zone set to UTC before database/sql hands it out. The
// connection string is parsed when a connection is opened, as sql.Open
// leaves it, so a malformed one still fails at the first connect, where
// connectError keeps it out of the error.
type utcConnector struct {
	dsn string
}

// Connect opens one connection and sets its time zone. A connection whose
// time zone could not be set is closed, never handed out.
func (c utcConnector) Connect(ctx context.Context) (driver.Conn, error) {
	connector, err := pq.NewConnector(c.dsn)
	if err != nil {
		return nil, err
	}
	conn, err := connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	execer, ok := conn.(driver.ExecerContext)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("the database driver cannot set the session time zone")
	}
	if _, err := execer.ExecContext(ctx, sessionTimeZone, nil); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// Driver is lib/pq's.
func (utcConnector) Driver() driver.Driver { return &pq.Driver{} }
