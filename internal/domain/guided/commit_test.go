package guided

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/events"
)

// storedArtifactRepo is an artifacts.Repository that answers as the Postgres
// one does for the calls a commit makes: a read returns a fresh artifact
// decoded from what was stored (the attributes through JSON, as from JSONB),
// never the caller's pointer; an update closes the current version and
// stores the new one; a delete closes the current version. It sits behind
// the real artifacts.DefaultService, not in its place: the commit has to
// meet the real UpdateArtifact and ChangeStatus, whose status mirror is what
// swallowed the approval, and an artifacts.Service fake that stored the
// attributes it was handed would have kept "approved" and hidden the bug.
type storedArtifactRepo struct {
	artifacts.Repository
	t        *testing.T
	current  map[string][]byte
	archived map[string][][]byte // oldest first
	// failUpdate names an artifact whose updates fail, as a store that has
	// gone away would.
	failUpdate string
}

func newStoredArtifactRepo(t *testing.T) *storedArtifactRepo {
	return &storedArtifactRepo{t: t, current: map[string][]byte{}, archived: map[string][][]byte{}}
}

func (r *storedArtifactRepo) encode(a *artifacts.Artifact) []byte {
	b, err := json.Marshal(a)
	if err != nil {
		r.t.Fatalf("encode artifact: %v", err)
	}
	return b
}

func (r *storedArtifactRepo) decode(b []byte) *artifacts.Artifact {
	var a artifacts.Artifact
	if err := json.Unmarshal(b, &a); err != nil {
		r.t.Fatalf("decode artifact: %v", err)
	}
	return &a
}

func (r *storedArtifactRepo) close(id string, at time.Time) {
	closed := r.decode(r.current[id])
	closed.ValidTo = &at
	r.archived[id] = append(r.archived[id], r.encode(closed))
	delete(r.current, id)
}

func (r *storedArtifactRepo) Save(a *artifacts.Artifact) error {
	r.current[a.ID] = r.encode(a)
	return nil
}

func (r *storedArtifactRepo) FindByID(id string) (*artifacts.Artifact, error) {
	b, ok := r.current[id]
	if !ok {
		return nil, artifacts.ErrNotFound
	}
	return r.decode(b), nil
}

func (r *storedArtifactRepo) Update(a *artifacts.Artifact) error {
	if a.ID == r.failUpdate {
		return errors.New("store unavailable")
	}
	if _, ok := r.current[a.ID]; !ok {
		return artifacts.ErrNotFound
	}
	r.close(a.ID, a.ValidFrom)
	r.current[a.ID] = r.encode(a)
	return nil
}

func (r *storedArtifactRepo) Delete(id string) error {
	if _, ok := r.current[id]; ok {
		r.close(id, time.Now())
	}
	return nil
}

func (r *storedArtifactRepo) NextSortOrder(projectID string, parentID *string) (int, error) {
	return len(r.current) + 1, nil
}

// FindVersionsByID answers newest first, as the Postgres query orders them.
func (r *storedArtifactRepo) FindVersionsByID(id string) ([]*artifacts.Artifact, error) {
	var versions []*artifacts.Artifact
	if b, ok := r.current[id]; ok {
		versions = append(versions, r.decode(b))
	}
	for i := len(r.archived[id]) - 1; i >= 0; i-- {
		versions = append(versions, r.decode(r.archived[id][i]))
	}
	return versions, nil
}

// recordingChatter keeps every note written, in order.
type recordingChatter struct {
	chatter.Service
	entries []*chatter.ChatterEntry
}

func (c *recordingChatter) CreateEntry(e *chatter.ChatterEntry) error {
	c.entries = append(c.entries, e)
	return nil
}

func (c *recordingChatter) notes(artifactID string) []string {
	var out []string
	for _, e := range c.entries {
		if e.ArtifactID == artifactID {
			out = append(out, e.EntryType+": "+e.Message)
		}
	}
	return out
}

// recordingBus keeps every event published, in order.
type recordingBus struct{ published []events.Event }

func (b *recordingBus) Publish(e events.Event)          { b.published = append(b.published, e) }
func (b *recordingBus) Subscribe(fn func(events.Event)) {}

type commitFixture struct {
	svc       *DefaultService
	sessions  *fakeRepo
	store     *storedArtifactRepo
	artifacts *artifacts.DefaultService
	chatter   *recordingChatter
	bus       *recordingBus
}

