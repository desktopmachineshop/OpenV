package postgres

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/products"
)

// The product profile repository's columns (refactor plan X13's
// characterization for product_profile_repository.go, in the manner of the
// chatter repository's), pinned as found: the nine columns Get reads back,
// each holding a value no other does, so a swap shows; what Upsert writes into
// each column and what its conflict branch updates and keeps (created_at is
// kept); the wall clock a TIMESTAMP holds for a time in another zone; the empty
// JSON an absent list or map is stored and read as; the NULLs the columns may
// hold once the schema is loosened, and the scan error that names each place;
// what Get answers for a row no one has, for a malformed id (no row too), and
// for a database that fails. The existing tests (workspace_content_utc_stamps_test.go and
// export_roundtrip_test.go) pin the timestamps and the export's field counts
// through the service, not these columns. The repository has no transaction
// (each method sends one statement), so there is none to pin.
//
// Every test that reads or writes rows gets its own database through rtDB;
// none takes a lock, so none needs NOWAIT.

// rtPPProfile is a profile whose every field differs from the one for any
// other n, so a swap of two columns in a scan or an insert shows. Its
// created_at is rtAt(n) and its updated_at rtAt(100+n).
func rtPPProfile(project string, n int) *products.ProductProfile {
	return &products.ProductProfile{
		ProjectID:        project,
		Vision:           fmt.Sprintf("vision %d", n),
		ProblemStatement: fmt.Sprintf("problem %d", n),
		TargetUsers:      fmt.Sprintf("users %d", n),
		Constraints:      []map[string]interface{}{{"label": fmt.Sprintf("constraint %d", n), "weight": float64(n)}},
		SuccessMetrics:   []map[string]interface{}{{"name": fmt.Sprintf("metric %d", n), "target": float64(10 * n)}},
		Settings:         map[string]interface{}{"theme": fmt.Sprintf("theme %d", n)},
		CreatedAt:        rtAt(n),
		UpdatedAt:        rtAt(100 + n),
	}
}

// rtPPSeedProject inserts a project for a profile to belong to and answers its id.
func rtPPSeedProject(t *testing.T, db *sql.DB) string {
	t.Helper()
	id := uuid.New().String()
	rtSeed(t, db, `INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Profile')`, id, uuid.New().String())
	return id
}

// rtPPSave stores a profile with Upsert, failing the test on an error.
func rtPPSave(t *testing.T, repo *ProductProfileRepository, p *products.ProductProfile) {
	t.Helper()
	if err := repo.Upsert(p); err != nil {
		t.Fatalf("Upsert %s: %v", p.ProjectID, err)
	}
}

// rtPPStored is the row of a profile as the table holds it, read by column
// name, so it does not depend on the repository's list or scan.
type rtPPStored struct {
	vision, problem, users         string
	constraints, metrics, settings []byte
	createdAt, updatedAt           time.Time
}

func rtPPRead(t *testing.T, db *sql.DB, project string) rtPPStored {
	t.Helper()
	var s rtPPStored
	err := db.QueryRow(`SELECT vision, problem_statement, target_users, constraints, success_metrics, settings, created_at, updated_at
		FROM product_profiles WHERE project_id = $1`, project).Scan(
		&s.vision, &s.problem, &s.users, &s.constraints, &s.metrics, &s.settings, &s.createdAt, &s.updatedAt)
	if err != nil {
		t.Fatalf("reading the stored profile of %s: %v", project, err)
	}
	return s
}

// rtPPWantJSON fails unless the stored JSON decodes to want, compared as Go
// values (the jsonb text is normalised, so it is not compared as text).
func rtPPWantJSON(t *testing.T, what string, stored []byte, want interface{}) {
	t.Helper()
	got := reflect.New(reflect.TypeOf(want))
	if err := json.Unmarshal(stored, got.Interface()); err != nil {
		t.Fatalf("%s: stored %q does not decode: %v", what, stored, err)
	}
	if !reflect.DeepEqual(got.Elem().Interface(), want) {
		t.Errorf("%s stored as %s, want %v", what, stored, want)
	}
}

