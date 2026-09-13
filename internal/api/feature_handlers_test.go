package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// featureFixture: a business workspace whose turned-on stable is 2026.09
// (cut from 2026-09-12.2), one gated feature shipped before that nightly and
// one after, and a notes file that also names 2026.10 as the newest stable.
func featureFixture(t *testing.T, plan, stableRelease string) (*Handler, *fakeOrgService) {
	t.Helper()
	saved := release.Registry
	release.Registry = []release.Feature{
		{Key: "old", ShippedIn: "2026-09-10"},
		{Key: "new", ShippedIn: "2026-09-20"},
	}
	t.Cleanup(func() { release.Registry = saved })
	notes, err := release.Parse("## 2026.10\n\nCut on 2026-11-02 from 2026-09-25.\n\n- x\n\n## 2026.09\n\nCut on 2026-10-01 from 2026-09-12.2.\n\n- y\n\n## 2026-09-25\n\n- z\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	svc := &fakeOrgService{
		plan:          plan,
		stableRelease: stableRelease,
		roles:         map[string]map[string]string{"org-1": {"admin": orgs.RoleAdmin, "member": orgs.RoleMember}},
		previews:      map[string]bool{},
	}
	return &Handler{orgService: svc, releaseService: staticRelease{notes: notes}}, svc
}

func featuresReq(t *testing.T, method, userID, body string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, "/api/v1/orgs/org-1/features", strings.NewReader(body))
	if userID != "" {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: userID}))
	}
	return mux.SetURLVars(r, map[string]string{"id": "org-1"})
}

func decodeFeatures(t *testing.T, w *httptest.ResponseRecorder) featuresResponse {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body %q)", w.Code, w.Body.String())
	}
	var resp featuresResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

// TestOrgFeaturesByChannel: a stable workspace on 2026.09 sees the feature
// shipped before its cut and not the later one; a nightly workspace sees
// both; a stable workspace with no release yet sees neither.
func TestOrgFeaturesByChannel(t *testing.T) {
	h, _ := featureFixture(t, orgs.PlanBusiness, "2026.09")
	w := httptest.NewRecorder()
	h.GetOrgFeatures(w, featuresReq(t, http.MethodGet, "member", ""))
	resp := decodeFeatures(t, w)
	if resp.Channel != "stable" || resp.StableRelease != "2026.09" || !resp.Features["old"] || resp.Features["new"] {
		t.Fatalf("stable 2026.09: %+v", resp)
	}

	h, _ = featureFixture(t, orgs.PlanSingle, "")
	w = httptest.NewRecorder()
	h.GetOrgFeatures(w, featuresReq(t, http.MethodGet, "member", ""))
	resp = decodeFeatures(t, w)
	if resp.Channel != "nightly" || !resp.Features["old"] || !resp.Features["new"] {
		t.Fatalf("nightly: %+v", resp)
	}

	h, _ = featureFixture(t, orgs.PlanBusiness, "")
	w = httptest.NewRecorder()
	h.GetOrgFeatures(w, featuresReq(t, http.MethodGet, "member", ""))
	resp = decodeFeatures(t, w)
	if resp.Features["old"] || resp.Features["new"] {
		t.Fatalf("stable with no release: %+v", resp)
	}

	w = httptest.NewRecorder()
	h.GetOrgFeatures(w, featuresReq(t, http.MethodGet, "stranger", ""))
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-member: status = %d", w.Code)
	}
}

// TestStablePreviewSwitchesTheCallerEarly: turning the preview on resolves
// the caller's gates against the newest stable (2026.10, cut from
// 2026-09-25, so "new" opens) while the workspace stays on 2026.09; a
// single-plan workspace has nothing to preview.
func TestStablePreviewSwitchesTheCallerEarly(t *testing.T) {
	h, svc := featureFixture(t, orgs.PlanBusiness, "2026.09")
	w := httptest.NewRecorder()
	h.SetMyStablePreview(w, featuresReq(t, http.MethodPut, "member", `{"enabled":true}`))
	resp := decodeFeatures(t, w)
	if !resp.Preview || resp.StableRelease != "2026.10" || !resp.Features["new"] {
		t.Fatalf("preview on: %+v", resp)
	}
	if !svc.previews["org-1/member"] {
		t.Fatalf("preview not recorded")
	}
	w = httptest.NewRecorder()
	h.GetOrgFeatures(w, featuresReq(t, http.MethodGet, "admin", ""))
	if resp := decodeFeatures(t, w); resp.Preview || resp.Features["new"] {
		t.Fatalf("another member inherited the preview: %+v", resp)
	}
	w = httptest.NewRecorder()
	h.SetMyStablePreview(w, featuresReq(t, http.MethodPut, "member", `{"enabled":false}`))
	if resp := decodeFeatures(t, w); resp.Preview || resp.StableRelease != "2026.09" {
		t.Fatalf("preview off: %+v", resp)
	}

	h, _ = featureFixture(t, orgs.PlanSingle, "")
	w = httptest.NewRecorder()
	h.SetMyStablePreview(w, featuresReq(t, http.MethodPut, "member", `{"enabled":true}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("locked plan: status = %d", w.Code)
	}
}
