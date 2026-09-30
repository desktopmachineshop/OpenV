package postgres

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"unicode"

	"github.com/lib/pq"
)

// ConnString builds lib/pq's key/value connection string from its parts,
// with sslmode=disable, the server's local-development connection when
// DATABASE_URL is unset. Each value is quoted, so that it reaches the
// database exactly as given (#379, question 24: a credential is used exactly
// as set): unquoted, lib/pq ends a value at its first space, drops the
// spaces and line breaks around it and reads a backslash as an escape, so a
// password with any of them was cut, changed or broke the string, and could
// add a key of its own.
func ConnString(host, port, user, password, dbname string) string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		connValue(host), connValue(port), connValue(user), connValue(password), connValue(dbname))
}

// connValue quotes one value of a key/value connection string, escaping a
// backslash and a single quote with a backslash, as libpq and lib/pq read it.
func connValue(v string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
}

// openDB opens lib/pq's pool on dsn for Connect, after refusing a
// connection string that holds a URL lib/pq would not read as one, before
// anything is dialled (urlShapeError), so the password in it reaches no log
// (REQ-97).
func openDB(dsn string) (*sql.DB, error) {
	if err := urlShapeError(dsn); err != nil {
		return nil, err
	}
	return sql.Open("postgres", dsn)
}

// urlShapeError refuses, before anything is dialled, a connection string
// that holds a URL lib/pq would not read as one (REQ-97). lib/pq reads a
// URL only when it starts with postgres:// or postgresql://, and anything
// else as key=value settings: a URL then either fails to parse, with an
// error that quotes all of it, password included, or, with a query, has
// everything up to its first '=' read as a key, which lib/pq sends to the
// server at PGHOST or localhost, whose refusal quotes it back. DATABASE_URL
// is used exactly as set (#379, question 24), so a space, a line break or a
// byte-order mark in front of it, as a secret pasted from a file can have,
// makes such a string; so do a scheme in capitals, the variable's name and
// '=' in front of the URL, and a scheme lib/pq does not know. A key=value
// string that lib/pq reads never has :// before its first '=', and one
// with postgres:// before its first quoted value is a URL set by mistake;
// ConnString quotes every value, so none it builds is refused.
func urlShapeError(dsn string) error {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return nil
	}
	if t := strings.TrimLeftFunc(dsn, invisible); t != dsn &&
		(strings.HasPrefix(t, "postgres://") || strings.HasPrefix(t, "postgresql://")) {
		return errors.New("the database URL has spaces, a line break or another invisible character in front of it")
	}
	unquoted, _, _ := strings.Cut(dsn, "'")
	unquoted = strings.ToLower(unquoted)
	scheme, eq := strings.Index(dsn, "://"), strings.IndexByte(dsn, '=')
	if strings.Contains(unquoted, "postgres://") || strings.Contains(unquoted, "postgresql://") ||
		(scheme >= 0 && (eq < 0 || scheme < eq)) {
		return errors.New("the database URL does not start with postgres:// or postgresql://")
	}
	return nil
}

// invisible reports a rune an operator cannot see in front of a pasted
// value: a space or line break, another control character, or a format
// character such as a byte-order mark or a zero-width space.
func invisible(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
}

// connectError keeps the connection string out of a connect error, since
// it may hold the password (REQ-97). It passes on only an error that
// quotes none of it beyond the host, port, user and database: the server's
// own answer (*pq.Error), a dial error, a TLS certificate error, and
// lib/pq's fixed messages. Every other error is replaced, since lib/pq
// quotes what it cannot parse: net/url's error quotes the whole URL, and its
// reason can quote a piece of it (an escape, a port), which a line break
// after DATABASE_URL, used exactly as set, causes; a key=value string's
// parse error quotes the text around the fault, such as the rest of an
// unquoted password with a space in it; and a value lib/pq will not take
// is quoted too. A URL that does not parse is reported as such, saying only
// whether it holds a control character.
func connectError(dsn string, err error) error {
	var bad *url.Error
	switch {
	case errors.As(err, &bad) && strings.ContainsFunc(dsn, unicode.IsControl):
		return errors.New("the database URL does not parse: it holds a control character, such as a line break")
	case errors.As(err, &bad):
		return errors.New("the database URL does not parse (the parser's reason is left out, since it can quote the password)")
	case quotesNoConnectionString(err):
		return err
	}
	return errors.New("could not connect to the database (lib/pq's reason is left out, since it can quote the connection string, password included)")
}

// quotesNoConnectionString reports an error connectError passes on. A
// *url.Error has a net.Error's methods, so connectError rules it out first.
func quotesNoConnectionString(err error) bool {
	var (
		server    *pq.Error
		dial      net.Error
		verify    *tls.CertificateVerificationError
		authority x509.UnknownAuthorityError
		hostname  x509.HostnameError
		invalid   x509.CertificateInvalidError
		record    tls.RecordHeaderError
		alert     tls.AlertError
	)
	if errors.As(err, &server) || errors.As(err, &dial) || errors.As(err, &verify) ||
		errors.As(err, &authority) || errors.As(err, &hostname) || errors.As(err, &invalid) ||
		errors.As(err, &record) || errors.As(err, &alert) {
		return true
	}
	for _, fixed := range []error{
		pq.ErrSSLNotSupported, pq.ErrSSLKeyUnknownOwnership, pq.ErrSSLKeyHasWorldPermissions,
		pq.ErrCouldNotDetectUsername, io.EOF, io.ErrUnexpectedEOF, driver.ErrBadConn,
		context.Canceled, context.DeadlineExceeded,
	} {
		if errors.Is(err, fixed) {
			return true
		}
	}
	return false
}
