package postgres

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// TestSetMemberPreviewRefusesANonMember locks in where the stable-release
// preview lives (REQ-138): on the account's membership of the workspace, so
// only a member has one. The UPDATE of org_members used to match no row for
// an account with no membership and report nothing, so a platform admin,
// whom the workspace guard lets by, was answered as though its preview were
// stored. It is refused with ErrNotMember, and no membership appears.
func TestSetMemberPreviewRefusesANonMember(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	svc := orgs.NewDefaultService(NewOrgRepository(db))

	member, outsider := uuid.New().String(), uuid.New().String()
	for _, u := range []struct{ id, email string }{{member, "preview-member@example.com"}, {outsider, "preview-outsider@example.com"}} {
		if _, err := db.Exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, 'P')`, u.id, u.email); err != nil {
			t.Fatal(err)
		}
	}
	org, err := svc.CreateOrg("Preview Workspace", orgs.TypeCompany, member)
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}

	if err := svc.SetMemberPreview(org.ID, member, true); err != nil {
		t.Fatalf("SetMemberPreview(member) = %v, want nil", err)
	}
	if on, err := svc.MemberPreview(org.ID, member); err != nil || !on {
		t.Fatalf("MemberPreview(member) = %v, %v; want true", on, err)
	}

	if err := svc.SetMemberPreview(org.ID, outsider, true); !errors.Is(err, orgs.ErrNotMember) {
		t.Fatalf("SetMemberPreview(non-member) = %v, want ErrNotMember", err)
	}
	if err := svc.SetMemberPreview(uuid.New().String(), member, true); !errors.Is(err, orgs.ErrNotMember) {
		t.Fatalf("SetMemberPreview(a workspace that does not exist) = %v, want ErrNotMember", err)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM org_members WHERE user_id = $1`, outsider).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("the refused preview left %d membership row(s)", rows)
	}
}