func newCommitFixture(t *testing.T) *commitFixture {
	f := &commitFixture{
		sessions: &fakeRepo{session: &Session{
			ID:               "gs-1",
			ProjectID:        "proj-1",
			Status:           StatusInProgress,
			Answers:          map[string]interface{}{},
			DraftArtifactIDs: []string{},
		}},
		store:   newStoredArtifactRepo(t),
		chatter: &recordingChatter{},
		bus:     &recordingBus{},
	}
	f.artifacts = artifacts.NewDefaultService(f.store)
	f.svc = NewDefaultService(f.sessions, f.artifacts, nil, f.chatter, nil, f.bus)
	return f
}

// approvedIDs lists the ids of the artifacts a commit reported approving,
// checking that each is reported as the approval left it.
func approvedIDs(t *testing.T, result *CommitResult) []string {
	t.Helper()
	ids := []string{}
	for _, a := range result.Approved {
		if a.Status != artifacts.StatusApproved {
			t.Errorf("reported approval of %s has status %q", a.ID, a.Status)
		}
		ids = append(ids, a.ID)
	}
	return ids
}

func (f *commitFixture) materialize(t *testing.T, drafts ...DraftSpec) []string {
	t.Helper()
	ids, err := f.svc.MaterializeDrafts("gs-1", drafts)
	if err != nil {
		t.Fatalf("MaterializeDrafts: %v", err)
	}
	return ids
}

func (f *commitFixture) statuses(t *testing.T, id string) []string {
	t.Helper()
	versions, err := f.artifacts.GetArtifactVersions(id)
	if err != nil {
		t.Fatalf("GetArtifactVersions(%s): %v", id, err)
	}
	var out []string
	for i := len(versions) - 1; i >= 0; i-- {
		out = append(out, versions[i].Status)
	}
	return out
}

// Committing a guided session is the wizard's sign-off: every draft it
// materialised comes out approved, in the status column and in the
// attribute mirror alike, having walked the review state machine (draft ->
// in_review -> approved, one version each) with the note each step leaves,
// and its other attributes untouched. The commit reports each approval, as
// the approval left it, for the handler to publish with the committing user
// as actor; the service publishes nothing itself. Before the fix the commit
// wrote status "approved" into the attributes through UpdateArtifact, whose
// mirror rewrote it from the column, so every draft stayed a draft at
// version 2.
func TestCommitApprovesTheSessionsDrafts(t *testing.T) {
	f := newCommitFixture(t)
	ids := f.materialize(t,
		DraftSpec{Type: "requirement", Title: "Warn before the spindle starts",
			Attributes: map[string]interface{}{"verification_method": "test"}},
		DraftSpec{Type: "hazard", Title: "Spindle starts with the guard open",
			Attributes: map[string]interface{}{"status": "approved"}},
	)

	result, err := f.svc.Commit("gs-1")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if result.Session.Status != StatusCommitted {
		t.Fatalf("session status = %s, want committed", result.Session.Status)
	}
	if got := approvedIDs(t, result); !reflect.DeepEqual(got, ids) {
		t.Errorf("reported approvals %v, want the session's drafts %v", got, ids)
	}
	for _, a := range result.Approved {
		if a.Version != 3 {
			t.Errorf("reported approval of %s at version %d, want 3", a.ID, a.Version)
		}
	}

	for i, id := range ids {
		a, err := f.artifacts.GetArtifact(id)
		if err != nil {
			t.Fatalf("GetArtifact(%s): %v", id, err)
		}
		if a.Status != artifacts.StatusApproved || a.Attributes["status"] != artifacts.StatusApproved {
			t.Errorf("draft %d: status %q, attributes.status %v, want both approved", i, a.Status, a.Attributes["status"])
		}
		if a.Version != 3 {
			t.Errorf("draft %d: version %d, want 3 (the draft, in review, approved)", i, a.Version)
		}
		if got, want := f.statuses(t, id), []string{"draft", "in_review", "approved"}; !reflect.DeepEqual(got, want) {
			t.Errorf("draft %d: version statuses %v, want %v", i, got, want)
		}
		wantNotes := []string{
			"status-change: Status changed: draft → in_review (guided definition)",
			"status-change: Status changed: in_review → approved (guided definition)",
			"guided-flow: Created via guided definition",
		}
		if got := f.chatter.notes(id); !reflect.DeepEqual(got, wantNotes) {
			t.Errorf("draft %d: notes %q, want %q", i, got, wantNotes)
		}
	}
	if a, _ := f.artifacts.GetArtifact(ids[0]); a.Attributes["verification_method"] != "test" {
		t.Errorf("the commit lost an attribute: %v", a.Attributes)
	}

	if len(f.bus.published) != 0 {
		t.Errorf("the service published %+v; the handler publishes the approvals, with the actor", f.bus.published)
	}
}

