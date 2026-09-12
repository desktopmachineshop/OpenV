package seeds

import (
	"errors"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/sharedproducts"
)

// fakePoolRepo is the storage port reduced to what seeding touches: a pool
// keyed by the normalized name, so it enforces the same uniqueness rule the
// real table does.
type fakePoolRepo struct {
	sharedproducts.Repository
	byKey     map[string]*sharedproducts.Product
	createErr error
	creates   int
}

func newFakePoolRepo() *fakePoolRepo {
	return &fakePoolRepo{byKey: map[string]*sharedproducts.Product{}}
}

func (f *fakePoolRepo) Create(p *sharedproducts.Product) error {
	f.creates++
	if f.createErr != nil {
		return f.createErr
	}
	if _, clash := f.byKey[p.NameKey]; clash {
		return sharedproducts.ErrDuplicate
	}
	copied := *p
	f.byKey[p.NameKey] = &copied
	return nil
}

func TestSeedSharedProductPoolFillsAnEmptyPool(t *testing.T) {
	repo := newFakePoolRepo()

	added, err := EnsureSharedProductPool(repo)
	if err != nil {
		t.Fatalf("seeding an empty pool: %v", err)
	}
	want := len(builtinProducts())
	if added != want {
		t.Fatalf("added %d rows, want %d", added, want)
	}
	if len(repo.byKey) != want {
		t.Fatalf("pool holds %d rows, want %d", len(repo.byKey), want)
	}
}

// The seed runs on every boot, so the second one must be a no-op rather than a
// second copy of the pool.
func TestSeedSharedProductPoolIsIdempotent(t *testing.T) {
	repo := newFakePoolRepo()
	if _, err := EnsureSharedProductPool(repo); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	before := len(repo.byKey)

	added, err := EnsureSharedProductPool(repo)
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if added != 0 {
		t.Fatalf("second seed added %d rows, want 0", added)
	}
	if len(repo.byKey) != before {
		t.Fatalf("pool grew from %d to %d on a repeat seed", before, len(repo.byKey))
	}
}

// A member who published under one of these names owns that row. The seed
// fills gaps; it never overwrites somebody's entry.
func TestSeedSharedProductPoolLeavesAMembersEntryAlone(t *testing.T) {
	repo := newFakePoolRepo()
	first := builtinProducts()[0]
	mine := &sharedproducts.Product{
		ID:            "member-row",
		NameKey:       sharedproducts.NameKey(first.Name),
		Name:          first.Name,
		CreatedByUser: "u-1",
	}
	repo.byKey[mine.NameKey] = mine

	added, err := EnsureSharedProductPool(repo)
	if err != nil {
		t.Fatalf("seeding beside a member's row: %v", err)
	}
	if want := len(builtinProducts()) - 1; added != want {
		t.Fatalf("added %d rows, want %d", added, want)
	}
	if got := repo.byKey[mine.NameKey]; got.ID != "member-row" || got.CreatedByUser != "u-1" {
		t.Fatalf("the member's row was replaced: %+v", got)
	}
}

// Every entry has to survive the same validation a published product meets,
// or it would be seeded on some deployments and silently dropped on others.
func TestBuiltinProductsPassPublishValidation(t *testing.T) {
	seen := map[string]string{}
	for _, b := range builtinProducts() {
		clean, err := sharedproducts.Sanitize(sharedproducts.Product{
			Category:    b.Category,
			Name:        b.Name,
			Description: b.Description,
			Vision:      b.Vision,
			Problem:     b.Problem,
			TargetUsers: b.TargetUsers,
		})
		if err != nil {
			t.Errorf("%q is not publishable: %v", b.Name, err)
			continue
		}
		// Sanitize flattens prose; a entry that arrives already flat is one
		// nobody has to guess the rendered form of.
		if clean.Description != b.Description || clean.Problem != b.Problem {
			t.Errorf("%q is rewritten by Sanitize; write it in its final form", b.Name)
		}
		if prev, clash := seen[clean.NameKey]; clash {
			t.Errorf("%q and %q share the normalized name %q, so only one can ever be seeded",
				prev, b.Name, clean.NameKey)
		}
		seen[clean.NameKey] = b.Name
		// The vision opens with the product's own name, which is what makes a
		// leaderboard row readable on its own.
		if !strings.HasPrefix(b.Vision, b.Name) {
			t.Errorf("%q: vision should start with the product name, got %q", b.Name, b.Vision)
		}
	}
}

// A pool that cannot be written must not stop the server booting.
func TestSeedSharedProductPoolReportsButDoesNotPanicOnFailure(t *testing.T) {
	repo := newFakePoolRepo()
	repo.createErr = errors.New("database is down")

	added, err := EnsureSharedProductPool(repo)
	if added != 0 {
		t.Fatalf("added %d rows against a failing repository", added)
	}
	if err == nil {
		t.Fatal("a failing insert should be reported to the caller")
	}
	if repo.creates != len(builtinProducts()) {
		t.Fatalf("gave up after %d attempts; every entry should be tried", repo.creates)
	}
}

// A nil repository is the "no database" case and must be a quiet no-op.
func TestSeedSharedProductPoolWithoutARepository(t *testing.T) {
	added, err := EnsureSharedProductPool(nil)
	if added != 0 || err != nil {
		t.Fatalf("nil repository: added %d, err %v; want 0, nil", added, err)
	}
}
