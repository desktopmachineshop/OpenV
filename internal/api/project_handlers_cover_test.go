package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// memCoverRepo is projects.CoverRepository in memory.
type memCoverRepo struct {
	covers map[string]*projects.Cover
	known  map[string]bool // the project ids SetCover accepts
	clock  time.Time
}

func (m *memCoverRepo) GetCover(id string) (*projects.Cover, error) {
	if c, ok := m.covers[id]; ok {
		return c, nil
	}
	return nil, projects.ErrNoCover
}

func (m *memCoverRepo) SetCover(id, mime string, data []byte) (time.Time, error) {
	if !m.known[id] {
		return time.Time{}, projects.ErrNotFound
	}
	m.clock = m.clock.Add(time.Minute)
	m.covers[id] = &projects.Cover{ProjectID: id, Mime: mime, Data: data, UpdatedAt: m.clock}
	return m.clock, nil
}

func (m *memCoverRepo) DeleteCover(id string) error {
	delete(m.covers, id)
	return nil
}

func (m *memCoverRepo) CoverStamps(ids []string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	for _, id := range ids {
		if c, ok := m.covers[id]; ok {
			out[id] = c.UpdatedAt
		}
	}
	return out, nil
}

// coverFixture is workspace org-1 with projects p-1 and p-2, where "editor"
// edits p-1, "viewer" views it, and "stranger" has no role, behind the
// registered routes.
func coverFixture(t *testing.T) (http.Handler, *memCoverRepo) {
	t.Helper()
	store := &fakeProjectService{byID: map[string]*projects.Project{
		"p-1": {ID: "p-1", OrgID: "org-1", Name: "One"},
		"p-2": {ID: "p-2", OrgID: "org-1", Name: "Two"},
	}}
	repo := &memCoverRepo{
		covers: map[string]*projects.Cover{},
		known:  map[string]bool{"p-1": true, "p-2": true},
		clock:  time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC),
	}
	h := NewHandler(HandlerDeps{
		ProjectService:      store,
		ProjectCoverService: projects.NewCoverService(repo),
		OrgService: &fakeOrgService{roles: map[string]map[string]string{
			"org-1": {"editor": orgs.RoleMember, "viewer": orgs.RoleMember, "stranger": orgs.RoleMember},
		}},
		MemberService: &fakeMemberService{roles: map[string]map[string]string{
			"p-1": {"editor": members.RoleEditor, "viewer": members.RoleViewer},
			"p-2": {"editor": members.RoleEditor, "viewer": members.RoleViewer},
		}},
	})
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	return router, repo
}

func coverReq(method, path, userID string, body *bytes.Buffer, contentType string) *http.Request {
	if body == nil {
		body = &bytes.Buffer{}
	}
	r := httptest.NewRequest(method, path, body)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	ctx := context.WithValue(r.Context(), ctxActiveOrg, "org-1")
	if userID != "" {
		ctx = context.WithValue(ctx, ctxUser, &users.User{ID: userID})
	}
	r = r.WithContext(ctx)
	return r
}

func coverUpload(t *testing.T, userID, mimeType string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreatePart(map[string][]string{
		"Content-Disposition": {`form-data; name="file"; filename="cover.bin"`},
		"Content-Type":        {mimeType},
	})
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	part.Write(data)
	mw.Close()
	return coverReq(http.MethodPost, "/api/v1/projects/p-1/cover", userID, &body, mw.FormDataContentType())
}

