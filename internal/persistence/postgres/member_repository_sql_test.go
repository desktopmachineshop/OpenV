package postgres

import (
	"database/sql/driver"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/members"
)

// X13's literal equality for the member repository: Upsert's INSERT, Find's
// SELECT and ListByProject's SELECT, assembled from memberColumns (ListByProject
// qualified by the alias pm), are the literals they replaced, byte for byte
// (spacing, line breaks and tabs included), and each method still sends its
// own. The statements are read off the wire by the recorder in pgkit_test.go, so
// the test needs no server and no database, and checks what each method sends,
// not a copy of it. The repository has no transaction: each method sends its one
// statement and nothing else. The other methods' statements are not in this
// file: they are not replaced.

const (
	// oldMemberInsert, Upsert
	oldMemberInsert = `
		INSERT INTO project_members (project_id, user_id, role, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_id, user_id) DO UPDATE SET role = EXCLUDED.role
	`
	// oldMemberFindSelect, Find
	oldMemberFindSelect = `
		SELECT project_id, user_id, role, created_at
		FROM project_members WHERE project_id = $1 AND user_id = $2
	`
	// oldMemberListByProjectSelect, ListByProject
	oldMemberListByProjectSelect = `
		SELECT pm.project_id, pm.user_id, pm.role, pm.created_at, u.name, u.email, u.avatar_url
		FROM project_members pm
		JOIN users u ON u.id = pm.user_id
		WHERE pm.project_id = $1
		ORDER BY lower(u.name) COLLATE "C", u.name COLLATE "C", lower(u.email) COLLATE "C", u.id
	`
)

// The list the base wrote out, on one line.
const oldMemberColumns = `project_id, user_id, role, created_at`

// The columns are the old list.
func TestMemberColumnsAreTheOldList(t *testing.T) {
	if memberColumns != oldMemberColumns {
		t.Errorf("memberColumns =\n  %q\nwant\n  %q", memberColumns, oldMemberColumns)
	}
}

// Each method sends its old literal and nothing else, and answers as it did for
// no row: Find nil and no error, ListByProject a nil list (JSON null), never [].
// A malformed id is no row either for the reads, and each statement is sent and
// answers the same.
func TestMemberStatementsAreTheOldLiterals(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewMemberRepository(db)
	project, user := uuid.New().String(), uuid.New().String()

	if err := repo.Upsert(&members.Member{ProjectID: project, UserID: user, Role: members.RoleViewer}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	wantEvents(t, "Upsert", rec.take(), "EXEC "+oldMemberInsert)

	got, err := repo.Find(project, user)
	if got != nil || err != nil {
		t.Errorf("Find with no row = %v, %v; want nil and no error", got, err)
	}
	wantEvents(t, "Find", rec.take(), "QUERY "+oldMemberFindSelect)

	got, err = repo.Find("not-a-uuid", user)
	if got != nil || err != nil {
		t.Errorf("Find of a malformed id = %v, %v; want nil and no error", got, err)
	}
	wantEvents(t, "Find of a malformed id", rec.take(), "QUERY "+oldMemberFindSelect)

	list, err := repo.ListByProject(project)
	rtWantNil(t, "ListByProject with no row", list, err)
	wantEvents(t, "ListByProject", rec.take(), "QUERY "+oldMemberListByProjectSelect)

	list, err = repo.ListByProject("not-a-uuid")
	rtWantNil(t, "ListByProject of a malformed project id", list, err)
	wantEvents(t, "ListByProject of a malformed project id", rec.take(), "QUERY "+oldMemberListByProjectSelect)
}

// The scans read their columns in the order of the lists, one row each, as the
// recorder answers a statement with a row of values: Find's four land in the
// membership's fields and leave the account's empty, ListByProject's seven land
// in all seven.
func TestMemberScansReadTheirColumnsInOrder(t *testing.T) {
	when := time.Date(2026, 3, 14, 9, 26, 53, 0, time.UTC)
	db, rec := recorderDB(t)
	repo := NewMemberRepository(db)
	rec.answers = []recAnswer{
		{"FROM project_members WHERE", []driver.Value{"the-project", "the-user", "the-role", when}},
		{"JOIN users u", []driver.Value{"the-project", "the-user", "the-role", when, "the-name", "the-email", "the-avatar"}},
	}

	got, err := repo.Find("the-project", "the-user")
	want := &members.Member{ProjectID: "the-project", UserID: "the-user", Role: "the-role", CreatedAt: when}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Find = %#v, %v; want %#v", got, err, want)
	}
	list, err := repo.ListByProject("the-project")
	joined := &members.Member{ProjectID: "the-project", UserID: "the-user", Role: "the-role", CreatedAt: when,
		UserName: "the-name", UserEmail: "the-email", AvatarURL: "the-avatar"}
	if err != nil || !reflect.DeepEqual(list, []*members.Member{joined}) {
		t.Errorf("ListByProject = %#v, %v; want [%#v]", list, err, joined)
	}
}
