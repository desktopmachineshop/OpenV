package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// The usage view counts live rows per workspace: an archived artifact, its
// earlier versions, a removed link and an automatic chatter entry do not
// count; events count for their workspace or their project's, and only
// user and agent ones.
func TestUsageCounts(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewUserMetricsRepository(db)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	org, other, proj, user := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'W', $2), ($3, 'Empty', $4)`, org, org, other, other)
	exec(`INSERT INTO projects (id, name, org_id) VALUES ($1, 'P', $2)`, proj, org)
	exec(`INSERT INTO users (id, email, name, auth_provider) VALUES ($1, $2, 'U', 'password')`, user, user+"@example.com")
	exec(`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, org, user)
	exec(`INSERT INTO user_activity_days (user_id, day) VALUES ($1, CURRENT_DATE)`, user)

	req, req2, tc, archived, heading := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO artifacts (id, version, project_id, type, title, valid_to) VALUES
		($1, 1, $6, 'requirement', 'r', NOW()), ($1, 2, $6, 'requirement', 'r', NULL),
		($2, 1, $6, 'requirement', 'r2', NULL),
		($3, 1, $6, 'test-case', 't', NULL),
		($4, 1, $6, 'requirement', 'gone', NOW()),
		($5, 1, $6, 'heading', 'h', NULL)`, req, req2, tc, archived, heading, proj)
	exec(`INSERT INTO links (id, from_id, to_id, type, valid_to) VALUES
		($1, $4, $5, 'verifies', NULL), ($2, $4, $5, 'verifies', NOW()), ($3, $6, $5, 'verifies', NULL)`,
		uuid.NewString(), uuid.NewString(), uuid.NewString(), tc, req, archived)
	run := uuid.NewString()
	exec(`INSERT INTO test_runs (id, project_id, name) VALUES ($1, $2, 'run')`, run, proj)
	exec(`INSERT INTO test_results (id, run_id, test_case_id, status) VALUES ($1, $2, $3, 'pass')`, uuid.NewString(), run, tc)
	exec(`INSERT INTO chatter (id, artifact_id, message, is_auto_entry) VALUES ($1, $3, 'hi', false), ($2, $3, 'auto', true)`,
		uuid.NewString(), uuid.NewString(), req)
	exec(`INSERT INTO domain_events (id, event_type, org_id, project_id, actor) VALUES
		($1, 'artifact.created', $4, $5, 'user:'||$6::text),
		($2, 'link.created', NULL, $5, 'agent:run'),
		($3, 'artifact.updated', $4, $5, 'system')`,
		uuid.NewString(), uuid.NewString(), uuid.NewString(), org, proj, user)

	counts, err := repo.WorkspaceCounts()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range counts {
		if c.OrgID != org {
			continue
		}
		found = true
		if c.Projects != 1 || c.Requirements != 2 || c.TestCases != 1 || c.OtherArtifacts != 1 ||
			c.Links != 1 || c.TestRuns != 1 || c.TestResults != 1 || c.Comments != 1 {
			t.Errorf("counts %+v", c)
		}
	}
	if !found {
		t.Fatalf("workspace missing from %+v", counts)
	}

	since := time.Now().UTC().AddDate(0, 0, -7)
	ws, err := repo.WorkspaceChanges(since)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, k := range ws {
		if k.Key == org {
			total += k.Count
		}
	}
	if total != 2 {
		t.Errorf("workspace changes %d, want 2 (a user's and an agent's, not the system's): %+v", total, ws)
	}
	uc, err := repo.UserChanges(since)
	if err != nil {
		t.Fatal(err)
	}
	if len(uc) != 1 || uc[0].Key != user || uc[0].Count != 1 {
		t.Errorf("user changes %+v", uc)
	}
	ma, err := repo.MemberActivity(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(ma) != 1 || ma[0].Members != 1 || ma[0].Active30d != 1 {
		t.Errorf("member activity %+v", ma)
	}
	ms, err := repo.Memberships()
	if err != nil || len(ms) != 1 || ms[0].Role != "owner" {
		t.Errorf("memberships %+v %v", ms, err)
	}
	last, err := repo.LastWorkspaceChange()
	if err != nil || last[org].IsZero() {
		t.Errorf("last change %+v %v", last, err)
	}
	lu, err := repo.LastUserActive()
	if err != nil || lu[user].IsZero() {
		t.Errorf("last active %+v %v", lu, err)
	}
	ad, err := repo.UserActiveDays(since)
	if err != nil || len(ad) != 1 {
		t.Errorf("active days %+v %v", ad, err)
	}
}
