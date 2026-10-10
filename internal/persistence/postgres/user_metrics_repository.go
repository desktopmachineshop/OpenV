package postgres

import (
	"database/sql"
	"time"

	"github.com/openv/requirements-platform/internal/domain/usermetrics"
)

// UserMetricsRepository implements usermetrics.Repository over users and
// user_activity_days. Days are DATE values in UTC.
type UserMetricsRepository struct {
	db *sql.DB
}

// NewUserMetricsRepository creates the repository.
func NewUserMetricsRepository(db *sql.DB) *UserMetricsRepository {
	return &UserMetricsRepository{db: db}
}

func dateOnly(t time.Time) string { return t.UTC().Format("2006-01-02") }

// RecordActivity upserts the user's day; a known country fills in an
// unknown one but never the other way round.
func (r *UserMetricsRepository) RecordActivity(userID string, day time.Time, country string) error {
	_, err := r.db.Exec(`
		INSERT INTO user_activity_days (user_id, day, country) VALUES ($1, $2::date, $3)
		ON CONFLICT (user_id, day) DO UPDATE SET country = EXCLUDED.country
		WHERE EXCLUDED.country <> '' AND user_activity_days.country <> EXCLUDED.country
	`, userID, dateOnly(day), country)
	return err
}

// UserTotals counts users by creation day.
func (r *UserMetricsRepository) UserTotals(today time.Time) (usermetrics.UserTotals, error) {
	var t usermetrics.UserTotals
	err := r.db.QueryRow(`
		SELECT COUNT(*),
			COUNT(*) FILTER (WHERE created_at::date > $1::date - 7),
			COUNT(*) FILTER (WHERE created_at::date > $1::date - 30),
			COUNT(*) FILTER (WHERE created_at::date > $1::date - 60 AND created_at::date <= $1::date - 30)
		FROM users
	`, dateOnly(today)).Scan(&t.Total, &t.New7d, &t.New30d, &t.NewPrev30d)
	return t, err
}

// ActivityTotals counts distinct active users per window, and lost ones.
func (r *UserMetricsRepository) ActivityTotals(today time.Time) (usermetrics.ActivityTotals, error) {
	var t usermetrics.ActivityTotals
	d := dateOnly(today)
	if err := r.db.QueryRow(`
		SELECT COUNT(DISTINCT user_id) FILTER (WHERE day = $1::date),
			COUNT(DISTINCT user_id) FILTER (WHERE day > $1::date - 7),
			COUNT(DISTINCT user_id)
		FROM user_activity_days WHERE day > $1::date - 30
	`, d).Scan(&t.DAU, &t.WAU, &t.MAU); err != nil {
		return t, err
	}
	err := r.db.QueryRow(`
		SELECT COUNT(*) FILTER (WHERE last_day <= $1::date - $2::int),
			COUNT(*) FILTER (WHERE last_day <= $1::date - $2::int AND last_day > $1::date - 2 * $2::int)
		FROM (SELECT MAX(day) AS last_day FROM user_activity_days GROUP BY user_id) t
	`, d, usermetrics.LostAfterDays).Scan(&t.Lost, &t.NewlyLost)
	return t, err
}

func (r *UserMetricsRepository) dayCounts(query string, args ...any) ([]usermetrics.DayCount, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []usermetrics.DayCount
	for rows.Next() {
		var c usermetrics.DayCount
		if err := rows.Scan(&c.Day, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DailySignups counts sign-ups per day from since.
func (r *UserMetricsRepository) DailySignups(since time.Time) ([]usermetrics.DayCount, error) {
	return r.dayCounts(`
		SELECT created_at::date, COUNT(*) FROM users
		WHERE created_at::date >= $1::date GROUP BY 1 ORDER BY 1
	`, dateOnly(since))
}

// DailyActive counts active users per day from since.
func (r *UserMetricsRepository) DailyActive(since time.Time) ([]usermetrics.DayCount, error) {
	return r.dayCounts(`
		SELECT day, COUNT(*) FROM user_activity_days
		WHERE day >= $1::date GROUP BY 1 ORDER BY 1
	`, dateOnly(since))
}

// AuthProviders counts users per sign-in method, largest first.
func (r *UserMetricsRepository) AuthProviders() ([]usermetrics.NamedCount, error) {
	rows, err := r.db.Query(`SELECT auth_provider, COUNT(*) FROM users GROUP BY 1 ORDER BY 2 DESC, 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []usermetrics.NamedCount
	for rows.Next() {
		var c usermetrics.NamedCount
		if err := rows.Scan(&c.Name, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Countries groups users by the country of their latest day that had one.
func (r *UserMetricsRepository) Countries(activeSince time.Time) ([]usermetrics.CountryCount, error) {
	rows, err := r.db.Query(`
		WITH latest AS (
			SELECT DISTINCT ON (user_id) user_id, country
			FROM user_activity_days WHERE country <> ''
			ORDER BY user_id, day DESC
		), recent AS (
			SELECT DISTINCT user_id FROM user_activity_days WHERE day >= $1::date
		)
		SELECT l.country, COUNT(*), COUNT(r.user_id)
		FROM latest l LEFT JOIN recent r ON r.user_id = l.user_id
		GROUP BY 1 ORDER BY 2 DESC, 1
	`, dateOnly(activeSince))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []usermetrics.CountryCount
	for rows.Next() {
		var c usermetrics.CountryCount
		if err := rows.Scan(&c.Country, &c.Users, &c.Active30d); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CohortSizes counts sign-ups per month from since.
func (r *UserMetricsRepository) CohortSizes(since time.Time) ([]usermetrics.DayCount, error) {
	return r.dayCounts(`
		SELECT date_trunc('month', created_at)::date, COUNT(*) FROM users
		WHERE created_at::date >= $1::date GROUP BY 1 ORDER BY 1
	`, dateOnly(since))
}

// CohortActivity counts each sign-up month's users active in each month.
func (r *UserMetricsRepository) CohortActivity(since time.Time) ([]usermetrics.CohortCell, error) {
	rows, err := r.db.Query(`
		SELECT date_trunc('month', u.created_at)::date, date_trunc('month', a.day)::date, COUNT(DISTINCT a.user_id)
		FROM users u JOIN user_activity_days a ON a.user_id = u.id
		WHERE u.created_at::date >= $1::date
		GROUP BY 1, 2 ORDER BY 1, 2
	`, dateOnly(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []usermetrics.CohortCell
	for rows.Next() {
		var c usermetrics.CohortCell
		if err := rows.Scan(&c.Cohort, &c.Month, &c.Users); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
