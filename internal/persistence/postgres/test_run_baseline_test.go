package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// A run keeps the baseline it names when that baseline is deleted (REQ-5),
// as history (REQ-13, #379's question 23), and every read of the run marks
// the reference baseline_deleted, computed against the baselines as they
// stand rather than stored: the lookup by id, the project's list, and a run
// read again after its status changed. A run that names no baseline, or one
// its project still has, is not marked. A baseline of another project, which
// a run could name before the create looked baselines up (REQ-6), reads as
// deleted too: the mark asks the run's own project alone, so a run never
// tells whether another project's baseline exists. Postgres-gated
// (OPENV_TEST_DATABASE_URL).
func TestARunKeepsADeletedBaselinesReference(t *testing.T) {
	db := testDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	runs, bases := NewVVRepository(db), NewBaselineRepository(db)
	projectID, otherID := uuid.New().String(), uuid.New().String()
	for _, id := range []string{projectID, otherID} {
		if _, err := db.Exec(`INSERT INTO projects (id, name) VALUES ($1, 'Cabinet')`, id); err != nil {
			t.Fatal(err)
		}
	}
	baseline := func(project string) string {
		t.Helper()
		b := &baselines.Baseline{ID: uuid.New().String(), ProjectID: project, Name: "Release 1",
			Snapshot: []byte(`{}`), CreatedAt: time.Now().UTC()}
		if err := bases.Create(b); err != nil {
			t.Fatal(err)
		}
		return b.ID
	}
	kept, deleted, others := baseline(projectID), baseline(projectID), baseline(otherID)

	started := time.Now().UTC().Add(-time.Hour)
	run := func(name string, baselineID *string) string {
		t.Helper()
		started = started.Add(time.Minute)
		r := &vv.TestRun{ID: uuid.New().String(), ProjectID: projectID, Name: name, BaselineID: baselineID,
			Status: vv.RunStatusInProgress, StartedAt: started, CreatedAt: started, UpdatedAt: started}
		if err := runs.SaveRun(r); err != nil {
			t.Fatal(err)
		}
		return r.ID
	}
	live := run("live", nil)
	onKept := run("on the kept baseline", &kept)
	onDeleted := run("on the deleted baseline", &deleted)
	onOthers := run("on another project's baseline", &others)

	if err := bases.Delete(deleted); err != nil {
		t.Fatal(err)
	}
	// The run on the deleted baseline is closed after the delete: its read
	// back still keeps the reference, and still marks it.
	closed, err := runs.FindRunByID(onDeleted)
	if err != nil {
		t.Fatal(err)
	}
	closed.Status, closed.UpdatedAt = vv.RunStatusCompleted, time.Now().UTC()
	if err := runs.UpdateRun(closed); err != nil {
		t.Fatal(err)
	}

	want := map[string]struct {
		baseline string
		deleted  bool
	}{
		live:      {"", false},
		onKept:    {kept, false},
		onDeleted: {deleted, true},
		onOthers:  {others, true},
	}
	check := func(how string, r *vv.TestRun) {
		t.Helper()
		w, ok := want[r.ID]
		if !ok {
			t.Fatalf("%s: run %s (%s) is not one of the project's", how, r.ID, r.Name)
		}
		got := ""
		if r.BaselineID != nil {
			got = *r.BaselineID
		}
		if got != w.baseline || r.BaselineDeleted != w.deleted {
			t.Errorf("%s: run %q names baseline %q, baseline_deleted %v; want %q, %v",
				how, r.Name, got, r.BaselineDeleted, w.baseline, w.deleted)
		}
	}
	for id := range want {
		r, err := runs.FindRunByID(id)
		if err != nil {
			t.Fatal(err)
		}
		check("FindRunByID", r)
	}
	listed, err := runs.ListRunsByProject(projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != len(want) {
		t.Fatalf("the project lists %d runs, want %d", len(listed), len(want))
	}
	for _, r := range listed {
		check("ListRunsByProject", r)
	}
}