// rtPPWantSame fails unless got is sent, field by field, with each time as a
// TIMESTAMP column hands it back, so a swap names the field it hit.
func rtPPWantSame(t *testing.T, what string, got, sent *products.ProductProfile) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: no profile, want one for %s", what, sent.ProjectID)
	}
	if got.ProjectID != sent.ProjectID {
		t.Errorf("%s project_id = %q, want %q", what, got.ProjectID, sent.ProjectID)
	}
	if got.Vision != sent.Vision {
		t.Errorf("%s vision = %q, want %q", what, got.Vision, sent.Vision)
	}
	if got.ProblemStatement != sent.ProblemStatement {
		t.Errorf("%s problem_statement = %q, want %q", what, got.ProblemStatement, sent.ProblemStatement)
	}
	if got.TargetUsers != sent.TargetUsers {
		t.Errorf("%s target_users = %q, want %q", what, got.TargetUsers, sent.TargetUsers)
	}
	if !reflect.DeepEqual(got.Constraints, sent.Constraints) {
		t.Errorf("%s constraints = %v, want %v", what, got.Constraints, sent.Constraints)
	}
	if !reflect.DeepEqual(got.SuccessMetrics, sent.SuccessMetrics) {
		t.Errorf("%s success_metrics = %v, want %v", what, got.SuccessMetrics, sent.SuccessMetrics)
	}
	if !reflect.DeepEqual(got.Settings, sent.Settings) {
		t.Errorf("%s settings = %v, want %v", what, got.Settings, sent.Settings)
	}
	rtWantTimestamp(t, what+" created_at", got.CreatedAt, sent.CreatedAt)
	rtWantTimestamp(t, what+" updated_at", got.UpdatedAt, sent.UpdatedAt)
}

