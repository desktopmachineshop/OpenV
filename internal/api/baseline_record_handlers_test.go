package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/downloads"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/reports"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// Baselines as a project's record (REQ-5, #379's decision on REQ-5 and
// REQ-6): a snapshot keeps the attribute definitions beside the export, a
// project owner's delete is published, and a baseline no row has answers
// not found on every route that takes one.

// recordedBaselines is a baselines.Repository over a map that keeps what
// Create stores and forgets what Delete removes.
type recordedBaselines struct {
	baselines.Repository
	byID map[string]*baselines.Baseline
}

func (f *recordedBaselines) Create(b *baselines.Baseline) error {
	f.byID[b.ID] = b
	return nil
}

func (f *recordedBaselines) GetByID(id string) (*baselines.Baseline, error) {
	if b, ok := f.byID[id]; ok {
		return b, nil
	}
	return nil, baselines.ErrNotFound
}

func (f *recordedBaselines) Delete(id string) error {
	delete(f.byID, id)
	return nil
}

// baselineRecordFixture is project P (proj-1) of workspace org-1, with an
// owner, an editor and a viewer, and baseline B1 of P.
func baselineRecordFixture(t *testing.T, exportSvc *fakeExportService) (*Handler, *recordedBaselines, *recordingBus) {
	repo := &recordedBaselines{byID: map[string]*baselines.Baseline{
		"b1": {ID: "b1", ProjectID: "proj-1", Name: "Release 1", Snapshot: json.RawMessage(`{"artifacts":[]}`)},
	}}
	baselineSvc := baselines.NewService(repo)
	h := vvRoutesHandler(t, map[string]*projects.Project{"proj-1": {ID: "proj-1", OrgID: "org-1"}},
		map[string]map[string]string{"proj-1": {
			"owner": members.RoleOwner, "editor": members.RoleEditor, "viewer": members.RoleViewer,
		}}, exportSvc, baselineSvc, &fakeVVService{})
	h.DownloadService = downloads.NewService(exportSvc, reports.NewService(exportSvc, baselineSvc))
	bus := &recordingBus{}
	h.Bus = bus
	return h, repo, bus
}

// baselineRoute serves one request through the router as the given account.
func baselineRoute(h *Handler, method, target, body, user string) *httptest.ResponseRecorder {
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: user}))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

// A baseline keeps the attribute definitions in effect beside the export:
// the snapshot is the export service's Snapshot, not the plain JSON export,
// which leaves them out. It stored the plain export.
func TestABaselineSnapshotKeepsTheAttributeDefinitions(t *testing.T) {
	exportSvc := &fakeExportService{
		data:     []byte(`{"artifacts":[]}`),
		snapshot: []byte(`{"artifacts":[],"attribute_definitions":[{"key":"risk","data_type":"enum"}]}`),
	}
	h, repo, _ := baselineRecordFixture(t, exportSvc)

	w := baselineRoute(h, http.MethodPost, "/api/v1/projects/proj-1/baselines", `{"name":"Release 2"}`, "editor")
	if w.Code != http.StatusCreated {
		t.Fatalf("capture = %d %s, want 201", w.Code, w.Body.String())
	}
	var created baselines.Baseline
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	stored := repo.byID[created.ID]
	if stored == nil || !strings.Contains(string(stored.Snapshot), `"attribute_definitions"`) {
		t.Errorf("stored snapshot = %v, want the export with its attribute definitions", stored)
	}
}

// A project owner may delete a baseline, and the delete is published as
// baseline.deleted under the name the baseline had; an editor may not, and
// a refused or missing delete publishes nothing. The delete published no
// event.
func TestAnOwnersBaselineDeleteIsPublished(t *testing.T) {
	h, repo, bus := baselineRecordFixture(t, &fakeExportService{})

	if w := baselineRoute(h, http.MethodDelete, "/api/v1/baselines/b1", "", "editor"); w.Code != http.StatusForbidden {
		t.Errorf("the editor's delete = %d %s, want 403", w.Code, w.Body.String())
	}
	if len(bus.published) != 0 || repo.byID["b1"] == nil {
		t.Fatalf("a refused delete published %v and removed the baseline: %v", bus.types(), repo.byID["b1"] == nil)
	}

	if w := baselineRoute(h, http.MethodDelete, "/api/v1/baselines/b1", "", "owner"); w.Code != http.StatusNoContent {
		t.Fatalf("the owner's delete = %d %s, want 204", w.Code, w.Body.String())
	}
	if repo.byID["b1"] != nil {
		t.Error("the owner's delete left the baseline")
	}
	if len(bus.published) != 1 {
		t.Fatalf("published %v, want one baseline.deleted", bus.types())
	}
	e := bus.published[0]
	if e.EventType != "baseline.deleted" || e.ProjectID != "proj-1" || e.EntityID != "b1" || e.Actor != "user:owner" ||
		e.OrgID != "org-1" || len(e.Payload) != 1 || e.Payload["name"] != "Release 1" {
		t.Errorf("event = %+v, want baseline.deleted of b1 in proj-1 by user:owner, payload {name: Release 1}", e)
	}

	if w := baselineRoute(h, http.MethodDelete, "/api/v1/baselines/b1", "", "owner"); w.Code != http.StatusNotFound {
		t.Errorf("a second delete = %d, want 404", w.Code)
	}
	if len(bus.published) != 1 {
		t.Errorf("a delete of nothing published %v", bus.types()[1:])
	}
}

