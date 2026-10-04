package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// An artifact's version history through the handlers, over the real
// artifact service and an in-memory store that keeps every version as the
// database does (REQ-4, #379's decision on REQ-4): a restore keeps the ref
// and is published, a version the artifact never had is not found, a
// deleted artifact's history stays readable to its project's viewers, and a
// figure upload takes the artifact to a new version.

// historyRepo is an artifacts.Repository over a slice of rows: an update
// closes the current row and appends the next, a delete closes it, and a
// row with no ref draws the next number of its prefix, as ensureRef does.
type historyRepo struct {
	artifacts.Repository
	rows []*artifacts.Artifact
	next map[string]int
}

func (m *historyRepo) mint(a *artifacts.Artifact) {
	if a.Ref != "" {
		return
	}
	prefix := artifacts.RefPrefix(a.Type)
	if m.next == nil {
		m.next = map[string]int{}
	}
	m.next[prefix]++
	a.Ref = artifacts.FormatRef(prefix, m.next[prefix])
}

func (m *historyRepo) Save(a *artifacts.Artifact) error {
	m.mint(a)
	copied := *a
	m.rows = append(m.rows, &copied)
	return nil
}

func (m *historyRepo) FindByID(id string) (*artifacts.Artifact, error) {
	for _, a := range m.rows {
		if a.ID == id && a.ValidTo == nil {
			copied := *a
			return &copied, nil
		}
	}
	return nil, artifacts.ErrNotFound
}

func (m *historyRepo) FindVersionsByID(id string) ([]*artifacts.Artifact, error) {
	var out []*artifacts.Artifact
	for i := len(m.rows) - 1; i >= 0; i-- {
		if m.rows[i].ID == id {
			copied := *m.rows[i]
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (m *historyRepo) close(id string, at time.Time) {
	for _, a := range m.rows {
		if a.ID == id && a.ValidTo == nil {
			a.ValidTo = &at
		}
	}
}

func (m *historyRepo) Update(a *artifacts.Artifact) error {
	m.mint(a)
	m.close(a.ID, a.ValidFrom)
	copied := *a
	m.rows = append(m.rows, &copied)
	return nil
}

func (m *historyRepo) Delete(id string) error {
	m.close(id, time.Now())
	return nil
}

func (m *historyRepo) NextSortOrder(string, *string) (int, error) { return 1, nil }

// historyAttachments stores a new figure, as CreateFigure does.
type historyAttachments struct {
	attachments.Service
	created []*attachments.Attachment
}

func (f *historyAttachments) CreateFigure(a *attachments.Attachment, artifactRef string) error {
	a.FigureRef, a.FigureNum = artifactRef+"-FIG-1", 1
	f.created = append(f.created, a)
	return nil
}

// historyFixture is project P (proj-1) of workspace W, with an editor, a
// viewer and an outsider who has no role in P, and requirement REQ-1 at
// version 2 (its body reworded).
func historyFixture(t *testing.T) (*Handler, *recordingBus, *historyRepo, string) {
	t.Helper()
	repo := &historyRepo{}
	svc := artifacts.NewDefaultService(repo)
	bus := &recordingBus{}
	h := NewHandler(HandlerDeps{
		ArtifactService:   svc,
		AttachmentService: &historyAttachments{},
		ChatterService:    &fakeChatterService{},
		LinkService:       &fakeLinkService{},
		Bus:               bus,
		UploadsDir:        t.TempDir(),
		ProjectService: &fakeProjectService{byID: map[string]*projects.Project{
			"proj-1": {ID: "proj-1", OrgID: "org-1"},
		}},
		OrgService: &fakeOrgService{roles: map[string]map[string]string{"org-1": {}}},
		MemberService: &fakeMemberService{roles: map[string]map[string]string{
			"proj-1": {"editor": members.RoleEditor, "viewer": members.RoleViewer},
		}},
	})
	req := artifacts.NewArtifact(artifacts.CreateArtifactRequest{ProjectID: "proj-1", Type: artifacts.TypeRequirement,
		Title: "Answer in time", Body: "The system shall answer within 2 s."})
	if err := svc.CreateArtifact(req); err != nil {
		t.Fatal(err)
	}
	body := "The system shall answer within 1 s."
	if _, err := svc.UpdateArtifact(req.ID, artifacts.UpdateArtifactRequest{Body: &body}); err != nil {
		t.Fatal(err)
	}
	return h, bus, repo, req.ID
}

// historyCall serves one request by the handler the route binds, as the
// router would, for the given account ("" for none).
func historyCall(h *Handler, method, target, body, user string, vars map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r = mux.SetURLVars(r, vars)
	if user != "" {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: user}))
	}
	w := httptest.NewRecorder()
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, "/restore"):
		h.RestoreArtifactVersion(w, r)
	case strings.HasSuffix(path, "/versions"):
		h.GetArtifactVersions(w, r)
	case strings.HasSuffix(path, "/links"):
		h.GetArtifactVersionLinks(w, r)
	}
	return w
}

