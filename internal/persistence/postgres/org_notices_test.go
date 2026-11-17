package postgres

import (
	"testing"

	"github.com/google/uuid"
)

// A notice key is claimed once per workspace, across callers; a released
// claim can be taken again; the record keeps the subject and how many
// admins it reached; and the rows go with their workspace.
func TestNoticeClaimOncePerKey(t *testing.T) {
	f := newClaimFixture(t)
	repo := NewOrgRepository(f.db)
	other := uuid.New().String()
	if _, err := f.db.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Other', 'other-org')`, other); err != nil {
		t.Fatal(err)
	}
	claim := func(orgID, key string) bool {
		t.Helper()
		won, err := repo.ClaimNotice(orgID, key, "Payment failed")
		if err != nil {
			t.Fatalf("ClaimNotice: %v", err)
		}
		return won
	}
	if !claim(f.orgID, "billing:payment_failed:sub_1:1") {
		t.Fatal("first claim should win")
	}
	if claim(f.orgID, "billing:payment_failed:sub_1:1") {
		t.Fatal("second claim of one key must not win")
	}
	if !claim(f.orgID, "billing:payment_failed:sub_1:2") {
		t.Fatal("another occurrence is another key and should win")
	}
	if !claim(other, "billing:payment_failed:sub_1:1") {
		t.Fatal("the same key in another workspace should win")
	}

	if err := repo.RecordNotice(f.orgID, "billing:payment_failed:sub_1:1", 3); err != nil {
		t.Fatalf("RecordNotice: %v", err)
	}
	var subject string
	var recipients int
	if err := f.db.QueryRow(`SELECT subject, recipients FROM workspace_notices WHERE org_id = $1 AND notice_key = $2`,
		f.orgID, "billing:payment_failed:sub_1:1").Scan(&subject, &recipients); err != nil {
		t.Fatal(err)
	}
	if subject != "Payment failed" || recipients != 3 {
		t.Fatalf("record = %q, %d", subject, recipients)
	}

	if err := repo.ReleaseNotice(f.orgID, "billing:payment_failed:sub_1:2"); err != nil {
		t.Fatalf("ReleaseNotice: %v", err)
	}
	if !claim(f.orgID, "billing:payment_failed:sub_1:2") {
		t.Fatal("a released claim should be won again")
	}

	if _, err := f.db.Exec(`DELETE FROM organizations WHERE id = $1`, other); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM workspace_notices WHERE org_id = $1`, other).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("%d notices outlived their workspace", left)
	}
}
