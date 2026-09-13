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
	"github.com/openv/requirements-platform/internal/domain/users"
)

func channelReq(t *testing.T, userID, orgID, body string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPut, "/api/v1/orgs/"+orgID, strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: userID}))
	return mux.SetURLVars(r, map[string]string{"id": orgID})
}

// TestUpdateOrgReleaseChannel: a business admin moves the workspace to
// nightly and back to the plan default; a personal-tier workspace is locked
// (400); an unknown channel is 400; a member is 403; an absent key changes
// nothing.
func TestUpdateOrgReleaseChannel(t *testing.T) {
	newHandler := func(plan string) (*Handler, *fakeOrgService) {
		svc := &fakeOrgService{
			plan:  plan,
			roles: map[string]map[string]string{"org-1": {"admin": orgs.RoleAdmin, "member": orgs.RoleMember}},
		}
		return &Handler{orgService: svc}, svc
	}

	h, svc := newHandler(orgs.PlanBusiness)
	w := httptest.NewRecorder()
	h.UpdateOrg(w, channelReq(t, "admin", "org-1", `{"release_channel":"nightly"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body %q)", w.Code, w.Body.String())
	}
	var o orgs.Org
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if o.ReleaseChannel != orgs.ChannelNightly || o.ReleaseChannelLocked {
		t.Fatalf("answer = %+v", o)
	}
	w = httptest.NewRecorder()
	h.UpdateOrg(w, channelReq(t, "admin", "org-1", `{"release_channel":""}`))
	if w.Code != http.StatusOK || len(svc.channelCalls) != 2 || svc.channelCalls[1] != "" {
		t.Fatalf("reset: status %d, calls %v", w.Code, svc.channelCalls)
	}

	w = httptest.NewRecorder()
	h.UpdateOrg(w, channelReq(t, "admin", "org-1", `{"release_channel":"beta"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown channel: status = %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.UpdateOrg(w, channelReq(t, "member", "org-1", `{"release_channel":"nightly"}`))
	if w.Code != http.StatusForbidden {
		t.Fatalf("member: status = %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.UpdateOrg(w, channelReq(t, "admin", "org-1", `{"name":"Renamed"}`))
	if w.Code != http.StatusOK || len(svc.channelCalls) != 2 {
		t.Fatalf("absent key: status %d, calls %v", w.Code, svc.channelCalls)
	}

	h, svc = newHandler(orgs.PlanSingle)
	w = httptest.NewRecorder()
	h.UpdateOrg(w, channelReq(t, "admin", "org-1", `{"release_channel":"stable"}`))
	if w.Code != http.StatusBadRequest || len(svc.channelCalls) != 0 {
		t.Fatalf("locked plan: status %d, calls %v", w.Code, svc.channelCalls)
	}
}
