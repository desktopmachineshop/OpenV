package postgres

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/products"
)

// X13's literal equality for the product profile repository: Get's SELECT and
// Upsert's INSERT (with its ON CONFLICT clause), assembled from
// productProfileColumns and the literals around it, are the literals they
// replaced, byte for byte (spacing, line breaks and tabs included), and each
// method still sends its own. The statements are read off the wire by the
// recorder in pgkit_test.go, so the test needs no server and no database, and
// checks what each method sends, not a copy of it. The repository has no
// transaction: each method sends its one statement and nothing else. Get
// answers nil, nil for no row, which the recorder answers for any query that
// matches none.

const (
	// oldProductProfileSelect, Get
	oldProductProfileSelect = `
		SELECT project_id, vision, problem_statement, target_users, constraints, success_metrics, settings, created_at, updated_at
		FROM product_profiles
		WHERE project_id = $1
	`
	// oldProductProfileUpsert, Upsert
	oldProductProfileUpsert = `
		INSERT INTO product_profiles (project_id, vision, problem_statement, target_users, constraints, success_metrics, settings, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (project_id) DO UPDATE SET
			vision = EXCLUDED.vision,
			problem_statement = EXCLUDED.problem_statement,
			target_users = EXCLUDED.target_users,
			constraints = EXCLUDED.constraints,
			success_metrics = EXCLUDED.success_metrics,
			settings = EXCLUDED.settings,
			updated_at = EXCLUDED.updated_at
	`
)

// The list the base wrote out, on one line.
const oldProductProfileColumns = `project_id, vision, problem_statement, target_users, constraints, success_metrics, settings, created_at, updated_at`

// The columns are the old list.
func TestProductProfileColumnsAreTheOldList(t *testing.T) {
	if productProfileColumns != oldProductProfileColumns {
		t.Errorf("productProfileColumns =\n  %q\nwant\n  %q", productProfileColumns, oldProductProfileColumns)
	}
}

// Each method sends its old literal and nothing else. Get answers nil, nil for
// a project with no row, the recorder's empty result, and Upsert's statement
// is one Exec.
func TestProductProfileStatementsAreTheOldLiterals(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewProductProfileRepository(db)
	project := uuid.New().String()

	if err := repo.Upsert(&products.ProductProfile{ProjectID: project}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	wantEvents(t, "Upsert", rec.take(), "EXEC "+oldProductProfileUpsert)

	got, err := repo.Get(project)
	if got != nil || err != nil {
		t.Errorf("Get with no row = %s, %v; want nil and no error", rtJSON(got), err)
	}
	wantEvents(t, "Get", rec.take(), "QUERY "+oldProductProfileSelect)
}
