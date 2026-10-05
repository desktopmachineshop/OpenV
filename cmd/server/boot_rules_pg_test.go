//go:build unix

package main

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

// Refactor step X7a, the rules that are SQL (boot_rules_test.go has the
// unit tables and the plan reference): the bootstrap org query and the
// event bus's project-to-workspace resolver, each over a database state
// written row by row with plain SQL, against a real database
// (OPENV_TEST_DATABASE_URL; skipped when unset). Each test reaches its rule
// through one variable at its top, the closure cmd/server builds today; X7b
// re-points it at OrgRepository.EarliestPersonalOrgID and
// ProjectRepository.OrgIDForProject with identical rows.

// ruleDB is a migrated database of the test's own, written to with plain
// SQL so that a row can hold a state the services would refuse to make.
type ruleDB struct {
	t    *testing.T
	conn *sql.DB
	url  string
	ids  map[string]string // name → id
}

func newRuleDB(t *testing.T) *ruleDB {
	t.Helper()
	db := freshDatabase(t)
	conn, err := postgres.Connect(db.url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	// Migrate, not MigrateAndBackfill: the backfill's last step makes
	// projects.org_id NOT NULL, which a database that held an ownerless
	// project when it ran never got.
	if err := postgres.Migrate(conn); err != nil {
		t.Fatal(err)
	}
	return &ruleDB{t: t, conn: conn, url: db.url, ids: map[string]string{}}
}

// row is the database for one subtest, emptied of what the rows write:
// the same connection, with t and no names yet.
func (d *ruleDB) row(t *testing.T) *ruleDB {
	t.Helper()
	r := &ruleDB{t: t, conn: d.conn, url: d.url, ids: map[string]string{}}
	r.exec(`TRUNCATE users, organizations, projects CASCADE`)
	return r
}

func (d *ruleDB) exec(query string, args ...any) {
	d.t.Helper()
	if _, err := d.conn.Exec(query, args...); err != nil {
		d.t.Fatalf("%s: %v", strings.Join(strings.Fields(query), " "), err)
	}
}

// ruleAt is the instant a row's "hour" names: hours after a fixed start.
func ruleAt(hour int) time.Time {
	return time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC).Add(time.Duration(hour) * time.Hour)
}

// account stores an account created at the given hour.
func (d *ruleDB) account(name string, hour int) {
	d.t.Helper()
	id := uuid.NewString()
	d.ids[name] = id
	d.exec(`INSERT INTO users (id, email, name, created_at, updated_at) VALUES ($1, $2, $3, $4, $4)`,
		id, name+"@example.test", name, ruleAt(hour))
}

// workspace stores a workspace of the type ("personal" or "company")
// created by the named account ("" for none) at the given hour.
func (d *ruleDB) workspace(name, orgType, createdBy string, hour int) {
	d.t.Helper()
	id := uuid.NewString()
	d.ids[name] = id
	var by any
	if createdBy != "" {
		by = d.ids[createdBy]
	}
	d.exec(`INSERT INTO organizations (id, name, slug, org_type, created_by, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $6)`,
		id, name, id, orgType, by, ruleAt(hour))
}

// member makes the named account a member of the named workspace.
func (d *ruleDB) member(workspace, account, role string) {
	d.t.Helper()
	d.exec(`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)`, d.ids[workspace], d.ids[account], role)
}

// personal stores an account and its personal workspace, named
// "<account>-personal", owned and created by it at the same hour.
func (d *ruleDB) personal(account string, hour int) {
	d.t.Helper()
	d.account(account, hour)
	d.workspace(account+"-personal", "personal", account, hour)
	d.member(account+"-personal", account, "admin")
}

// project stores a project in the named workspace ("" leaves org_id NULL).
func (d *ruleDB) project(name, workspace string) {
	d.t.Helper()
	id := uuid.NewString()
	d.ids[name] = id
	var org any
	if workspace != "" {
		org = d.ids[workspace]
	}
	d.exec(`INSERT INTO projects (id, org_id, name) VALUES ($1, $2, $3)`, id, org, name)
}

