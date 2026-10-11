package postgres

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/members"
)

// The member repository's project_members columns (refactor plan X13's
// characterization for member_repository.go, in the manner of the chatter
// repository's), pinned as found: the four columns Find reads, which ListByProject
// reads again with the account's three, and Upsert writes. The round-trip tests
// of member_repository_roundtrip_test.go write a membership with Upsert and read
// it back with Find, so a column that Upsert and Find swapped alike would not
// show; these tests seed the row by SQL and read the stored row by column name,
// so each side stands on its own, each column holding a value no other does. They
// also pin what the round trips do not: the NULLs the columns may hold once the
// schema is loosened, and the scan error that names each place, for both reads.
// The team grants, the project ids and the roles are other statements on other
// lists and stay with the round trips.
//
// The repository has no transaction (each method sends one statement), so none
// is pinned. Every test that reads or writes rows gets its own database through
// rtDB; none takes a lock, so none needs NOWAIT.

// rtPMWorld is a workspace with a project and an account to hold memberships:
// the account's name, email and avatar differ from every other value.
type rtPMWorld struct {
	db      *sql.DB
	repo    *MemberRepository
	project string
	user    string
}

func rtPMNew(t *testing.T) rtPMWorld {
	t.Helper()
	db := rtDB(t)
	w := rtPMWorld{db: db, repo: NewMemberRepository(db), project: uuid.New().String(), user: uuid.New().String()}
	org := uuid.New().String()
	rtSeedOrg(t, db, org)
	rtSeedProject(t, db, w.project, org, "Platform")
	rtSeedUser(t, db, w.user, "dana@example.com", "Dana Whitfield", "https://example.com/dana.png")
	return w
}

// rtPMSeed inserts a membership by column name, the way Upsert does not.
func rtPMSeed(t *testing.T, w rtPMWorld, role string, createdAt time.Time) {
	t.Helper()
	rtSeed(t, w.db, `INSERT INTO project_members (project_id, user_id, role, created_at) VALUES ($1, $2, $3, $4)`,
		w.project, w.user, role, createdAt)
}

// Find reads back all four columns of a row written by SQL, each with a value
// no other holds, and joins no account: the display fields stay empty.
func TestMemberFindReadsBackEveryColumn(t *testing.T) {
	w := rtPMNew(t)
	rtPMSeed(t, w, members.RoleReviewer, rtAt(7))

	got, err := w.repo.Find(w.project, w.user)
	if err != nil || got == nil {
		t.Fatalf("Find: %v, %v", got, err)
	}
	if got.ProjectID != w.project {
		t.Errorf("Find project_id = %q, want %q", got.ProjectID, w.project)
	}
	if got.UserID != w.user {
		t.Errorf("Find user_id = %q, want %q", got.UserID, w.user)
	}
	if got.Role != members.RoleReviewer {
		t.Errorf("Find role = %q, want %q", got.Role, members.RoleReviewer)
	}
	rtWantTimestamp(t, "Find created_at", got.CreatedAt, rtAt(7))
	if got.UserName != "" || got.UserEmail != "" || got.AvatarURL != "" {
		t.Errorf("Find joined name %q, email %q, avatar %q; want none (Find joins no account)", got.UserName, got.UserEmail, got.AvatarURL)
	}
}

// ListByProject reads back the four columns and the account's three, each in
// its own field, from rows written by SQL: the account's name, email and
// avatar_url are the user row's, not the membership's.
func TestMemberListByProjectReadsBackEveryColumn(t *testing.T) {
	w := rtPMNew(t)
	rtPMSeed(t, w, members.RoleEditor, rtAt(9))

	got, err := w.repo.ListByProject(w.project)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListByProject = %s, %v; want the one member", rtJSON(got), err)
	}
	m := got[0]
	if m.ProjectID != w.project {
		t.Errorf("ListByProject project_id = %q, want %q", m.ProjectID, w.project)
	}
	if m.UserID != w.user {
		t.Errorf("ListByProject user_id = %q, want %q", m.UserID, w.user)
	}
	if m.Role != members.RoleEditor {
		t.Errorf("ListByProject role = %q, want %q", m.Role, members.RoleEditor)
	}
	rtWantTimestamp(t, "ListByProject created_at", m.CreatedAt, rtAt(9))
	if m.UserName != "Dana Whitfield" {
		t.Errorf("ListByProject user_name = %q, want %q", m.UserName, "Dana Whitfield")
	}
	if m.UserEmail != "dana@example.com" {
		t.Errorf("ListByProject user_email = %q, want %q", m.UserEmail, "dana@example.com")
	}
	if m.AvatarURL != "https://example.com/dana.png" {
		t.Errorf("ListByProject avatar_url = %q, want %q", m.AvatarURL, "https://example.com/dana.png")
	}
}