// Get reads back every column, each with a value no other column holds, from a
// row written by SQL rather than by Upsert, so the read is pinned on its own.
func TestProductProfileGetReadsBackEveryColumn(t *testing.T) {
	db := rtDB(t)
	repo := NewProductProfileRepository(db)
	project := rtPPSeedProject(t, db)
	want := rtPPProfile(project, 1)

	constraints, _ := json.Marshal(want.Constraints)
	metrics, _ := json.Marshal(want.SuccessMetrics)
	settings, _ := json.Marshal(want.Settings)
	rtSeed(t, db, `INSERT INTO product_profiles (project_id, vision, problem_statement, target_users, constraints, success_metrics, settings, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		project, want.Vision, want.ProblemStatement, want.TargetUsers, constraints, metrics, settings, want.CreatedAt, want.UpdatedAt)

	got, err := repo.Get(project)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	rtPPWantSame(t, "Get", got, want)
}

// Upsert writes every field into its own column: read back by column name, the
// stored row is what was sent, and Get answers the same.
func TestProductProfileUpsertWritesEveryColumn(t *testing.T) {
	db := rtDB(t)
	repo := NewProductProfileRepository(db)
	project := rtPPSeedProject(t, db)
	sent := rtPPProfile(project, 2)

	rtPPSave(t, repo, sent)

	stored := rtPPRead(t, db, project)
	for _, c := range []struct{ column, got, want string }{
		{"vision", stored.vision, sent.Vision},
		{"problem_statement", stored.problem, sent.ProblemStatement},
		{"target_users", stored.users, sent.TargetUsers},
	} {
		if c.got != c.want {
			t.Errorf("stored %s = %q, want %q", c.column, c.got, c.want)
		}
	}
	rtPPWantJSON(t, "constraints", stored.constraints, sent.Constraints)
	rtPPWantJSON(t, "success_metrics", stored.metrics, sent.SuccessMetrics)
	rtPPWantJSON(t, "settings", stored.settings, sent.Settings)
	rtWantTimestamp(t, "stored created_at", stored.createdAt, sent.CreatedAt)
	rtWantTimestamp(t, "stored updated_at", stored.updatedAt, sent.UpdatedAt)

	got, err := repo.Get(project)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	rtPPWantSame(t, "Get after Upsert", got, sent)
}

// A time in another zone is stored as its wall clock: the TIMESTAMP column
// drops the offset rather than converting it (rtCEST is an offset a server
// outside UTC would carry).
func TestProductProfileUpsertStoresTheWallClockOfAnotherZone(t *testing.T) {
	db := rtDB(t)
	repo := NewProductProfileRepository(db)
	project := rtPPSeedProject(t, db)
	sent := rtPPProfile(project, 3)
	sent.CreatedAt = rtAt(3).In(rtCEST)
	sent.UpdatedAt = rtAt(103).In(rtCEST)

	rtPPSave(t, repo, sent)

	stored := rtPPRead(t, db, project)
	rtWantTimestamp(t, "stored created_at", stored.createdAt, sent.CreatedAt)
	rtWantTimestamp(t, "stored updated_at", stored.updatedAt, sent.UpdatedAt)
}

// A second Upsert of a project updates every field of the conflict branch and
// keeps the created_at of the first: ON CONFLICT does not name created_at.
func TestProductProfileUpsertConflictUpdatesButKeepsCreatedAt(t *testing.T) {
	db := rtDB(t)
	repo := NewProductProfileRepository(db)
	project := rtPPSeedProject(t, db)
	first := rtPPProfile(project, 1)
	rtPPSave(t, repo, first)

	second := rtPPProfile(project, 2)
	second.CreatedAt = rtAt(50) // a different created_at, which must not be written
	rtPPSave(t, repo, second)

	stored := rtPPRead(t, db, project)
	if stored.vision != second.Vision || stored.problem != second.ProblemStatement || stored.users != second.TargetUsers {
		t.Errorf("after the conflict: vision %q, problem %q, users %q; want %q, %q, %q",
			stored.vision, stored.problem, stored.users, second.Vision, second.ProblemStatement, second.TargetUsers)
	}
	rtPPWantJSON(t, "constraints after the conflict", stored.constraints, second.Constraints)
	rtPPWantJSON(t, "success_metrics after the conflict", stored.metrics, second.SuccessMetrics)
	rtPPWantJSON(t, "settings after the conflict", stored.settings, second.Settings)
	rtWantTimestamp(t, "updated_at after the conflict", stored.updatedAt, second.UpdatedAt)
	rtWantTimestamp(t, "created_at after the conflict (kept from the first write)", stored.createdAt, first.CreatedAt)
}

// Nil and empty lists and maps are stored as their empty JSON ([] and {}), and
// Get answers them as non-nil empties, never nil.
func TestProductProfileNilIsStoredAsEmpty(t *testing.T) {
	db := rtDB(t)
	repo := NewProductProfileRepository(db)
	project := rtPPSeedProject(t, db)

	rtPPSave(t, repo, &products.ProductProfile{ProjectID: project, CreatedAt: rtAt(1), UpdatedAt: rtAt(2)})

	stored := rtPPRead(t, db, project)
	rtPPWantJSON(t, "nil constraints", stored.constraints, []map[string]interface{}{})
	rtPPWantJSON(t, "nil success_metrics", stored.metrics, []map[string]interface{}{})
	rtPPWantJSON(t, "nil settings", stored.settings, map[string]interface{}{})

	got, err := repo.Get(project)
	if err != nil || got == nil {
		t.Fatalf("Get: %v, %v; want a profile", got, err)
	}
	if got.Constraints == nil || len(got.Constraints) != 0 {
		t.Errorf("Get constraints = %#v; want an empty, non-nil list", got.Constraints)
	}
	if got.SuccessMetrics == nil || len(got.SuccessMetrics) != 0 {
		t.Errorf("Get success_metrics = %#v; want an empty, non-nil list", got.SuccessMetrics)
	}
	if got.Settings == nil || len(got.Settings) != 0 {
		t.Errorf("Get settings = %#v; want an empty, non-nil map", got.Settings)
	}
	if got.Vision != "" || got.ProblemStatement != "" || got.TargetUsers != "" {
		t.Errorf("Get of an empty profile: vision %q, problem %q, users %q; want empty text", got.Vision, got.ProblemStatement, got.TargetUsers)
	}
}

// Get answers nil and no error for a project with no profile, and the same for
// a malformed id: noRow counts Postgres's refusal of a non-uuid as no row.
func TestProductProfileGetAnswersNilForNoRow(t *testing.T) {
	db := rtDB(t)
	repo := NewProductProfileRepository(db)

	got, err := repo.Get(uuid.New().String())
	if got != nil || err != nil {
		t.Errorf("Get with no row = %s, %v; want nil and no error", rtJSON(got), err)
	}

	got, err = repo.Get("not-a-uuid")
	if got != nil || err != nil {
		t.Errorf("Get of a malformed id = %s, %v; want nil and no error", rtJSON(got), err)
	}
}

// A column the schema forbids NULL, here one a test has let take it, fails the
// read that scans it, naming the column and its place: the text columns 1 to 3
// and the timestamps 7 and 8. Get returns the scan's error as it came, with no
// profile. A NULL JSON column is not an error: it is read as the empty default.
func TestProductProfileNullColumnsFailTheScanNamingItsPlace(t *testing.T) {
	const str = "converting NULL to string is unsupported"
	const stamp = "unsupported Scan, storing driver.Value type <nil> into type *time.Time"
	for _, c := range []struct {
		column  string
		index   int
		message string // empty for a JSON column, which reads as its empty default
	}{
		{column: "vision", index: 1, message: str},
		{column: "problem_statement", index: 2, message: str},
		{column: "target_users", index: 3, message: str},
		{column: "constraints", index: 4},
		{column: "success_metrics", index: 5},
		{column: "settings", index: 6},
		{column: "created_at", index: 7, message: stamp},
		{column: "updated_at", index: 8, message: stamp},
	} {
		t.Run(c.column, func(t *testing.T) {
			db := rtDB(t)
			repo := NewProductProfileRepository(db)
			project := rtPPSeedProject(t, db)
			rtPPSave(t, repo, rtPPProfile(project, 1))

			rtSeed(t, db, `ALTER TABLE product_profiles ALTER COLUMN `+c.column+` DROP NOT NULL`)
			rtSeed(t, db, `UPDATE product_profiles SET `+c.column+` = NULL WHERE project_id = $1`, project)

			got, err := repo.Get(project)
			if c.message == "" {
				if err != nil || got == nil {
					t.Fatalf("Get over a NULL %s = %s, %v; want a profile and no error", c.column, rtJSON(got), err)
				}
				empty := map[string]bool{
					"constraints":     got.Constraints != nil && len(got.Constraints) == 0,
					"success_metrics": got.SuccessMetrics != nil && len(got.SuccessMetrics) == 0,
					"settings":        got.Settings != nil && len(got.Settings) == 0,
				}[c.column]
				if !empty {
					t.Errorf("Get over a NULL %s = %s; want the empty default", c.column, rtJSON(got))
				}
				return
			}
			want := fmt.Sprintf("sql: Scan error on column index %d, name %q: %s", c.index, c.column, c.message)
			if got != nil || err == nil || err.Error() != want {
				t.Errorf("Get over a NULL %s = %s, %v; want nil and %s", c.column, rtJSON(got), err, want)
			}
		})
	}
}

// A database that fails the statement hands its error back as it came, with no
// profile for Get and no write for Upsert. It needs no server.
func TestProductProfileHandsBackADatabaseFailure(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewProductProfileRepository(db)
	project := uuid.New().String()

	rec.failOn = "FROM product_profiles"
	got, err := repo.Get(project)
	if got != nil || err != errRecordedStatement {
		t.Errorf("Get over a failing database = %s, %v; want nil and the database's error as it came", rtJSON(got), err)
	}

	rec.failOn = "INSERT INTO product_profiles"
	if err := repo.Upsert(rtPPProfile(project, 1)); err != errRecordedStatement {
		t.Errorf("Upsert over a failing database = %v; want the database's error as it came", err)
	}
}
