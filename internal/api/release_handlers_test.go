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
	if resp.Version != "2026-09-13" || resp.Date != "2026-09-13" || len(resp.Notes) != 1 || resp.Notes[0] != "Shipped" {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Stable != nil || resp.Deployment != "shared" {
		t.Fatalf("stable = %+v, deployment = %q", resp.Stable, resp.Deployment)
	}
	if resp.Markdown != "- Shipped" {
		t.Fatalf("markdown = %q", resp.Markdown)
	}
	// The history is the parsed releases, not the file: the file also holds
	// the Unreleased section, and serving it whole once put work that had not
	// shipped in front of customers.
	if len(resp.Releases) != 2 || resp.Releases[0].Version != "2026-09-13" ||
		resp.Releases[1].Version != "2026-09-12" {
		t.Fatalf("releases = %+v", resp.Releases)
	}

	w = httptest.NewRecorder()
	h.GetRelease(w, releaseReq(""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no session: status = %d", w.Code)
	}
}

const stableNotes = "## 0.3.0\n\n### New features\n\n- n\n\n## 0.2.0\n\nStable channel release since 2026-10-01.\n\n### Bug fixes\n\n- s\n\n## 0.1.0\n\n### New features\n\n- o\n"

// TestGetPublicReleaseFeed: the open feed names the running release and the
// stable with the day it was designated, cacheable, and nothing else.
func TestGetPublicReleaseFeed(t *testing.T) {
	notes, _ := release.Parse(stableNotes)
	h := &Handler{releaseService: staticRelease{notes: notes}}
	w := httptest.NewRecorder()
	h.GetPublicRelease(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/release", nil))
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Fatalf("status %d, cache %q", w.Code, w.Header().Get("Cache-Control"))
	}
	var feed map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &feed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if feed["version"] != "0.3.0" || feed["stable"] != "0.2.0" || feed["stable_since"] != "2026-10-01" || len(feed) != 3 {
		t.Fatalf("feed = %v", feed)
	}
}

// TestGetReleaseCarriesTheStable: a dedicated build answers its kind, and
// the stable release comes with the notes merged since the previous one.
func TestGetReleaseCarriesTheStable(t *testing.T) {
	notes, _ := release.Parse(stableNotes)
	h := &Handler{releaseService: staticRelease{notes: notes}, deploymentKind: "dedicated"}
	w := httptest.NewRecorder()
	h.GetRelease(w, releaseReq("u1"))
	var resp releaseResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Deployment != "dedicated" || resp.Stable == nil || resp.Stable.Version != "0.2.0" || resp.Stable.Since != "2026-10-01" {
		t.Fatalf("resp = %+v", resp)
	}
	if len(resp.Stable.Notes) != 2 || resp.Releases[1].StableSince != "2026-10-01" {
		t.Fatalf("stable = %+v, releases = %+v", resp.Stable, resp.Releases)
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
	if resp.Releases == nil || len(resp.Releases) != 0 {
		t.Fatalf("releases = %+v, want an empty list rather than null", resp.Releases)
	}
}
