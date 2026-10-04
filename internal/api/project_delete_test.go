package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/projects"
)

// removingProjects deletes its one project, answering what the delete
// removed, or fails with err.
type removingProjects struct {
	fakeProjectService
	removed *projects.Removed
	err     error
	deleted []string
}

func (f *removingProjects) DeleteProject(id string) (*projects.Removed, error) {
	f.deleted = append(f.deleted, id)
	if f.err != nil {
		return nil, f.err
	}
	return f.removed, nil
}

// announcingRuns records the runs it is told were cancelled.
type announcingRuns struct {
	fakeRunService
	announced [][]string
}

func (f *announcingRuns) AnnounceCancelled(ids []string) {
	f.announced = append(f.announced, ids)
}

// Deleting a project removes the stored files of the figures and evidence
// the delete took with it, once it has committed, and announces the runs it
// cancelled (#379 bugs 136 and 137: the files stayed on disk, and the runs
// went on). A file already gone, and one that will not go, are logged, not
// answered: the project is deleted, 204, either way.
func TestDeletingAProjectRemovesItsFilesAndAnnouncesItsRuns(t *testing.T) {
	dir := t.TempDir()
	figure, evidence := filepath.Join(dir, "figure.png"), filepath.Join(dir, "evidence-1")
	stuck := filepath.Join(dir, "stuck") // a directory with a file in it, which os.Remove refuses
	for _, f := range []string{figure, evidence, filepath.Join(stuck, "inside")} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	kept := filepath.Join(dir, "another project's figure.png")
	if err := os.WriteFile(kept, []byte("bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	projectSvc := &removingProjects{
		fakeProjectService: fakeProjectService{byID: map[string]*projects.Project{"proj-1": {ID: "proj-1", OrgID: "org-1"}}},
		removed: &projects.Removed{
			Files:         []string{evidence, figure, filepath.Join(dir, "already gone"), stuck},
			CancelledRuns: []string{"run-queued", "run-running"},
		},
	}
	runs := &announcingRuns{}
	h := newTestHandler(t, withProjectService(projectSvc), withRunService(runs))

	w := httptest.NewRecorder()
	h.DeleteProject(w, agentReq(http.MethodDelete, "/api/v1/projects/proj-1", map[string]string{"id": "proj-1"}, ""))
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/v1/projects/proj-1: %d %s, want 204", w.Code, w.Body.String())
	}
	for _, f := range []string{figure, evidence} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("%s after the delete: %v, want it removed", filepath.Base(f), err)
		}
	}
	for _, f := range []string{kept, stuck} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s after the delete: %v, want it left", filepath.Base(f), err)
		}
	}
	if want := [][]string{{"run-queued", "run-running"}}; !reflect.DeepEqual(runs.announced, want) {
		t.Errorf("runs announced as cancelled: %q, want %q", runs.announced, want)
	}
}

// A delete that fails removes no file and announces no run: nothing it
// would have taken went.
func TestAFailedProjectDeleteRemovesNothing(t *testing.T) {
	figure := filepath.Join(t.TempDir(), "figure.png")
	if err := os.WriteFile(figure, []byte("bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		err  error
		code int
	}{{projects.ErrNotFound, http.StatusNotFound}, {errors.New("the database went away"), http.StatusInternalServerError}} {
		projectSvc := &removingProjects{
			fakeProjectService: fakeProjectService{byID: map[string]*projects.Project{"proj-1": {ID: "proj-1", OrgID: "org-1"}}},
			err:                c.err,
		}
		runs := &announcingRuns{}
		h := newTestHandler(t, withProjectService(projectSvc), withRunService(runs))
		w := httptest.NewRecorder()
		h.DeleteProject(w, agentReq(http.MethodDelete, "/api/v1/projects/proj-1", map[string]string{"id": "proj-1"}, ""))
		if w.Code != c.code {
			t.Errorf("a delete failing with %v: %d %s, want %d", c.err, w.Code, w.Body.String(), c.code)
		}
		if _, err := os.Stat(figure); err != nil {
			t.Errorf("a file after a delete failing with %v: %v, want it left", c.err, err)
		}
		if len(runs.announced) != 0 {
			t.Errorf("runs announced after a delete failing with %v: %q, want none", c.err, runs.announced)
		}
	}
}
