package postgres

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"

	"github.com/lib/pq"
)

// TestConnectLeavesTheURLOutOfItsError: lib/pq parses DATABASE_URL with
// net/url, whose error quotes the whole URL and whose reason can quote a
// piece of it, so a password in it would reach the boot log (REQ-97). Used
// exactly as set, a URL with a line break after it is such an error. The
// error says what is wrong without any of the URL; one that is not about
// the URL is left as it is.
func TestConnectLeavesTheURLOutOfItsError(t *testing.T) {
	for dsn, want := range map[string]string{
		"postgres://openv:pw_do_not_log@127.0.0.1:1/db?sslmode=disable\n": "the database URL does not parse: it holds a control character, such as a line break",
		"postgres://openv:pw_do%zznot_log@127.0.0.1:1/db":                 "the database URL does not parse (the parser's reason is left out, since it can quote the password)",
		"postgres://openv:pw_do/not_log@127.0.0.1:1/db":                   "the database URL does not parse (the parser's reason is left out, since it can quote the password)",
	} {
		_, err := Connect(dsn)
		if err == nil || err.Error() != want {
			t.Errorf("Connect(%q) = %v, want %q", dsn, err, want)
		}
		if err != nil && (strings.Contains(err.Error(), "pw_do") || strings.Contains(err.Error(), "not_log")) {
			t.Errorf("Connect(%q)'s error quotes the password: %v", dsn, err)
		}
	}
	_, err := Connect("host=127.0.0.1 port=1 user=openv password=pw dbname=db sslmode=disable")
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("a refused connection should keep its own error, got %v", err)
	}
}

// TestConnectRefusesAURLItWouldNotReadAsOne: lib/pq reads a connection
// string as a URL only when it starts with postgres:// or postgresql://;
// anything else it reads as key=value settings, whose parse error quotes the
// whole URL, password included, or, with a query, whose first key is the URL
// up to its first '=', which it sends to the server at PGHOST or localhost
// and which that server's refusal quotes back. DATABASE_URL is used exactly
// as set (#379, question 24), so a space, a line break or a byte-order mark
// in front of it, as a secret pasted from a file can have, is such a string.
// Connect refuses each before anything is dialled, with a message that
// quotes none of it.
func TestConnectRefusesAURLItWouldNotReadAsOne(t *testing.T) {
	const inFront = "the database URL has spaces, a line break or another invisible character in front of it"
	const notAURL = "the database URL does not start with postgres:// or postgresql://"
	for dsn, want := range map[string]string{
		" postgres://openv:pw_do_not_log@127.0.0.1:1/db":                      inFront,
		" postgresql://openv:pw_do_not_log@127.0.0.1:1/db?sslmode=disable":    inFront,
		"\npostgres://openv:pw_do_not_log@127.0.0.1:1/db":                     inFront,
		"\t postgres://openv:pw_do_not_log@127.0.0.1:1/db?sslmode=disable\n":  inFront,
		"\ufeffpostgres://openv:pw_do_not_log@127.0.0.1:1/db":                 inFront,
		"\u200bpostgres://openv:pw_do_not_log@127.0.0.1:1/db?sslmode=disable": inFront,
		"POSTGRES://openv:pw_do_not_log@127.0.0.1:1/db?sslmode=disable":       notAURL,
		"DATABASE_URL=postgres://openv:pw_do_not_log@127.0.0.1:1/db":          notAURL,
		"postgis://openv:pw_do_not_log@127.0.0.1:1/db?sslmode=disable":        notAURL,
		"\"postgres://openv:pw_do_not_log@127.0.0.1:1/db?sslmode=disable\"":   notAURL,
	} {
		_, err := Connect(dsn)
		if err == nil || err.Error() != want {
			t.Errorf("Connect(%q) = %v, want %q", dsn, err, want)
		}
		if err != nil && (strings.Contains(err.Error(), "pw_do") || strings.Contains(err.Error(), "not_log")) {
			t.Errorf("Connect(%q)'s error quotes the password: %v", dsn, err)
		}
	}
	// A key=value string may hold :// inside a quoted value, as
	// ConnString's quoted password can, and is then read as it always was.
	_, err := Connect(ConnString("127.0.0.1", "1", "openv", "pw://postgres://x", "db"))
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("a key=value string with :// in its quoted password should be dialled and keep its own error, got %v", err)
	}
}

// TestConnectLeavesAKeyValueStringOutOfItsError: a key=value connection
// string that lib/pq cannot parse, such as one whose unquoted password has
// a space in it, is refused with an error that quotes the rest of the
// password (missing "=" after "do_not_log"), as a value lib/pq will not
// take is (connect_timeout's); the error Connect returns quotes none of it
// (REQ-97).
func TestConnectLeavesAKeyValueStringOutOfItsError(t *testing.T) {
	const want = "could not connect to the database (lib/pq's reason is left out, since it can quote the connection string, password included)"
	for _, dsn := range []string{
		"host=127.0.0.1 port=1 user=openv password=pw do_not_log dbname=db sslmode=disable",
		"host=127.0.0.1 port=1 user=openv password='pw_do_not_log dbname=db sslmode=disable",
		"host=127.0.0.1 port=1 user=openv password=pw connect_timeout=do_not_log",
	} {
		_, err := Connect(dsn)
		if err == nil || err.Error() != want {
			t.Errorf("Connect(%q) = %v, want %q", dsn, err, want)
		}
	}
}

// TestConnectErrorKeepsOnlyErrorsThatQuoteNoConnectionString: connectError
// passes on the server's own answer and a dial error, which name at most the
// host, port, user and database, and replaces every other error, which may
// quote the connection string. A *url.Error has a net.Error's methods, so it
// must be caught before them.
func TestConnectErrorKeepsOnlyErrorsThatQuoteNoConnectionString(t *testing.T) {
	const dsn = "postgres://openv:pw_do_not_log@db.example:5432/db"
	kept := []error{
		&pq.Error{Severity: "FATAL", Code: "28P01", Message: `password authentication failed for user "openv"`},
		&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")},
		fmt.Errorf("wrapped: %w", &net.DNSError{Err: "no such host", Name: "db.example"}),
		pq.ErrSSLNotSupported,
	}
	for _, err := range kept {
		if got := connectError(dsn, err); got != err {
			t.Errorf("connectError(%v) = %v, want it kept", err, got)
		}
	}
	for _, err := range []error{
		&url.Error{Op: "parse", URL: dsn, Err: errors.New("invalid port")},
		fmt.Errorf(`missing "=" after %q in connection info string"`, dsn),
		fmt.Errorf("pq: unsupported sslmode %q", "pw_do_not_log"),
		errors.New("something lib/pq may add one day: pw_do_not_log"),
	} {
		got := connectError(dsn, err)
		if got == err || strings.Contains(got.Error(), "pw_do_not_log") {
			t.Errorf("connectError(%v) = %v, want it replaced", err, got)
		}
	}
}