// A restore keeps the artifact's ref, as every other version does, and is
// published as artifact.restored: the version written and the one brought
// back. It answered REQ-2, a ref minted for it, and published nothing.
func TestARestoreKeepsTheRefAndIsPublished(t *testing.T) {
	h, bus, _, id := historyFixture(t)

	w := historyCall(h, http.MethodPost, "/api/v1/artifacts/"+id+"/restore", `{"version":1}`, "editor",
		map[string]string{"id": id})
	if w.Code != http.StatusOK {
		t.Fatalf("restore = %d %s, want 200", w.Code, w.Body.String())
	}
	var got artifacts.Artifact
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Ref != "REQ-1" || got.Version != 3 || got.Body != "The system shall answer within 2 s." {
		t.Errorf("restored = ref %q version %d body %q, want REQ-1 at version 3 with version 1's body",
			got.Ref, got.Version, got.Body)
	}

	if len(bus.published) != 1 {
		t.Fatalf("published %v, want one artifact.restored", bus.types())
	}
	e := bus.published[0]
	if e.EventType != "artifact.restored" || e.ProjectID != "proj-1" || e.EntityID != id || e.Actor != "user:editor" {
		t.Errorf("event = %s %s %s by %s, want artifact.restored of the artifact in proj-1 by user:editor",
			e.EventType, e.ProjectID, e.EntityID, e.Actor)
	}
	want := map[string]interface{}{"artifact_type": "requirement", "title": "Answer in time", "version": 3, "restored_version": 1}
	for k, v := range want {
		if e.Payload[k] != v {
			t.Errorf("payload[%s] = %#v, want %#v", k, e.Payload[k], v)
		}
	}
	if len(e.Payload) != len(want) {
		t.Errorf("payload = %v, want the keys of %v", e.Payload, want)
	}
}

// A version the artifact never had is not found, as a version's links
// answer it; it answered 500, and wrote nothing then either.
func TestRestoringAVersionTheArtifactNeverHadIsNotFound(t *testing.T) {
	h, bus, repo, id := historyFixture(t)
	rows := len(repo.rows)

	for _, version := range []string{"99", "0", "-1"} {
		w := historyCall(h, http.MethodPost, "/api/v1/artifacts/"+id+"/restore", `{"version":`+version+`}`, "editor",
			map[string]string{"id": id})
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"artifact version not found"`) {
			t.Errorf("restore version %s = %d %s, want 404 artifact version not found", version, w.Code, w.Body.String())
		}
	}
	if len(repo.rows) != rows || len(bus.published) != 0 {
		t.Errorf("a refused restore wrote %d rows and published %v", len(repo.rows)-rows, bus.types())
	}
}

// A deleted artifact's history reads as the artifact did: its project's
// viewers read every version and a version's links, and an account with no
// role in the project gets the 404 an id no artifact has gets. The viewer
// got that 404 too, since the guard looked the project up through the
// artifact's current row, which the delete closes.
func TestADeletedArtifactsHistoryStaysReadableToItsReaders(t *testing.T) {
	h, _, _, id := historyFixture(t)
	if err := h.ArtifactService.DeleteArtifact(id); err != nil {
		t.Fatal(err)
	}

	w := historyCall(h, http.MethodGet, "/api/v1/artifacts/"+id+"/versions", "", "viewer", map[string]string{"id": id})
	if w.Code != http.StatusOK {
		t.Fatalf("the viewer's read of the deleted artifact's versions = %d %s, want 200", w.Code, w.Body.String())
	}
	var versions []*artifacts.Artifact
	if err := json.Unmarshal(w.Body.Bytes(), &versions); err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].Version != 2 || versions[0].ValidTo == nil || versions[1].Version != 1 {
		t.Errorf("versions = %d rows, want versions 2 and 1, every one closed", len(versions))
	}

	w = historyCall(h, http.MethodGet, "/api/v1/artifacts/"+id+"/links?version=1", "", "viewer", map[string]string{"id": id})
	if w.Code != http.StatusOK {
		t.Errorf("the viewer's read of a deleted artifact's version links = %d %s, want 200", w.Code, w.Body.String())
	}

	phantom := "11111111-1111-4111-8111-111111111111"
	for _, c := range []struct{ user, id, target string }{
		{"outsider", id, "/versions"},
		{"outsider", id, "/links?version=1"},
		{"viewer", phantom, "/versions"},
		{"viewer", phantom, "/links?version=1"},
	} {
		w := historyCall(h, http.MethodGet, "/api/v1/artifacts/"+c.id+c.target, "", c.user, map[string]string{"id": c.id})
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"project not found"`) {
			t.Errorf("%s reads %s of %s = %d %s, want 404 project not found", c.user, c.target, c.id, w.Code, w.Body.String())
		}
	}

	// The live links are the current artifact's: a deleted one has none to
	// read, and answers as before.
	w = historyCall(h, http.MethodGet, "/api/v1/artifacts/"+id+"/links", "", "viewer", map[string]string{"id": id})
	if w.Code != http.StatusNotFound {
		t.Errorf("the viewer's read of a deleted artifact's live links = %d, want 404", w.Code)
	}
}

// A figure upload takes the artifact to a new version, as a figure's new
// file and its rename do, through an update that changes nothing it says.
// It left the artifact at its version.
func TestAFigureUploadTakesTheArtifactToANewVersion(t *testing.T) {
	h, _, repo, id := historyFixture(t)

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("artifact_id", id)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="wiring.png"`)
	header.Set("Content-Type", "image/png")
	part, _ := form.CreatePart(header)
	_, _ = part.Write([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00"))
	_ = form.Close()

	r := httptest.NewRequest(http.MethodPost, "/api/v1/attachments/upload", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "editor"}))
	w := httptest.NewRecorder()
	h.UploadAttachment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload = %d %s, want 201", w.Code, w.Body.String())
	}

	current, err := repo.FindByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != 3 || current.Ref != "REQ-1" || current.Body != "The system shall answer within 1 s." {
		t.Errorf("after the upload the artifact is %s at version %d, body %q; want REQ-1 at version 3, its body as it was",
			current.Ref, current.Version, current.Body)
	}
	if len(repo.rows) != 3 {
		t.Errorf("the store holds %d versions, want 3", len(repo.rows))
	}
}
