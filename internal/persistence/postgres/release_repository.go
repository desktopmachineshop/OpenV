package postgres

import (
	"database/sql"
	"time"
)

// ReleaseRepository records which releases have been announced to members.
type ReleaseRepository struct {
	db *sql.DB
}

// NewReleaseRepository creates the release announcement store.
func NewReleaseRepository(db *sql.DB) *ReleaseRepository {
	return &ReleaseRepository{db: db}
}

// ClaimReleaseAnnouncement records that version is being announced and
// reports whether this caller won the claim. The primary key is the whole
// mechanism: a second insert of the same version changes nothing and
// answers false, so replicas booting together announce a release once.
func (r *ReleaseRepository) ClaimReleaseAnnouncement(version string, at time.Time) (bool, error) {
	res, err := r.db.Exec(`
		INSERT INTO release_announcements (version, announced_at) VALUES ($1, $2)
		ON CONFLICT (version) DO NOTHING
	`, version, at)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}