func serveCover(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// An editor's PNG is stored, the answer is the project stamped with when,
// and the cover is served back as the stored type to a viewer.
func TestProjectCoverUploadThenServe(t *testing.T) {
	h, repo := coverFixture(t)
	png := smallPNG(t)

	w := serveCover(h, coverUpload(t, "editor", "image/png", png))
	if w.Code != http.StatusOK {
		t.Fatalf("upload: status %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	var p projects.Project
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	if p.CoverUpdatedAt == nil || !p.CoverUpdatedAt.Equal(repo.covers["p-1"].UpdatedAt) {
		t.Fatalf("cover_updated_at = %v, want the stored time (body %q)", p.CoverUpdatedAt, w.Body.String())
	}

	w = serveCover(h, coverReq(http.MethodGet, "/api/v1/projects/p-1/cover", "viewer", nil, ""))
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), png) {
		t.Fatalf("get: status %d, %d bytes; want 200 and the PNG", w.Code, w.Body.Len())
	}
	for k, want := range map[string]string{
		"Content-Type":           "image/png",
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "private, max-age=86400",
	} {
		if got := w.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

// The project list and a project's read carry cover_updated_at only for a
// project that has a cover; without one the key is absent, as before.
func TestProjectReadsCarryTheCoverStamp(t *testing.T) {
	h, _ := coverFixture(t)
	if w := serveCover(h, coverUpload(t, "editor", "image/png", smallPNG(t))); w.Code != http.StatusOK {
		t.Fatalf("upload: status %d (body %q)", w.Code, w.Body.String())
	}
	w := serveCover(h, coverReq(http.MethodGet, "/api/v1/projects", "editor", nil, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("list: status %d (body %q)", w.Code, w.Body.String())
	}
	var list []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("listed %d projects, want 2 (body %q)", len(list), w.Body.String())
	}
	for _, p := range list {
		_, has := p["cover_updated_at"]
		if want := p["id"] == "p-1"; has != want {
			t.Errorf("project %v: cover_updated_at present = %v, want %v", p["id"], has, want)
		}
	}
	w = serveCover(h, coverReq(http.MethodGet, "/api/v1/projects/p-2", "editor", nil, ""))
	if bytes.Contains(w.Body.Bytes(), []byte("cover_updated_at")) {
		t.Errorf("a project with no cover names cover_updated_at: %s", w.Body.String())
	}
}

// A viewer cannot change the cover, a stranger finds no project, and an
// upload that is not a raster image of its declared type is refused.
func TestProjectCoverUploadRefusals(t *testing.T) {
	cases := []struct {
		name, user, mime string
		data             []byte
		want             int
	}{
		{"viewer", "viewer", "image/png", nil, http.StatusForbidden},
		{"stranger", "stranger", "image/png", nil, http.StatusNotFound},
		{"signed out", "", "image/png", nil, http.StatusUnauthorized},
		{"svg", "editor", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), http.StatusBadRequest},
		{"text as png", "editor", "image/png", []byte("not an image"), http.StatusBadRequest},
		{"too large", "editor", "image/png", bytes.Repeat([]byte{0}, projects.MaxCoverBytes+1), http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, repo := coverFixture(t)
			data := tc.data
			if data == nil {
				data = smallPNG(t)
			}
			w := serveCover(h, coverUpload(t, tc.user, tc.mime, data))
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d (body %q)", w.Code, tc.want, w.Body.String())
			}
			if len(repo.covers) != 0 {
				t.Fatalf("a refused upload stored a cover")
			}
		})
	}
}

// Removing the cover answers the project without the stamp, and the image
// is then a 404; removing a cover that is not there is no error.
func TestProjectCoverDelete(t *testing.T) {
	h, repo := coverFixture(t)
	if w := serveCover(h, coverUpload(t, "editor", "image/png", smallPNG(t))); w.Code != http.StatusOK {
		t.Fatalf("upload: status %d (body %q)", w.Code, w.Body.String())
	}
	if w := serveCover(h, coverReq(http.MethodDelete, "/api/v1/projects/p-1/cover", "viewer", nil, "")); w.Code != http.StatusForbidden {
		t.Fatalf("viewer delete: status %d, want 403", w.Code)
	}
	for i := 0; i < 2; i++ {
		w := serveCover(h, coverReq(http.MethodDelete, "/api/v1/projects/p-1/cover", "editor", nil, ""))
		if w.Code != http.StatusOK || bytes.Contains(w.Body.Bytes(), []byte("cover_updated_at")) {
			t.Fatalf("delete %d: status %d, body %q; want 200 without cover_updated_at", i+1, w.Code, w.Body.String())
		}
	}
	if len(repo.covers) != 0 {
		t.Fatalf("cover still stored after delete")
	}
	if w := serveCover(h, coverReq(http.MethodGet, "/api/v1/projects/p-1/cover", "viewer", nil, "")); w.Code != http.StatusNotFound {
		t.Fatalf("get after delete: status %d, want 404", w.Code)
	}
}

// coverKeys is the workspace key store: org-1's OpenAI key, when set.
type coverKeys struct {
	providers.KeyService
	key string
}

func (k *coverKeys) RevealKey(orgID, keyEnv string) (string, error) {
	if orgID == "org-1" && keyEnv == projects.CoverKeyEnv {
		return k.key, nil
	}
	return "", nil
}

type coverGen struct {
	img    []byte
	err    error
	prompt string
	key    string
}

func (g *coverGen) Generate(_ context.Context, apiKey, prompt string) ([]byte, error) {
	g.key, g.prompt = apiKey, prompt
	return g.img, g.err
}

