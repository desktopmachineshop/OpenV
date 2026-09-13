package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/users"
)

func releaseReq(userID string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/release", nil)
	if userID != "" {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: userID}))
	}
	return r
}

// TestGetReleaseAnswersCurrentAndHistory: the current section's version,
// bullets and markdown, plus the whole file, uncached; no session is 401.
func TestGetReleaseAnswersCurrentAndHistory(t *testing.T) {
	notes, err := release.Parse("## Unreleased\n\n- soon\n\n## 2026-09-13\n\n- Shipped\n\n## 2026-09-12\n\n- Older\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	h := &Handler{releaseService: staticRelease{notes: notes}}
	w := httptest.NewRecorder()
	h.GetRelease(w, releaseReq("u1"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body %q)", w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q", cc)
	}
	var resp releaseResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Version != "2026-09-13" || resp.Date != "2026-09-13" || len(resp.Notes) != 1 || resp.Notes[0].Text != "Shipped" {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Markdown != "- Shipped" || resp.History != notes.Markdown {
		t.Fatalf("markdown = %q, history matches file: %v", resp.Markdown, resp.History == notes.Markdown)
	}

	w = httptest.NewRecorder()
	h.GetRelease(w, releaseReq(""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no session: status = %d", w.Code)
	}
}

// TestGetReleaseWithoutARelease: a build whose notes have no dated section
// answers empty fields and an empty notes list, never null.
func TestGetReleaseWithoutARelease(t *testing.T) {
	notes, _ := release.Parse("## Unreleased\n")
	h := &Handler{releaseService: staticRelease{notes: notes}}
	w := httptest.NewRecorder()
	h.GetRelease(w, releaseReq("u1"))
	var resp releaseResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Version != "" || resp.Notes == nil || len(resp.Notes) != 0 {
		t.Fatalf("resp = %+v", resp)
	}
}