// name is the row name an id belongs to, matched exactly, "" for "", or
// the id itself.
func (d *ruleDB) name(id string) string {
	if id == "" {
		return ""
	}
	for n, v := range d.ids {
		if v == id {
			return n
		}
	}
	return id
}

// closedDB is a connection to the test's database that has been closed.
func (d *ruleDB) closedDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := postgres.Connect(d.url)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	return conn
}

// TestBootstrapOrgIDRule pins the bootstrap org query (the legacy
// WORKER_API_KEY's workspace and the auth middleware's fallback): of the
// workspaces of type personal, the one whose member account was created
// first, by the account's created_at (ORDER BY u.created_at LIMIT 1), not
// the workspace's, nor the order rows were written in; "" when there is
// none or the query fails. It goes through org_members, so a personal
// workspace counts by its members, not by its created_by, and it reads
// soft-deleted workspaces too: two quirks of the query, on states the
// services do not make (a personal workspace takes no members and cannot be
// deleted), pinned because X7b keeps "the same query".
func TestBootstrapOrgIDRule(t *testing.T) {
	// X7b re-points this at OrgRepository.EarliestPersonalOrgID with
	// identical rows.
	earliest := func(db *sql.DB) string { return bootstrapOrgID(db)() }

	d := newRuleDB(t)
	for _, c := range []struct {
		name  string
		state func(d *ruleDB)
		want  string // the row name of the workspace, "" for none
	}{
		{"no accounts at all", func(d *ruleDB) {}, ""},
		{"one account and its personal workspace", func(d *ruleDB) { d.personal("ann", 0) }, "ann-personal"},
		{"three accounts, written latest first: the earliest account's wins", func(d *ruleDB) {
			d.personal("cy", 30)
			d.personal("bo", 20)
			d.personal("ann", 10)
		}, "ann-personal"},
		{"by the account's creation, not the workspace's", func(d *ruleDB) {
			d.account("ann", 1)
			d.account("bo", 2)
			d.workspace("ann-personal", "personal", "ann", 50)
			d.member("ann-personal", "ann", "admin")
			d.workspace("bo-personal", "personal", "bo", 3)
			d.member("bo-personal", "bo", "admin")
		}, "ann-personal"},
		{"accounts with only company workspaces", func(d *ruleDB) {
			d.account("ann", 1)
			d.account("bo", 2)
			d.workspace("acme", "company", "ann", 1)
			d.member("acme", "ann", "admin")
			d.member("acme", "bo", "member")
		}, ""},
		{"the earliest account has only a company workspace: the next one's personal wins", func(d *ruleDB) {
			d.account("ann", 1)
			d.workspace("acme", "company", "ann", 1)
			d.member("acme", "ann", "admin")
			d.personal("bo", 2)
			d.personal("cy", 3)
		}, "bo-personal"},
		{"a personal workspace no member row names is not found", func(d *ruleDB) {
			d.account("ann", 1)
			d.workspace("ann-personal", "personal", "ann", 1)
		}, ""},
		{"a personal workspace with no member row is passed over for a later one", func(d *ruleDB) {
			d.account("ann", 1)
			d.workspace("ann-personal", "personal", "ann", 1)
			d.personal("bo", 2)
		}, "bo-personal"},
		{"quirk: a personal workspace counts by its earliest member, not its creator", func(d *ruleDB) {
			d.account("ann", 1)
			d.personal("bo", 2)
			d.personal("cy", 3)
			d.member("cy-personal", "ann", "member")
		}, "cy-personal"},
		{"quirk: a soft-deleted personal workspace still counts", func(d *ruleDB) {
			d.personal("ann", 1)
			d.personal("bo", 2)
			d.exec(`UPDATE organizations SET deleted_at = $2 WHERE id = $1`, d.ids["ann-personal"], ruleAt(5))
		}, "ann-personal"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := d.row(t)
			c.state(r)
			if got := r.name(earliest(r.conn)); got != c.want {
				t.Errorf("bootstrap org = %q, want %q", got, c.want)
			}
		})
	}

	t.Run("the database is closed", func(t *testing.T) {
		d.row(t).personal("ann", 1)
		if got := earliest(d.closedDB(t)); got != "" {
			t.Errorf("bootstrap org over a closed database = %q, want \"\"", got)
		}
	})
}

