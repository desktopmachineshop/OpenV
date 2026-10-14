package postgres

import (
	"fmt"
	"time"

	"github.com/openv/requirements-platform/internal/domain/usermetrics"
)

// The visitor half of UserMetricsRepository: it implements
// usermetrics.VisitorRepository over site_visitors and site_visit_days,
// joined to users for conversions. Days are DATE values in UTC.

// visitorConverted holds for a visitor v whose linked account u was made
// after the visitor was first seen: a sign-up, not a sign-in on a new
// browser.
const visitorConverted = `u.created_at >= v.first_seen`

// RecordVisit creates the visitor on its first visit and notes the day.
func (r *UserMetricsRepository) RecordVisit(visitorID string, day time.Time, path, source, country string) error {
	if _, err := r.db.Exec(`
		INSERT INTO site_visitors (id, first_day, landing_path, source, country)
		VALUES ($1, $2::date, $3, $4, $5)
		ON CONFLICT (id) DO NOTHING
	`, visitorID, dateOnly(day), path, source, country); err != nil {
		return err
	}
	_, err := r.db.Exec(`
		INSERT INTO site_visit_days (visitor_id, day) VALUES ($1, $2::date)
		ON CONFLICT DO NOTHING
	`, visitorID, dateOnly(day))
	return err
}

// LinkVisitor links an unlinked visitor to the account it signed in to.
func (r *UserMetricsRepository) LinkVisitor(visitorID, userID string) error {
	_, err := r.db.Exec(`
		UPDATE site_visitors SET user_id = $2, linked_at = NOW()
		WHERE id = $1 AND user_id IS NULL
	`, visitorID, userID)
	return err
}

// VisitorTotals counts visitors, new visitors, conversions and sign-ups.
func (r *UserMetricsRepository) VisitorTotals(today time.Time) (usermetrics.VisitorTotals, error) {
	var t usermetrics.VisitorTotals
	d := dateOnly(today)
	if err := r.db.QueryRow(`
		SELECT COUNT(DISTINCT visitor_id) FILTER (WHERE day = $1::date),
			COUNT(DISTINCT visitor_id) FILTER (WHERE day > $1::date - 7),
			COUNT(DISTINCT visitor_id)
		FROM site_visit_days WHERE day > $1::date - 30
	`, d).Scan(&t.Today, &t.Last7d, &t.Last30d); err != nil {
		return t, err
	}
	if err := r.db.QueryRow(`
		SELECT COUNT(*) FILTER (WHERE v.first_day > $1::date - 7),
			COUNT(*) FILTER (WHERE v.first_day > $1::date - 30),
			COUNT(*) FILTER (WHERE v.first_day <= $1::date - 30),
			COUNT(*) FILTER (WHERE v.first_day > $1::date - 30 AND `+visitorConverted+`),
			COUNT(*) FILTER (WHERE v.first_day <= $1::date - 30 AND `+visitorConverted+`)
		FROM site_visitors v LEFT JOIN users u ON u.id = v.user_id
		WHERE v.first_day > $1::date - 60
	`, d).Scan(&t.New7d, &t.New30d, &t.NewPrev30d, &t.Converted30d, &t.ConvertedPrev30d); err != nil {
		return t, err
	}
	err := r.db.QueryRow(`
		SELECT COUNT(*),
			COUNT(*) FILTER (WHERE EXISTS (
				SELECT 1 FROM site_visitors v WHERE v.user_id = u.id AND `+visitorConverted+`))
		FROM users u WHERE u.created_at::date > $1::date - 30
	`, d).Scan(&t.Signups30d, &t.TrackedSignups30d)
	return t, err
}

// DailyVisitors counts distinct visitors per day from since.
func (r *UserMetricsRepository) DailyVisitors(since time.Time) ([]usermetrics.DayCount, error) {
	return r.dayCounts(`
		SELECT day, COUNT(*) FROM site_visit_days
		WHERE day >= $1::date GROUP BY 1 ORDER BY 1
	`, dateOnly(since))
}

// DailyNewVisitors counts visitors per first day from since.
func (r *UserMetricsRepository) DailyNewVisitors(since time.Time) ([]usermetrics.DayCount, error) {
	return r.dayCounts(`
		SELECT first_day, COUNT(*) FROM site_visitors
		WHERE first_day >= $1::date GROUP BY 1 ORDER BY 1
	`, dateOnly(since))
}

// DailyConversions counts accounts made by visitors per sign-up day from
// since; an account two of its browsers led to counts once.
func (r *UserMetricsRepository) DailyConversions(since time.Time) ([]usermetrics.DayCount, error) {
	return r.dayCounts(`
		SELECT u.created_at::date, COUNT(DISTINCT u.id)
		FROM site_visitors v JOIN users u ON u.id = v.user_id
		WHERE `+visitorConverted+` AND u.created_at::date >= $1::date
		GROUP BY 1 ORDER BY 1
	`, dateOnly(since))
}

// visitorBreakdownColumns maps each breakdown dimension to its column; the
// query is built only from these.
var visitorBreakdownColumns = map[string]string{
	usermetrics.BreakdownLandingPage: "landing_path",
	usermetrics.BreakdownSource:      "source",
	usermetrics.BreakdownCountry:     "country",
}

// VisitorBreakdown groups the visitors first seen from since by dimension.
func (r *UserMetricsRepository) VisitorBreakdown(dimension string, since time.Time, limit int) ([]usermetrics.VisitorBreakdownRow, error) {
	col, ok := visitorBreakdownColumns[dimension]
	if !ok {
		return nil, fmt.Errorf("unknown visitor breakdown %q", dimension)
	}
	rows, err := r.db.Query(`
		SELECT v.`+col+`, COUNT(*), COUNT(*) FILTER (WHERE `+visitorConverted+`)
		FROM site_visitors v LEFT JOIN users u ON u.id = v.user_id
		WHERE v.first_day >= $1::date
		GROUP BY 1 ORDER BY 2 DESC, 1 LIMIT $2
	`, dateOnly(since), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []usermetrics.VisitorBreakdownRow
	for rows.Next() {
		var row usermetrics.VisitorBreakdownRow
		if err := rows.Scan(&row.Name, &row.Visitors, &row.Conversions); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