// generateFixture is coverFixture with org-1's key store and a stub
// generator behind the cover service.
func generateFixture(t *testing.T, key string, gen *coverGen) (http.Handler, *memCoverRepo) {
	t.Helper()
	store := &fakeProjectService{byID: map[string]*projects.Project{"p-1": {ID: "p-1", OrgID: "org-1", Name: "One"}}}
	repo := &memCoverRepo{covers: map[string]*projects.Cover{}, known: map[string]bool{"p-1": true}, clock: time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)}
	h := NewHandler(HandlerDeps{
		ProjectService:      store,
		ProjectCoverService: projects.NewCoverService(repo).WithGenerator(gen),
		ProviderKeyService:  &coverKeys{key: key},
		OrgService:          &fakeOrgService{roles: map[string]map[string]string{"org-1": {"editor": orgs.RoleMember, "viewer": orgs.RoleMember}}},
		MemberService: &fakeMemberService{roles: map[string]map[string]string{
			"p-1": {"editor": members.RoleEditor, "viewer": members.RoleViewer},
		}},
	})
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	return router, repo
}

func generateReq(user, body string) *http.Request {
	return coverReq(http.MethodPost, "/api/v1/projects/p-1/cover/generate", user, bytes.NewBufferString(body), "application/json")
}

// An editor's prompt is generated with the workspace's key and stored as
// the cover, and the answer is the project stamped with it.
func TestGenerateProjectCover(t *testing.T) {
	gen := &coverGen{img: smallPNG(t)}
	h, repo := generateFixture(t, "sk-workspace", gen)
	w := serveCover(h, generateReq("editor", `{"prompt":"A brake rig, wide shot"}`))
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte("cover_updated_at")) {
		t.Fatalf("status %d, body %q; want 200 with cover_updated_at", w.Code, w.Body.String())
	}
	if gen.key != "sk-workspace" || gen.prompt != "A brake rig, wide shot" {
		t.Errorf("generated with key %q, prompt %q", gen.key, gen.prompt)
	}
	if c := repo.covers["p-1"]; c == nil || c.Mime != "image/png" {
		t.Errorf("stored %+v, want the PNG", c)
	}
}

// Every refusal stores nothing: a viewer (403), a workspace with no key
// (409, saying where to add one), a bad prompt (400) and the provider's
// failure (502, with its reason).
func TestGenerateProjectCoverRefusals(t *testing.T) {
	cases := []struct {
		name, user, key, body string
		err                   error
		want                  int
		says                  string
	}{
		{"viewer", "viewer", "sk", `{"prompt":"x"}`, nil, http.StatusForbidden, ""},
		{"no key", "editor", "", `{"prompt":"x"}`, nil, http.StatusConflict, "Workspace settings"},
		{"empty prompt", "editor", "sk", `{"prompt":"  "}`, nil, http.StatusBadRequest, "1 to 1000"},
		{"long prompt", "editor", "sk", `{"prompt":"` + strings.Repeat("a", projects.MaxCoverPromptRunes+1) + `"}`, nil, http.StatusBadRequest, "1 to 1000"},
		{"provider", "editor", "sk", `{"prompt":"x"}`, fmt.Errorf("%w: rejected by the safety system", projects.ErrCoverProvider), http.StatusBadGateway, "safety system"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gen := &coverGen{img: smallPNG(t), err: tc.err}
			h, repo := generateFixture(t, tc.key, gen)
			w := serveCover(h, generateReq(tc.user, tc.body))
			if w.Code != tc.want || !strings.Contains(w.Body.String(), tc.says) {
				t.Fatalf("status %d, body %q; want %d saying %q", w.Code, w.Body.String(), tc.want, tc.says)
			}
			if len(repo.covers) != 0 {
				t.Fatalf("a refused generation stored a cover")
			}
		})
	}
}

// Generations are limited per workspace: past the burst the answer is 429
// with Retry-After, and the provider is not called.
func TestGenerateProjectCoverIsRateLimited(t *testing.T) {
	gen := &coverGen{img: smallPNG(t)}
	h, _ := generateFixture(t, "sk", gen)
	for i := 0; i < defaultCoverGenBurst; i++ {
		if w := serveCover(h, generateReq("editor", `{"prompt":"x"}`)); w.Code != http.StatusOK {
			t.Fatalf("generation %d: status %d (body %q)", i+1, w.Code, w.Body.String())
		}
	}
	gen.prompt = ""
	w := serveCover(h, generateReq("editor", `{"prompt":"one more"}`))
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("over the burst: status %d, Retry-After %q; want 429 with one", w.Code, w.Header().Get("Retry-After"))
	}
	if gen.prompt != "" {
		t.Errorf("the provider was called past the limit")
	}
}
