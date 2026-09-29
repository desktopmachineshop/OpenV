package postgres

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/lib/pq"
)

// A route takes whatever text a caller puts where it expects an id, and
// Postgres refuses text that is not a uuid with an error (SQLSTATE 22P02,
// invalid_text_representation), or, for text that is not UTF-8 or holds a
// NUL, before it reads it as anything (SQLSTATE 22021,
// character_not_in_repertoire), where a well-formed id no row has simply
// matches nothing. The repositories answer such an id exactly as they answer
// one no row has, so that a malformed id is never a 500: a single-row read
// reports no row (noRow), a list or a write whose id Postgres refused matched
// nothing (malformedID), and a list of ids keeps only those that can match
// (uuidsOnly).

// noSuchID is a well-formed id no row has (nothing mints the nil UUID), for a
// filter that must match as an id no row has matches, where a malformed one
// would make the query fail.
const noSuchID = "00000000-0000-0000-0000-000000000000"

// malformedID reports whether err is Postgres refusing a value as a uuid, or
// refusing text that is not UTF-8 or holds a NUL, which it does before it
// reads a type: an id that is not a UUID, which no row has. Text Postgres
// cannot hold is in no row either, so where such text stands beside the ids
// (a status filter, say) the query matches nothing all the same.
func malformedID(err error) bool {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return false
	}
	return pqErr.Code == "22021" ||
		pqErr.Code == "22P02" && strings.Contains(pqErr.Message, "invalid input syntax for type uuid")
}

// noRow reports whether a single-row read found nothing: sql.ErrNoRows, or an
// id Postgres refused as a uuid (malformedID).
func noRow(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || malformedID(err)
}

// matchedNone is err, or nil where Postgres refused an id as a uuid: a delete
// by an id no row has matches nothing, which is no failure.
func matchedNone(err error) error {
	if malformedID(err) {
		return nil
	}
	return err
}

// foreignKeyViolation reports whether err is Postgres refusing a row whose
// reference, through the named foreign key, names no row (SQLSTATE 23503).
func foreignKeyViolation(err error, constraint string) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23503" && pqErr.Constraint == constraint
}

// isUUID reports whether Postgres reads s as a uuid, as its uuid input does:
// 32 hex digits, in braces or not, with a hyphen allowed after any group of
// four but the last, and nothing else, not even a space.
func isUUID(s string) bool {
	if strings.HasPrefix(s, "{") {
		if !strings.HasSuffix(s, "}") || len(s) < 2 {
			return false
		}
		s = s[1 : len(s)-1]
	}
	digits := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
			digits++
			if digits > 32 {
				return false
			}
		case c == '-' && digits > 0 && digits%4 == 0 && digits < 32 && s[i-1] != '-':
		default:
			return false
		}
	}
	return digits == 32 && s[len(s)-1] != '-'
}

// uuidsOnly keeps the ids Postgres reads as uuids: the others match no row, so
// a query over the list answers as it would with them left out.
func uuidsOnly(ids []string) []string {
	kept := make([]string, 0, len(ids))
	for _, id := range ids {
		if isUUID(id) {
			kept = append(kept, id)
		}
	}
	return kept
}
