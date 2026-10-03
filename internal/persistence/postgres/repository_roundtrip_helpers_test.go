package postgres

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/lib/pq"
)

// Shared by the round trips of the team, work item, project, agent and member
// repositories (refactor plan S15b, OpenV REQ-23): each exported method is
// driven against a real Postgres (OPENV_TEST_DATABASE_URL), and what it
// answers is pinned as found, including where it looks wrong: every field
// read back, the time zone and precision a time comes back in, the order of
// each list and what breaks its ties, what a read answers for a row no one
// has (a sentinel, an error of the repository's own, or no row and no
// error), whether an empty list is nil (JSON null) or [] (JSON []), what a
// malformed id does, and what any other failure hands back. M12 splits
// repository and domain files by concern and narrows their interfaces, and
// X13 gives each repository a columns const and a scan function; these tests
// are what holds these five repositories still while they do.

// rtDB is a fresh database with the production schema.
func rtDB(t *testing.T) *sql.DB {
	t.Helper()
	db := testDB(t)
	initTestSchema(t, db)
	return db
}

// rtSeed runs one statement, failing the test on an error: the rows a
// repository reads but does not write (workspaces, accounts, people-teams),
// and rows no method of it could write.
func rtSeed(t *testing.T, db *sql.DB, query string, args ...interface{}) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("seed %q: %v", query, err)
	}
}

func rtSeedOrg(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	rtSeed(t, db, `INSERT INTO organizations (id, name, slug) VALUES ($1, 'Round trip', $2)`, id, "rt-"+id)
}

func rtSeedUser(t *testing.T, db *sql.DB, id, email, name, avatarURL string) {
	t.Helper()
	rtSeed(t, db, `INSERT INTO users (id, email, name, avatar_url, auth_provider, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'password', NOW(), NOW())`, id, email, name, avatarURL)
}

// The instants the round trips stamp rows with: fixed, a minute apart, each
// with nanoseconds a TIMESTAMP column cannot hold, and none of them half a
// microsecond, where Postgres's rounding (to even) and Go's (away from zero)
// would part.
var rtBase = time.Date(2026, 3, 14, 9, 26, 53, 123456789, time.UTC)

func rtAt(minutes int) time.Time {
	return rtBase.Add(time.Duration(minutes) * time.Minute)
}

// rtCEST is an offset a time.Now() on a server outside UTC would carry.
var rtCEST = time.FixedZone("CEST", 2*60*60)

// rtWantTimestamp fails unless got is what a TIMESTAMP (without time zone)
// column hands back for sent: sent's wall clock, its offset dropped rather
// than converted (Postgres ignores the offset lib/pq sends), rounded to the
// microsecond, in lib/pq's unnamed fixed zone of offset 0, which is not
// time.UTC.
func rtWantTimestamp(t *testing.T, what string, got, sent time.Time) {
	t.Helper()
	want := time.Date(sent.Year(), sent.Month(), sent.Day(), sent.Hour(), sent.Minute(), sent.Second(),
		sent.Nanosecond(), time.UTC).Round(time.Microsecond)
	if !got.Equal(want) {
		t.Errorf("%s read back as %s, want %s (the wall clock of %s, to the microsecond)",
			what, got.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano), sent.Format(time.RFC3339Nano))
	}
	if name, offset := got.Zone(); name != "" || offset != 0 || got.Location() == time.UTC {
		t.Errorf("%s read back in zone %q%+d (%v), want lib/pq's unnamed fixed zone at offset 0", what, name, offset, got.Location())
	}
}

// rtWantTimestamptz fails unless got is sent's instant, rounded to the
// microsecond, in time.UTC (a TIMESTAMPTZ column read through inUTC).
func rtWantTimestamptz(t *testing.T, what string, got *time.Time, sent time.Time) {
	t.Helper()
	want := sent.Round(time.Microsecond)
	if got == nil || !got.Equal(want) || got.Location() != time.UTC {
		t.Errorf("%s read back as %v, want %s in time.UTC", what, got, want.UTC().Format(time.RFC3339Nano))
	}
}