// Upsert writes every field into its own column: read back by column name, the
// stored row is what was sent, a time in another zone stored as its wall clock,
// the offset dropped; a second Upsert rewrites the role alone.
func TestMemberUpsertWritesEveryColumn(t *testing.T) {
	w := rtPMNew(t)

	stored := func() (project, user, role string, createdAt time.Time) {
		t.Helper()
		err := w.db.QueryRow(`SELECT project_id, user_id, role, created_at FROM project_members`).Scan(&project, &user, &role, &createdAt)
		if err != nil {
			t.Fatalf("reading the stored membership: %v", err)
		}
		return
	}

	sent := &members.Member{ProjectID: w.project, UserID: w.user, Role: members.RoleReviewer, CreatedAt: rtAt(3).In(rtCEST)}
	if err := w.repo.Upsert(sent); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	project, user, role, createdAt := stored()
	if project != w.project || user != w.user || role != members.RoleReviewer {
		t.Errorf("stored project_id %q, user_id %q, role %q; want %q, %q, %q", project, user, role, w.project, w.user, members.RoleReviewer)
	}
	rtWantTimestamp(t, "stored created_at", createdAt, sent.CreatedAt)

	if err := w.repo.Upsert(&members.Member{ProjectID: w.project, UserID: w.user, Role: members.RoleOwner, CreatedAt: rtAt(50)}); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}
	_, _, role, createdAt = stored()
	if role != members.RoleOwner {
		t.Errorf("stored role after a second Upsert = %q, want %q", role, members.RoleOwner)
	}
	rtWantTimestamp(t, "stored created_at after a second Upsert (kept from the first write)", createdAt, sent.CreatedAt)
}

// A column the schema forbids NULL, here one a test has let take it, fails the
// read that scans it, naming the column and its place: role the third and
// created_at the fourth of Find's four and of ListByProject's seven, and for
// ListByProject the account's name, email and avatar_url the fifth to the
// seventh (project_id and user_id are keys, which cannot be NULL). Each read
// returns the scan's error as it came, with no member, and ListByProject no
// list. Find joins no account, so a NULL there is no error for it.
func TestMemberNullColumnsFailTheScanNamingItsPlace(t *testing.T) {
	const str = "converting NULL to string is unsupported"
	const stamp = "unsupported Scan, storing driver.Value type <nil> into type *time.Time"
	for _, c := range []struct {
		table, column string
		index         int // the column's place in the list, from 0
		message       string
		inFind        bool // Find reads the column too
	}{
		{"project_members", "role", 2, str, true},
		{"project_members", "created_at", 3, stamp, true},
		{"users", "name", 4, str, false},
		{"users", "email", 5, str, false},
		{"users", "avatar_url", 6, str, false},
	} {
		t.Run(c.column, func(t *testing.T) {
			w := rtPMNew(t)
			rtPMSeed(t, w, members.RoleViewer, rtAt(1))
			rtSeed(t, w.db, `ALTER TABLE `+c.table+` ALTER COLUMN `+c.column+` DROP NOT NULL`)
			if c.table == "users" {
				rtSeed(t, w.db, `UPDATE users SET `+c.column+` = NULL WHERE id = $1`, w.user)
			} else {
				rtSeed(t, w.db, `UPDATE project_members SET `+c.column+` = NULL WHERE project_id = $1`, w.project)
			}

			want := fmt.Sprintf("sql: Scan error on column index %d, name %q: %s", c.index, c.column, c.message)
			got, err := w.repo.Find(w.project, w.user)
			if c.inFind {
				if got != nil || err == nil || err.Error() != want {
					t.Errorf("Find over a NULL %s = %s, %v; want nil and %s", c.column, rtJSON(got), err, want)
				}
			} else if err != nil || got == nil {
				t.Errorf("Find over a NULL %s of the account = %s, %v; want the membership and no error (it joins no account)", c.column, rtJSON(got), err)
			}

			list, err := w.repo.ListByProject(w.project)
			if list != nil || err == nil || err.Error() != want {
				t.Errorf("ListByProject over a NULL %s = %s, %v; want nil and %s", c.column, rtJSON(list), err, want)
			}
		})
	}
}
