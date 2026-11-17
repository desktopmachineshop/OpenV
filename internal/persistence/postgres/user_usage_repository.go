package postgres

import (
	"database/sql"
	"time"

	"github.com/openv/requirements-platform/internal/domain/usermetrics"
)

// The usage view's aggregates (usermetrics.UsageRepository). Every query
// answers counts and ids: no artifact text, name or reference leaves here.

// workspaceCountsSQL counts live rows per workspace through each row's
// project; a project with no workspace counts for none.
const workspaceCountsSQL = `
	WITH p AS (SELECT id, org_id FROM projects WHERE org_id IS NOT NULL),
	proj AS (SELECT org_id, COUNT(*) n FROM p GROUP BY org_id),
	art AS (
		SELECT p.org_id,
			COUNT(*) FILTER (WHERE a.type = 'requirement') req,
			COUNT(*) FILTER (WHERE a.type = 'test-case') tc,
			COUNT(*) FILTER (WHERE a.type NOT IN ('requirement', 'test-case')) other
		FROM artifacts a JOIN p ON p.id = a.project_id
		WHERE a.valid_to IS NULL GROUP BY p.org_id),
	lnk AS (
		SELECT p.org_id, COUNT(*) n
		FROM links l
		JOIN artifacts a ON a.id = l.from_id AND a.valid_to IS NULL
		JOIN p ON p.id = a.project_id
		WHERE l.valid_to IS NULL GROUP BY p.org_id),
	runs AS (SELECT p.org_id, COUNT(*) n FROM test_runs r JOIN p ON p.id = r.project_id GROUP BY p.org_id),
	res AS (
		SELECT p.org_id, COUNT(*) n
		FROM test_results t JOIN test_runs r ON r.id = t.run_id JOIN p ON p.id = r.project_id
		GROUP BY p.org_id),
	com AS (
		SELECT p.org_id, COUNT(*) n
		FROM chatter c
		JOIN artifacts a ON a.id = c.artifact_id AND a.valid_to IS NULL
		JOIN p ON p.id = a.project_id
		WHERE NOT c.is_auto_entry AND c.entry_type = 'comment' GROUP BY p.org_id)
	SELECT o.id::text,
		COALESCE(proj.n, 0), COALESCE(art.req, 0), COALESCE(art.tc, 0), COALESCE(art.other, 0),
		COALESCE(lnk.n, 0), COALESCE(runs.n, 0), COALESCE(res.n, 0), COALESCE(com.n, 0)
	FROM organizations o
	LEFT JOIN proj ON proj.org_id = o.id
	LEFT JOIN art ON art.org_id = o.id
	LEFT JOIN lnk ON lnk.org_id = o.id
	LEFT JOIN runs ON runs.org_id = o.id
	LEFT JOIN res ON res.org_id = o.id
	LEFT JOIN com ON com.org_id = o.id
	WHERE o.deleted_at IS NULL
	ORDER BY o.id`

// WorkspaceCounts answers what each live workspace holds now.
func (r *UserMetricsRepository) WorkspaceCounts() ([]usermetrics.WorkspaceCounts, error) {
	rows, err := r.db.Query(workspaceCountsSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []usermetrics.WorkspaceCounts{}
	for rows.Next() {
		var c usermetrics.WorkspaceCounts
		if err := rows.Scan(&c.OrgID, &c.Projects, &c.Requirements, &c.TestCases, &c.OtherArtifacts,
			&c.Links, &c.TestRuns, &c.TestResults, &c.Comments); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MemberActivity counts each workspace's members and those signed in on
// any of the last 30 days.
func (r *UserMetricsRepository) MemberActivity(today time.Time) ([]usermetrics.MemberActivity, error) {
	rows, err := r.db.Query(`
		SELECT m.org_id::text, COUNT(*),
			COUNT(*) FILTER (WHERE EXISTS (
				SELECT 1 FROM user_activity_days d
				WHERE d.user_id = m.user_id AND d.day > $1::date - 30))
		FROM org_members m GROUP BY m.org_id
	`, dateOnly(today))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []usermetrics.MemberActivity{}
	for rows.Next() {
		var m usermetrics.MemberActivity
		if err := rows.Scan(&m.OrgID, &m.Members, &m.Active30d); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func scanKeyedDays(rows *sql.Rows, err error) ([]usermetrics.KeyedDay, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []usermetrics.KeyedDay{}
	for rows.Next() {
		var k usermetrics.KeyedDay
		if err := rows.Scan(&k.Key, &k.Day, &k.Count); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// WorkspaceChanges counts each workspace's user and agent events per day.
// An event stamped with no workspace takes its project's.
func (r *UserMetricsRepository) WorkspaceChanges(since time.Time) ([]usermetrics.KeyedDay, error) {
	return scanKeyedDays(r.db.Query(`
		SELECT COALESCE(e.org_id, p.org_id)::text, e.created_at::date, COUNT(*)
		FROM domain_events e LEFT JOIN projects p ON p.id = e.project_id
		WHERE e.created_at >= $1::date AND e.actor <> 'system' AND COALESCE(e.org_id, p.org_id) IS NOT NULL
		GROUP BY 1, 2
	`, dateOnly(since)))
}

// UserChanges counts each user's own events per day (actor "user:<id>").
func (r *UserMetricsRepository) UserChanges(since time.Time) ([]usermetrics.KeyedDay, error) {
	return scanKeyedDays(r.db.Query(`
		SELECT substr(actor, 6), created_at::date, COUNT(*)
		FROM domain_events
		WHERE created_at >= $1::date AND actor LIKE 'user:%'
		GROUP BY 1, 2
	`, dateOnly(since)))
}

// UserActiveDays answers each day a user was signed in.
func (r *UserMetricsRepository) UserActiveDays(since time.Time) ([]usermetrics.KeyedDay, error) {
	return scanKeyedDays(r.db.Query(`
		SELECT user_id::text, day, 1 FROM user_activity_days WHERE day >= $1::date
	`, dateOnly(since)))
}

func scanLatest(rows *sql.Rows, err error) (map[string]time.Time, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var k string
		var t time.Time
		if err := rows.Scan(&k, &t); err != nil {
			return nil, err
		}
		out[k] = t
	}
	return out, rows.Err()
}

// LastWorkspaceChange answers each workspace's latest user or agent event.
func (r *UserMetricsRepository) LastWorkspaceChange() (map[string]time.Time, error) {
	return scanLatest(r.db.Query(`
		SELECT COALESCE(e.org_id, p.org_id)::text, MAX(e.created_at)
		FROM domain_events e LEFT JOIN projects p ON p.id = e.project_id
		WHERE e.actor <> 'system' AND COALESCE(e.org_id, p.org_id) IS NOT NULL
		GROUP BY 1
	`))
}

// LastUserActive answers each user's latest signed-in day.
func (r *UserMetricsRepository) LastUserActive() (map[string]time.Time, error) {
	return scanLatest(r.db.Query(`SELECT user_id::text, MAX(day) FROM user_activity_days GROUP BY 1`))
}

// Memberships answers every workspace membership with its role.
func (r *UserMetricsRepository) Memberships() ([]usermetrics.Membership, error) {
	rows, err := r.db.Query(`
		SELECT m.org_id::text, m.user_id::text, m.role
		FROM org_members m JOIN organizations o ON o.id = m.org_id
		WHERE o.deleted_at IS NULL
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []usermetrics.Membership{}
	for rows.Next() {
		var m usermetrics.Membership
		if err := rows.Scan(&m.OrgID, &m.UserID, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