// A draft is taken to approved from wherever it stands at commit time: one
// already in review needs only the approval; one already approved, or
// superseded (terminal), is left as it is, with no new version; one deleted
// since it was materialised is skipped. Only the draft the commit approved
// is reported as an approval. Every draft still in the project gets the
// commit's note.
func TestCommitTakesEachDraftFromWhereItStands(t *testing.T) {
	f := newCommitFixture(t)
	ids := f.materialize(t,
		DraftSpec{Type: "requirement", Title: "In review"},
		DraftSpec{Type: "requirement", Title: "Approved"},
		DraftSpec{Type: "requirement", Title: "Superseded"},
		DraftSpec{Type: "requirement", Title: "Deleted"},
	)
	inReview, approved, superseded, deleted := ids[0], ids[1], ids[2], ids[3]
	for _, move := range []struct {
		id string
		to []string
	}{
		{inReview, []string{artifacts.StatusInReview}},
		{approved, []string{artifacts.StatusInReview, artifacts.StatusApproved}},
		{superseded, []string{artifacts.StatusInReview, artifacts.StatusApproved, artifacts.StatusSuperseded}},
	} {
		for _, to := range move.to {
			if _, err := f.artifacts.ChangeStatus(move.id, to); err != nil {
				t.Fatalf("ChangeStatus(%s, %s): %v", move.id, to, err)
			}
		}
	}
	if err := f.artifacts.DeleteArtifact(deleted); err != nil {
		t.Fatalf("DeleteArtifact: %v", err)
	}

	result, err := f.svc.Commit("gs-1")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if got, want := approvedIDs(t, result), []string{inReview}; !reflect.DeepEqual(got, want) {
		t.Errorf("reported approvals %v, want only the draft that was in review %v", got, want)
	}

	for _, c := range []struct {
		id, name, status string
		version          int
		notes            []string
	}{
		{inReview, "in review", artifacts.StatusApproved, 3, []string{
			"status-change: Status changed: in_review → approved (guided definition)",
			"guided-flow: Created via guided definition",
		}},
		{approved, "approved", artifacts.StatusApproved, 3, []string{"guided-flow: Created via guided definition"}},
		{superseded, "superseded", artifacts.StatusSuperseded, 4, []string{"guided-flow: Created via guided definition"}},
	} {
		a, err := f.artifacts.GetArtifact(c.id)
		if err != nil {
			t.Fatalf("GetArtifact(%s): %v", c.name, err)
		}
		if a.Status != c.status || a.Attributes["status"] != c.status || a.Version != c.version {
			t.Errorf("%s: status %q, attributes.status %v, version %d; want %s at version %d",
				c.name, a.Status, a.Attributes["status"], a.Version, c.status, c.version)
		}
		if got := f.chatter.notes(c.id); !reflect.DeepEqual(got, c.notes) {
			t.Errorf("%s: notes %q, want %q", c.name, got, c.notes)
		}
	}
	if got := f.chatter.notes(deleted); got != nil {
		t.Errorf("the deleted draft got notes: %q", got)
	}
	if len(f.bus.published) != 0 {
		t.Errorf("the service published %+v; the handler publishes the approvals, with the actor", f.bus.published)
	}
}

// A commit that fails part way has still approved the drafts before the
// failure, and a retry leaves those alone, so it hands them back with the
// error for the handler to record. The session stays in progress.
func TestCommitReportsTheApprovalsMadeBeforeAFailure(t *testing.T) {
	f := newCommitFixture(t)
	ids := f.materialize(t,
		DraftSpec{Type: "requirement", Title: "Approved before the failure"},
		DraftSpec{Type: "requirement", Title: "Fails"},
		DraftSpec{Type: "requirement", Title: "Never reached"},
	)
	f.store.failUpdate = ids[1]

	result, err := f.svc.Commit("gs-1")
	if err == nil {
		t.Fatal("Commit succeeded over a failing store")
	}
	if result == nil {
		t.Fatal("Commit returned no result with its error: the approval made before the failure would go unrecorded")
	}
	if got, want := approvedIDs(t, result), ids[:1]; !reflect.DeepEqual(got, want) {
		t.Errorf("reported approvals %v, want %v", got, want)
	}
	if result.Session != nil {
		t.Errorf("a failed commit returned the session %+v", result.Session)
	}
	if f.sessions.session.Status != StatusInProgress {
		t.Errorf("session status = %s after a failed commit, want in progress", f.sessions.session.Status)
	}
	if a, _ := f.artifacts.GetArtifact(ids[2]); a.Status != artifacts.StatusDraft {
		t.Errorf("the draft after the failure is %s, want draft", a.Status)
	}
}
