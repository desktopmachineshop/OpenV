package postgres

import (
	"database/sql"
	"fmt"
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

// ClaimStableStep records that step ("announced", "reminded" or
// "turned_on") is being taken for a workspace and stable release, and
// reports whether this caller won it: the step's timestamp is set only
// when it is still NULL, so a second caller changes nothing.
func (r *ReleaseRepository) ClaimStableStep(orgID, version, step string, at time.Time) (bool, error) {
	var column string
	switch step {
	case "announced":
		column = "announced_at"
	case "reminded":
		column = "reminded_at"
	case "turned_on":
		column = "turned_on_at"
	default:
		return false, fmt.Errorf("unknown stable step %q", step)
	}
	res, err := r.db.Exec(`
		INSERT INTO release_schedule (org_id, version, `+column+`) VALUES ($1, $2, $3)
		ON CONFLICT (org_id, version) DO UPDATE SET `+column+` = EXCLUDED.`+column+`
		WHERE release_schedule.`+column+` IS NULL
	`, orgID, version, at)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
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