// rtWantPQ fails unless err is Postgres's own refusal, unwrapped or wrapped,
// with the SQLSTATE code (and, when given, the constraint).
func rtWantPQ(t *testing.T, what string, err error, code, constraint string) {
	t.Helper()
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || string(pqErr.Code) != code || (constraint != "" && pqErr.Constraint != constraint) {
		t.Errorf("%s: %v, want Postgres's refusal %s %s", what, err, code, constraint)
	}
}

// rtWantNil fails unless a list that matched nothing is nil, which the
// API's JSON encoding answers as null.
func rtWantNil[T any](t *testing.T, what string, got []T, err error) {
	t.Helper()
	if err != nil || got != nil {
		t.Errorf("%s: %#v, %v; want a nil list (JSON null) and no error", what, got, err)
	}
}

// rtWantEmpty fails unless a list that matched nothing is an empty,
// non-nil slice, which the API's JSON encoding answers as [].
func rtWantEmpty[T any](t *testing.T, what string, got []T, err error) {
	t.Helper()
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("%s: %#v (nil: %v), %v; want an empty, non-nil list (JSON []) and no error", what, got, got == nil, err)
	}
}

// rtWantRefused fails unless err is Postgres refusing text as a
// uuid, unwrapped or wrapped: the methods that do not answer a malformed id
// as one no row has hand the refusal back as it came.
func rtWantRefused(t *testing.T, what string, err error) {
	t.Helper()
	if !malformedID(err) {
		t.Errorf("%s: %v, want Postgres's refusal of a malformed uuid", what, err)
	}
}

// rtWantOrder fails unless got lists want's groups in order, the names within
// a group in any order: a group is the rows that tie on every key of the
// list's ORDER BY, which Postgres may hand back in any order, so a test pins
// which keys break a tie and that nothing else does, never the order of rows
// no key tells apart.
func rtWantOrder(t *testing.T, what string, got []string, want ...[]string) {
	t.Helper()
	at, ok := 0, true
	for _, group := range want {
		if at+len(group) > len(got) {
			ok = false
			break
		}
		g, w := append([]string(nil), got[at:at+len(group)]...), append([]string(nil), group...)
		sort.Strings(g)
		sort.Strings(w)
		ok = ok && reflect.DeepEqual(g, w)
		at += len(group)
	}
	if !ok || at != len(got) {
		t.Errorf("%s listed %q, want %q (the names in each inner group tie, in any order)", what, got, want)
	}
}

// rtJSON prints a value as the API would encode it, for a failure message:
// pointers followed, nil apart from empty.
func rtJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return err.Error()
	}
	return string(b)
}

// rtWantSame fails unless got and want are deeply equal, a nil slice or map
// apart from an empty one.
func rtWantSame(t *testing.T, what string, got, want interface{}) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s read back\n  %s\nwant\n  %s", what, rtJSON(got), rtJSON(want))
	}
}

// rtClosedDB is a handle whose every statement fails, "sql: database is
// closed", for what a method hands back when the database fails it for a
// reason that is not the id. It needs no server.
func rtClosedDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("postgres", "postgres://closed.invalid/none?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return db
}

// rtWantClosed fails unless err reads exactly prefix followed by the closed
// handle's error: handed back as it came, or wrapped by prefix.
func rtWantClosed(t *testing.T, what string, err error, prefix string) {
	t.Helper()
	if want := prefix + "sql: database is closed"; err == nil || err.Error() != want {
		t.Errorf("%s on a failing database: %v, want %q", what, err, want)
	}
}

// rtUnencodable is a JSON object value encoding/json refuses (NaN), for a
// write whose map the repository encodes before it runs a statement.
var rtUnencodable = map[string]interface{}{"x": math.NaN()}

// rtWantUnencodable fails unless err is the JSON encoder's refusal.
func rtWantUnencodable(t *testing.T, what string, err error) {
	t.Helper()
	var unsupported *json.UnsupportedValueError
	if !errors.As(err, &unsupported) {
		t.Errorf("%s of a value JSON cannot hold: %v, want the encoder's refusal", what, err)
	}
}