// TestProjectOrgResolverRule pins the event bus's org resolver: a
// project's workspace id as text, and "" for a project with no workspace
// (COALESCE), an id no project has, an id that is not a UUID or is empty
// (the $1::uuid cast fails, and any error reads as ""), and a closed
// database. The cast reads a UUID in any form Postgres accepts, upper case
// or without hyphens, and the answer is the workspace id's canonical
// lower-case text (each row's answer is matched to a row name exactly).
func TestProjectOrgResolverRule(t *testing.T) {
	// X7b re-points this at ProjectRepository.OrgIDForProject with
	// identical rows.
	resolve := func(db *sql.DB, projectID string) string { return projectOrgResolver(db)(projectID) }

	d := newRuleDB(t).row(t)
	d.personal("ann", 1)
	d.workspace("acme", "company", "ann", 2)
	d.member("acme", "ann", "admin")
	d.project("roadmap", "acme")
	d.project("orphan", "")

	for _, c := range []struct {
		name, input string
		want        string // the row name of the workspace, "" for none
	}{
		{"a project in a workspace", d.ids["roadmap"], "acme"},
		{"the same id in upper case", strings.ToUpper(d.ids["roadmap"]), "acme"},
		{"the same id without hyphens", strings.ReplaceAll(d.ids["roadmap"], "-", ""), "acme"},
		{"a project with no workspace: NULL reads as empty", d.ids["orphan"], ""},
		{"a UUID no project has", uuid.NewString(), ""},
		{"a workspace's id is no project's", d.ids["acme"], ""},
		{"not a UUID: the cast fails", "not-a-uuid", ""},
		{"a UUID with a character too many: the cast fails", d.ids["roadmap"] + "0", ""},
		{"the empty string: the cast fails", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := d.name(resolve(d.conn, c.input)); got != c.want {
				t.Errorf("resolve(%q) = %q, want %q", c.input, got, c.want)
			}
		})
	}

	t.Run("the database is closed", func(t *testing.T) {
		if got := resolve(d.closedDB(t), d.ids["roadmap"]); got != "" {
			t.Errorf("resolve over a closed database = %q, want \"\"", got)
		}
	})
}

// routingKeys is the worker key store with the online check of one account
// failing, though it answers online; every other call reaches the real
// store.
type routingKeys struct {
	*postgres.WorkerKeyRepository
	failFor string
}

func (r routingKeys) HasOnlinePersonalKey(orgID, userID string, since time.Time) (bool, error) {
	if userID == r.failFor {
		return true, errors.New("worker_keys: statement timeout")
	}
	return r.WorkerKeyRepository.HasOnlinePersonalKey(orgID, userID, since)
}

// routingOrgs is the workspace store with one workspace that cannot be
// read; every other call reaches the real store.
type routingOrgs struct {
	*postgres.OrgRepository
	failFor string
}

func (r routingOrgs) FindOrgByID(id string) (*orgs.Org, error) {
	if id == r.failFor {
		return nil, errors.New("organizations: statement timeout")
	}
	return r.OrgRepository.FindOrgByID(id)
}