// A baseline no row has, a malformed id and another project's baseline
// answer 404 on every route that takes a baseline (REQ-6), whichever format
// or read it names: the download answered 500 until #419, and this pins
// every route beside it.
func TestAMissingBaselineIsNotFoundOnEveryRouteThatTakesOne(t *testing.T) {
	h, repo, _ := baselineRecordFixture(t, &fakeExportService{})
	repo.byID["b-other"] = &baselines.Baseline{ID: "b-other", ProjectID: "proj-2", Name: "Other"}

	for _, id := range []string{"11111111-1111-4111-8111-111111111111", "not-a-uuid", "b-other"} {
		targets := []string{
			"/api/v1/projects/proj-1/report?baseline_id=" + id,
			"/api/v1/projects/proj-1/report?format=docx&baseline_id=" + id,
			"/api/v1/projects/proj-1/download/options?baseline_id=" + id,
			"/api/v1/projects/proj-1/vv/coverage?baseline_id=" + id,
			"/api/v1/projects/proj-1/vv/matrix?baseline_id=" + id,
			"/api/v1/projects/proj-1/vv/gaps?baseline_id=" + id,
			"/api/v1/projects/proj-1/vv/report?baseline_id=" + id,
			"/api/v1/projects/proj-1/quality?baseline_id=" + id,
			"/api/v1/projects/proj-1/impact?artifact=a1&baseline_id=" + id,
			"/api/v1/projects/proj-1/ai-map?baseline_id=" + id,
			"/api/v1/baselines/b1/diff?against=" + id,
		}
		for _, format := range downloads.Formats {
			targets = append(targets, "/api/v1/projects/proj-1/download/"+string(format)+"?baseline_id="+id)
		}
		if id != "b-other" {
			targets = append(targets, "/api/v1/baselines/"+id, "/api/v1/baselines/"+id+"/diff?against=live")
		}
		for _, target := range targets {
			w := baselineRoute(h, http.MethodGet, target, "", "viewer")
			if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "baseline not found") {
				t.Errorf("GET %s = %d %s, want 404 baseline not found", target, w.Code, w.Body.String())
			}
		}
		if id != "b-other" {
			w := baselineRoute(h, http.MethodDelete, "/api/v1/baselines/"+id, "", "owner")
			if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "baseline not found") {
				t.Errorf("DELETE %s = %d %s, want 404 baseline not found", id, w.Code, w.Body.String())
			}
		}
	}
}

// runRecordingVV records the runs CreateRun is asked to store.
type runRecordingVV struct {
	fakeVVService
	created []vv.CreateRunRequest
}

func (f *runRecordingVV) CreateRun(req vv.CreateRunRequest, createdBy *string) (*vv.TestRun, error) {
	f.created = append(f.created, req)
	return &vv.TestRun{ID: "run-1", ProjectID: req.ProjectID, Name: req.Name, BaselineID: req.BaselineID}, nil
}

// A test run's baseline_id is the one baseline a route takes in its body, and
// it answers as every other route answers a baseline that does not exist
// (REQ-6, #379's decision): an id no baseline has, a malformed or empty one
// and another project's answer 404 baseline not found, after the project's
// guard and the body's decode, and nothing is stored; the project's own
// baseline is stored, and a run with no baseline_id, or a null one, has none.
// The create stored any well-formed id, and answered a malformed one 400 with
// the driver's text.
func TestATestRunsBaselineIsOneOfItsProject(t *testing.T) {
	h, repo, _ := baselineRecordFixture(t, &fakeExportService{})
	repo.byID["b-other"] = &baselines.Baseline{ID: "b-other", ProjectID: "proj-2", Name: "Other"}
	runs := &runRecordingVV{}
	h.VVService = runs
	const route = "/api/v1/projects/proj-1/test-runs"

	for _, id := range []string{"11111111-1111-4111-8111-111111111111", "not-a-uuid", "", "b-other"} {
		body := `{"name":"Run","baseline_id":"` + id + `"}`
		w := baselineRoute(h, http.MethodPost, route, body, "editor")
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "baseline not found") {
			t.Errorf("create with baseline_id %q = %d %s, want 404 baseline not found", id, w.Code, w.Body.String())
		}
	}
	if w := baselineRoute(h, http.MethodPost, route, `{"name":"Run","baseline_id":"not-a-uuid"}`, "viewer"); w.Code != http.StatusForbidden {
		t.Errorf("the viewer's create = %d %s, want the guard's 403 before the baseline is looked up", w.Code, w.Body.String())
	}
	if w := baselineRoute(h, http.MethodPost, route, `{"name":"Run","baseline_id":`, "editor"); w.Code != http.StatusBadRequest {
		t.Errorf("a malformed body = %d %s, want the decode's 400", w.Code, w.Body.String())
	}
	if len(runs.created) != 0 {
		t.Fatalf("refused creates stored %d runs", len(runs.created))
	}

	for _, body := range []string{`{"name":"Run","baseline_id":"b1"}`, `{"name":"Run"}`, `{"name":"Run","baseline_id":null}`} {
		if w := baselineRoute(h, http.MethodPost, route, body, "editor"); w.Code != http.StatusCreated {
			t.Errorf("create %s = %d %s, want 201", body, w.Code, w.Body.String())
		}
	}
	if len(runs.created) != 3 || runs.created[0].BaselineID == nil || *runs.created[0].BaselineID != "b1" ||
		runs.created[1].BaselineID != nil || runs.created[2].BaselineID != nil {
		t.Errorf("stored runs = %+v, want one on b1 and two with no baseline", runs.created)
	}
}
