package postgres

import "time"

// inUTC puts a TIMESTAMPTZ read in UTC. The driver hands a TIMESTAMPTZ back
// at the offset of the session's TimeZone, where a TIMESTAMP comes back as
// its UTC wall clock, so without this the columns migration 0050 moved to
// TIMESTAMPTZ would answer in whatever zone the database server is set to,
// unlike every other time the API answers, and a client that reads a date
// off the text (the evidence page takes captured_at's first ten characters)
// would read another day.
func inUTC(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