// TestRoutingPolicyRule pins the two closures stage agents hands
// SetRoutingPolicy (wire_agents.go), through what they decide: whether a
// run a member launches is reserved for the member's personal runner
// (PreferredUserID) and for how long (HostedAfter, after CreatedAt).
//
// The personal-runner check: the launcher's personal key in the run's
// workspace was used within the last 30 seconds (pinned at 25 s, online,
// and 35 s, not); a key of the launcher in another workspace, another
// member's key, or a check that fails, even one that says online, reserve
// nothing. The runner-grace lookup: the workspace's limits'
// runner_grace_seconds when it is a JSON number, truncated to whole
// seconds (quirk: 90.9 is 90) and with no upper bound; when it is missing,
// not a number, null, zero or less, or the workspace cannot be read, the
// lookup gives 0 and the run service's default of 60 s applies.
//
// The seam survives X7c: the test wires storage, workspace and agents in
// process, as main() does, over a database of its own
// (OPENV_TEST_DATABASE_URL; skipped when unset), and launches through the
// wired run service, so moving both closures into agentruns.RoutingPolicy
// and the runner-grace lookup changes none of it. The two rows of a
// failing store fail it through the services stage workspace built,
// swapped before stage agents runs; were the moved policy to read the
// stores some other way, those two rows' swap is what X7c re-points, with
// identical rows.
func TestRoutingPolicyRule(t *testing.T) {
	db := freshDatabase(t)
	conn, err := postgres.Connect(db.url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	// Stage workspace reads these two; unset, the tiers are off and there
	// is no runner pool, whatever the shell running the test has.
	t.Setenv("OPENV_BILLING_GRANDFATHER_BEFORE", "")
	t.Setenv("RUNNER_POOL_KEY", "")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	a := &app{db: conn, agentsDir: t.TempDir(), uploadsDir: t.TempDir()}
	a.storage()
	a.workspace()

	ids := map[string]string{}
	now := time.Now().UTC()
	for _, name := range []string{"mia", "ned", "kit"} {
		ids[name] = uuid.NewString()
		must(a.userRepo.SaveUser(&users.User{ID: ids[name], Email: name + "@example.test", Name: name,
			AuthProvider: users.ProviderPassword, CreatedAt: now, UpdatedAt: now}))
	}
	for _, name := range []string{"W", "V", "X"} {
		org, err := a.orgService.CreateOrg(name, orgs.TypeCompany, ids["mia"])
		must(err)
		ids[name] = org.ID
	}
	must(a.orgService.AddMember(ids["W"], ids["ned"], orgs.RoleMember))
	must(a.orgService.AddMember(ids["W"], ids["kit"], orgs.RoleMember))

	// kit's online check fails; workspace X cannot be read.
	a.workerKeyService = workerkeys.NewDefaultService(routingKeys{a.workerKeyRepo, ids["kit"]})
	a.workerKeyService.SetPairingRepository(a.workerKeyRepo)
	a.orgService = orgs.NewDefaultService(routingOrgs{a.orgRepo, ids["X"]})
	a.agents()

	// Each workspace's first seeded agent, and each account's personal key
	// in a workspace, named "<account>@<workspace>".
	agentOf := map[string]string{}
	for _, ws := range []string{"W", "V", "X"} {
		list, err := a.agentService.List(ids[ws])
		must(err)
		if len(list) == 0 {
			t.Fatalf("stage agents seeded no agent into workspace %s", ws)
		}
		agentOf[ws] = list[0].ID
	}
	keys := map[string]string{}
	for _, k := range []struct{ account, ws string }{{"mia", "W"}, {"mia", "V"}, {"mia", "X"}, {"ned", "W"}, {"kit", "W"}} {
		account := ids[k.account]
		key, _, err := a.workerKeyService.Create(ids[k.ws], "laptop", &account, &account)
		must(err)
		keys[k.account+"@"+k.ws] = key.ID
	}

	const s = time.Second
	for _, c := range []struct {
		name      string
		launcher  string
		ws        string
		used      map[string]time.Duration // key → how long ago it was last used; absent, never
		limits    string                   // the workspace's limits
		reserved  bool
		graceSecs int
	}{
		// The personal-runner check.
		{"the launcher's key used 10 s ago: reserved, for the default 60 s", "mia", "W",
			map[string]time.Duration{"mia@W": 10 * s}, `{}`, true, 60},
		{"used 25 s ago: still online", "mia", "W", map[string]time.Duration{"mia@W": 25 * s}, `{}`, true, 60},
		{"used 35 s ago: offline, not reserved", "mia", "W", map[string]time.Duration{"mia@W": 35 * s}, `{}`, false, 0},
		{"never used: not reserved", "mia", "W", nil, `{}`, false, 0},
		{"only the launcher's key in another workspace used: not reserved", "mia", "W",
			map[string]time.Duration{"mia@V": 10 * s}, `{}`, false, 0},
		{"only another member's key used: not reserved", "mia", "W", map[string]time.Duration{"ned@W": 10 * s}, `{}`, false, 0},
		{"the check fails, though it says online: not reserved", "kit", "W",
			map[string]time.Duration{"kit@W": 10 * s}, `{}`, false, 0},
		// The runner-grace lookup.
		{"a grace of 90", "mia", "W", map[string]time.Duration{"mia@W": 10 * s}, `{"runner_grace_seconds": 90}`, true, 90},
		{"a grace of 1", "mia", "W", map[string]time.Duration{"mia@W": 10 * s}, `{"runner_grace_seconds": 1}`, true, 1},
		{"quirk: a grace of 90.9 is truncated to 90", "mia", "W", map[string]time.Duration{"mia@W": 10 * s},
			`{"runner_grace_seconds": 90.9}`, true, 90},
		{"a grace of a day: no upper bound", "mia", "W", map[string]time.Duration{"mia@W": 10 * s},
			`{"runner_grace_seconds": 86400}`, true, 86400},
		{"a grace as a string: the default", "mia", "W", map[string]time.Duration{"mia@W": 10 * s},
			`{"runner_grace_seconds": "90"}`, true, 60},
		{"a grace of null: the default", "mia", "W", map[string]time.Duration{"mia@W": 10 * s},
			`{"runner_grace_seconds": null}`, true, 60},
		{"a grace of 0: the default", "mia", "W", map[string]time.Duration{"mia@W": 10 * s}, `{"runner_grace_seconds": 0}`, true, 60},
		{"a negative grace: the default", "mia", "W", map[string]time.Duration{"mia@W": 10 * s},
			`{"runner_grace_seconds": -30}`, true, 60},
		{"other limits but no grace: the default", "mia", "W", map[string]time.Duration{"mia@W": 10 * s},
			`{"max_projects": 7}`, true, 60},
		{"the workspace cannot be read: the default", "mia", "X", map[string]time.Duration{"mia@X": 10 * s},
			`{"runner_grace_seconds": 90}`, true, 60},
	} {
		t.Run(c.name, func(t *testing.T) {
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err := conn.Exec(`UPDATE worker_keys SET last_used_at = NULL`)
			must(err)
			for key, ago := range c.used {
				// last_used_at holds a UTC wall clock.
				_, err := conn.Exec(`UPDATE worker_keys SET last_used_at = $2 WHERE id = $1`, keys[key], time.Now().UTC().Add(-ago))
				must(err)
			}
			_, err = conn.Exec(`UPDATE organizations SET limits = $2 WHERE id = $1`, ids[c.ws], c.limits)
			must(err)

			launcher := ids[c.launcher]
			run, _, err := a.runService.Launch(agentruns.LaunchRequest{OrgID: ids[c.ws], AgentID: agentOf[c.ws],
				Prompt: "Summarise my week.", LaunchedBy: &launcher})
			must(err)

			reserved := run.PreferredUserID != nil
			if reserved != c.reserved {
				t.Fatalf("reserved for the launcher's runner: %v (preferred %v), want %v", reserved, run.PreferredUserID, c.reserved)
			}
			if !reserved {
				if run.HostedAfter != nil {
					t.Errorf("an unreserved run has hosted_after %v, want none", run.HostedAfter)
				}
				return
			}
			if *run.PreferredUserID != launcher {
				t.Errorf("reserved for %s, want the launcher %s", *run.PreferredUserID, launcher)
			}
			if run.HostedAfter == nil {
				t.Fatal("a reserved run has no hosted_after")
			}
			if got := run.HostedAfter.Sub(run.CreatedAt).Round(time.Second); got != time.Duration(c.graceSecs)*time.Second {
				t.Errorf("hosted_after is %v after created_at, want %ds", got, c.graceSecs)
			}
		})
	}
}
