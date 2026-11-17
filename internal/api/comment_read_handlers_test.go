package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/commentreads"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

const (
	readArtifactA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	readArtifactB = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

// fakeCommentReadRepo keeps read state by member, workspace and artifact.
// Only readArtifactA belongs to org-1, as the real repository's join on the
// artifact's project decides.
type fakeCommentReadRepo struct {
	rows map[string]time.Time
}

func (f *fakeCommentReadRepo) key(user, org, id string) string { return user + "|" + org + "|" + id }

func (f *fakeCommentReadRepo) List(user, org string, ids []string) ([]commentreads.Read, error) {
	out := []commentreads.Read{}
	for _, id := range ids {
		if at, ok := f.rows[f.key(user, org, id)]; ok {
			out = append(out, commentreads.Read{ArtifactID: id, ReadAt: at})
		}
	}
	return out, nil
}

func (f *fakeCommentReadRepo) Set(user, org string, reads []commentreads.Read) ([]commentreads.Read, error) {
	out := []commentreads.Read{}
	for _, r := range reads {
		if org != "org-1" || r.ArtifactID != readArtifactA {
			continue
		}
		f.rows[f.key(user, org, r.ArtifactID)] = r.ReadAt
		out = append(out, r)
	}
	return out, nil
}

func TestCommentReadsRoundTrip(t *testing.T) {
	repo := &fakeCommentReadRepo{rows: map[string]time.Time{}}
	h := newTestHandler(t, func(h *Handler) {
		h.CommentReadService = commentreads.NewDefaultService(repo)
		h.OrgService = &fakeOrgService{}
	})
	router := mux.NewRouter()
	h.registerCommentReadRoutes(router)
	serve := func(r *http.Request) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	list := func(user, org, ids string) []commentreads.Read {
		t.Helper()
		w := serve(savedSearchReq("GET", "/api/v1/me/comment-reads?artifact_ids="+ids, "", user, org))
		if w.Code != http.StatusOK {
			t.Fatalf("list = %d %s", w.Code, w.Body.String())
		}
		var body struct {
			Reads []commentreads.Read `json:"comment_reads"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body.Reads == nil {
			t.Fatalf("list body %s: want an array", w.Body.String())
		}
		return body.Reads
	}

	for _, r := range []*http.Request{
		savedSearchReq("GET", "/api/v1/me/comment-reads?artifact_ids="+readArtifactA, "", "", "org-1"),
		savedSearchReq("PUT", "/api/v1/me/comment-reads", `{"reads":[{"artifact_id":"`+readArtifactA+`"}]}`, "", "org-1"),
	} {
		if w := serve(r); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s signed out = %d, want 401", r.Method, w.Code)
		}
	}
	if got := list("dana", "org-1", readArtifactA); len(got) != 0 {
		t.Fatalf("nothing read yet: %+v", got)
	}

	// Mark read: no read_at is now. The other workspace's artifact is skipped.
	before := time.Now().UTC().Add(-time.Second)
	w := serve(savedSearchReq("PUT", "/api/v1/me/comment-reads",
		`{"reads":[{"artifact_id":"`+readArtifactA+`"},{"artifact_id":"`+readArtifactB+`"}]}`, "dana", "org-1"))
	if w.Code != http.StatusOK {
		t.Fatalf("mark read = %d %s", w.Code, w.Body.String())
	}
	got := list("dana", "org-1", readArtifactA+","+readArtifactB)
	if len(got) != 1 || got[0].ArtifactID != readArtifactA || got[0].ReadAt.Before(before) {
		t.Fatalf("after mark read: %+v", got)
	}
	if other := list("sam", "org-1", readArtifactA); len(other) != 0 {
		t.Fatalf("another member's read state leaked: %+v", other)
	}

	// Mark unread: an earlier time moves the read state back.
	back := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	w = serve(savedSearchReq("PUT", "/api/v1/me/comment-reads",
		`{"reads":[{"artifact_id":"`+readArtifactA+`","read_at":"2026-01-02T03:04:05Z"}]}`, "dana", "org-1"))
	if w.Code != http.StatusOK {
		t.Fatalf("mark unread = %d", w.Code)
	}
	if got := list("dana", "org-1", readArtifactA); len(got) != 1 || !got[0].ReadAt.Equal(back) {
		t.Fatalf("after mark unread: %+v", got)
	}

	for name, body := range map[string]string{
		"no reads": `{"reads":[]}`,
		"bad id":   `{"reads":[{"artifact_id":"nope"}]}`,
		"not json": `{`,
	} {
		if w := serve(savedSearchReq("PUT", "/api/v1/me/comment-reads", body, "dana", "org-1")); w.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", name, w.Code)
		}
	}
	if w := serve(savedSearchReq("PUT", "/api/v1/me/comment-reads", `{"reads":[{"artifact_id":"`+readArtifactA+`"}]}`, "dana", "")); w.Code != http.StatusBadRequest {
		t.Errorf("no active workspace = %d, want 400", w.Code)
	}
}

func TestMarkingCommentsReadIsGatedOnTheFeature(t *testing.T) {
	repo := &fakeCommentReadRepo{rows: map[string]time.Time{}}
	h := newTestHandler(t, func(h *Handler) {
		h.CommentReadService = commentreads.NewDefaultService(repo)
		h.OrgService = &fakeOrgService{plan: orgs.PlanTeam, stableRelease: "0.9.2"}
	})
	w := httptest.NewRecorder()
	h.SetCommentReads(w, savedSearchReq("PUT", "/api/v1/me/comment-reads", `{"reads":[{"artifact_id":"`+readArtifactA+`"}]}`, "dana", "org-1"))
	if w.Code != http.StatusForbidden || len(repo.rows) != 0 {
		t.Fatalf("mark read on a stable channel without the feature = %d (%d rows), want 403", w.Code, len(repo.rows))
	}
	w = httptest.NewRecorder()
	h.ListCommentReads(w, savedSearchReq("GET", "/api/v1/me/comment-reads?artifact_ids="+readArtifactA, "", "dana", "org-1"))
	if w.Code != http.StatusOK {
		t.Fatalf("reading is never gated: %d", w.Code)
	}
}
