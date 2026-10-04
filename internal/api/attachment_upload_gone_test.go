package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/projects"
)

// goneAttachments refuses every new figure as one whose project went while
// it was uploaded.
type goneAttachments struct{ attachments.Service }

func (goneAttachments) CreateFigure(*attachments.Attachment, string) error {
	return attachments.ErrNoArtifact
}

// A figure whose project is deleted while its file comes in is answered as
// a figure for an artifact no row has, 404 "project not found", and its
// stored file is removed (#379 bug 152: the figure went in with no project,
// leaving an orphan row, counter and file).
func TestAFigureWhoseProjectWentMeanwhileIsAnsweredAsNotFound(t *testing.T) {
	dir := t.TempDir()
	h := newTestHandler(t,
		withProjectService(&fakeProjectService{byID: map[string]*projects.Project{"proj-1": {ID: "proj-1", OrgID: "org-1"}}}),
		func(h *Handler) {
			h.ArtifactService = &fakeArtifactService{byID: map[string]*artifacts.Artifact{"art-1": {ID: "art-1", ProjectID: "proj-1", Ref: "REQ-1"}}}
			h.AttachmentService = goneAttachments{}
			h.UploadsDir = dir
		})

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("artifact_id", "art-1")
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="wiring.png"`)
	header.Set("Content-Type", "image/png")
	part, _ := form.CreatePart(header)
	_, _ = part.Write([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00"))
	_ = form.Close()
	r := agentReq(http.MethodPost, "/api/v1/attachments/upload", nil, body.String())
	r.Header.Set("Content-Type", form.FormDataContentType())

	w := httptest.NewRecorder()
	h.UploadAttachment(w, r)
	if w.Code != http.StatusNotFound || !bytes.Contains(w.Body.Bytes(), []byte(`"project not found"`)) {
		t.Errorf("an upload whose project went meanwhile = %d %s, want 404 project not found", w.Code, w.Body.String())
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("the uploads directory after the refused upload holds %d entries (%v), want none", len(entries), err)
	}
}
