package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/projects"
)

// deletingAttachments holds one figure and deletes it, answering the stored
// files the delete took, or fails with err. It records whether the figure's
// current file was still on disk when the delete was asked for.
type deletingAttachments struct {
	attachments.Service
	figure       *attachments.Attachment
	files        []string
	err          error
	fileAtDelete bool
}

func (f *deletingAttachments) GetAttachment(id string) (*attachments.Attachment, error) {
	if f.figure != nil && f.figure.ID == id {
		return f.figure, nil
	}
	return nil, nil
}

func (f *deletingAttachments) DeleteAttachment(id string) ([]string, error) {
	_, err := os.Stat(f.figure.FilePath)
	f.fileAtDelete = err == nil
	if f.err != nil {
		return nil, f.err
	}
	return f.files, nil
}

// attachmentDeleteFixture is a figure on artifact art-1 of project proj-1,
// its current file and an earlier version's written to a directory of the
// test's own.
func attachmentDeleteFixture(t *testing.T, svc *deletingAttachments) (h *Handler, current, earlier string) {
	t.Helper()
	dir := t.TempDir()
	current, earlier = filepath.Join(dir, "v2_seal.png"), filepath.Join(dir, "v1_seal.png")
	for _, f := range []string{current, earlier} {
		if err := os.WriteFile(f, []byte("bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	svc.figure = &attachments.Attachment{ID: "att-1", ArtifactID: "art-1", FilePath: current, Version: 2}
	h = newTestHandler(t,
		withProjectService(&fakeProjectService{byID: map[string]*projects.Project{"proj-1": {ID: "proj-1", OrgID: "org-1"}}}),
		func(h *Handler) {
			h.ArtifactService = &fakeArtifactService{byID: map[string]*artifacts.Artifact{"art-1": {ID: "art-1", ProjectID: "proj-1"}}}
			h.AttachmentService = svc
		})
	return h, current, earlier
}

// Deleting a figure removes the stored file of every version of it, once
// the rows are gone (#379 bug 145: only the current version's file was
// removed, before the row was, and the earlier versions' stayed on disk). A
// file already gone is no failure.
func TestDeletingAFigureRemovesEveryVersionsFile(t *testing.T) {
	svc := &deletingAttachments{}
	h, current, earlier := attachmentDeleteFixture(t, svc)
	svc.files = []string{earlier, current, filepath.Join(filepath.Dir(current), "already gone")}

	w := httptest.NewRecorder()
	h.DeleteAttachment(w, agentReq(http.MethodDelete, "/api/v1/attachments/att-1", map[string]string{"id": "att-1"}, ""))
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/v1/attachments/att-1: %d %s, want 204", w.Code, w.Body.String())
	}
	if !svc.fileAtDelete {
		t.Errorf("the figure's file was removed before its rows were deleted")
	}
	for _, f := range []string{current, earlier} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("%s after the delete: %v, want it removed", filepath.Base(f), err)
		}
	}
}

// A figure delete that fails removes no file: the figure is still there,
// and its file with it (#379 bug 145: the current file went first, so a
// failed delete left a figure whose file was gone).
func TestAFailedFigureDeleteRemovesNoFile(t *testing.T) {
	svc := &deletingAttachments{err: errors.New("the database went away")}
	h, current, earlier := attachmentDeleteFixture(t, svc)

	w := httptest.NewRecorder()
	h.DeleteAttachment(w, agentReq(http.MethodDelete, "/api/v1/attachments/att-1", map[string]string{"id": "att-1"}, ""))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("a figure delete that fails: %d %s, want 500", w.Code, w.Body.String())
	}
	for _, f := range []string{current, earlier} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s after a failed delete: %v, want it left", filepath.Base(f), err)
		}
	}
}
