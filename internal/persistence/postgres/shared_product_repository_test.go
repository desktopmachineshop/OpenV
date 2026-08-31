package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/sharedproducts"
)

// shareOne publishes a product through the service, which is the only path a
// real caller has (sanitization and limits included).
func shareOne(t *testing.T, svc *sharedproducts.DefaultService, name, orgID, userID string) *sharedproducts.Product {
	t.Helper()
	p, err := svc.Publish(sharedproducts.Product{
		Category:    "kitchen appliance",
		Name:        name,
		Description: "A coffee tin that recognises Kevin and locks.",
		Vision:      name + " becomes the reason the bean jar survives a Tuesday.",
		Problem:     "Beans vanish overnight and nobody admits to owning the grinder.",
		TargetUsers: "office workers whose beans keep leaving with Kevin",
	}, orgID, userID)
	if err != nil {
		t.Fatalf("publish %q: %v", name, err)
	}
	return p
}

// TestSharedProductPoolIsCrossTenant is the shape of the feature: two
// different workspaces publish, and each sees the other's product. This is
// the one table where that is correct, so it is worth asserting rather than
// assuming.
func TestSharedProductPoolIsCrossTenant(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	svc := sharedproducts.NewDefaultService(NewSharedProductRepository(db), 0, 0)

	orgA, orgB := uuid.New().String(), uuid.New().String()
	shareOne(t, svc, "Kevinproof", orgA, uuid.New().String())
	shareOne(t, svc, "Crustodian", orgB, uuid.New().String())

	pool, err := svc.List(0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	names := map[string]bool{}
	for _, p := range pool {
		names[p.Name] = true
		// Nothing published crosses a tenant boundary except the joke itself.
		if p.CreatedByOrg != "" || p.CreatedByUser != "" {
			t.Errorf("listing exposed author metadata: %+v", p)
		}
	}
	if !names["Kevinproof"] || !names["Crustodian"] {
		t.Errorf("pool = %v, want both workspaces' products", names)
	}
}

// TestSharedProductDedupeAndRateLimit covers the two SQL-backed guards: the
// unique name key (so one product cannot be flooded in under a hundred
// spellings) and the per-workspace daily allowance.
func TestSharedProductDedupeAndRateLimit(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewSharedProductRepository(db)
	svc := sharedproducts.NewDefaultService(repo, 3, 0)

	orgID, userID := uuid.New().String(), uuid.New().String()
	shareOne(t, svc, "Kevinproof", orgID, userID)

	// A cosmetic respelling normalizes to the same key and is refused.
	_, err := svc.Publish(sharedproducts.Product{
		Category: "kitchen appliance", Name: "kevin-proof!",
		Description: "A different tin that also locks.",
		Vision:      "Kevin-proof! becomes something else entirely.",
		Problem:     "Beans still vanish overnight, differently.",
		TargetUsers: "office workers who suspect a second Kevin",
	}, orgID, userID)
	if !errors.Is(err, sharedproducts.ErrDuplicate) {
		t.Errorf("respelling error = %v, want ErrDuplicate", err)
	}

	// Fill the workspace's allowance (3), then the next publish is refused.
	shareOne(t, svc, "Crustodian", orgID, userID)
	shareOne(t, svc, "Loafwatch", orgID, userID)
	if _, err := svc.Publish(sharedproducts.Product{
		Category: "toy", Name: "Fourthing",
		Description: "A toy that counts to four.",
		Vision:      "Fourthing becomes the fourth thing.",
		Problem:     "Nobody counts past three.",
		TargetUsers: "children who lose interest immediately after three",
	}, orgID, userID); !errors.Is(err, sharedproducts.ErrRateLimited) {
		t.Errorf("over the daily cap: %v, want ErrRateLimited", err)
	}

	// The cap is per workspace: a different one still publishes.
	shareOne(t, svc, "Fourthing", uuid.New().String(), uuid.New().String())

	// Hidden rows still count against the publisher's allowance, so taking
	// something down does not buy a fresh slot.
	count, err := repo.CountByOrgSince(orgID, time.Now().Add(-24*time.Hour))
	if err != nil || count != 3 {
		t.Errorf("CountByOrgSince = %d, %v; want 3", count, err)
	}
}

// TestSharedProductReportingAndTakedown: distinct reporters hide an entry for
// everyone, one account cannot, and an admin delete removes it along with its
// reports.
func TestSharedProductReportingAndTakedown(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewSharedProductRepository(db)
	svc := sharedproducts.NewDefaultService(repo, 0, 0)

	product := shareOne(t, svc, "Kevinproof", uuid.New().String(), uuid.New().String())
	loud := uuid.New().String()

	// One account, many clicks: the entry stays visible to everyone.
	for i := 0; i < sharedproducts.ReportsToHide*2; i++ {
		if err := svc.Report(product.ID, loud); err != nil {
			t.Fatalf("report: %v", err)
		}
	}
	pool, _ := svc.List(0)
	if len(pool) != 1 {
		t.Fatalf("one account hid an entry by reporting repeatedly (pool = %d)", len(pool))
	}

	// Distinct reporters do hide it.
	for i := 1; i < sharedproducts.ReportsToHide; i++ {
		if err := svc.Report(product.ID, uuid.New().String()); err != nil {
			t.Fatalf("report: %v", err)
		}
	}
	if pool, _ = svc.List(0); len(pool) != 0 {
		t.Errorf("entry still visible after %d distinct reporters", sharedproducts.ReportsToHide)
	}

	// Admin takedown removes the row (and its reports cascade).
	if err := svc.Delete(product.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var reports int
	if err := db.QueryRow(`SELECT COUNT(*) FROM shared_product_reports WHERE product_id = $1`, product.ID).Scan(&reports); err != nil {
		t.Fatalf("count reports: %v", err)
	}
	if reports != 0 {
		t.Errorf("%d reports outlived the deleted product", reports)
	}
	if err := svc.Delete(product.ID); !errors.Is(err, sharedproducts.ErrNotFound) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
	if err := svc.Report(product.ID, loud); !errors.Is(err, sharedproducts.ErrNotFound) {
		t.Errorf("report on a deleted product = %v, want ErrNotFound", err)
	}
}
