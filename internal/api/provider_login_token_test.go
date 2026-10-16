package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/providers/usertokens"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// memLoginRepo keeps login requests in memory, claiming as the SQL does.
type memLoginRepo struct {
	rows map[string]*providers.LoginRequest
}

func (m *memLoginRepo) SaveLogin(l *providers.LoginRequest) error {
	c := *l
	m.rows[l.ID] = &c
	return nil
}
func (m *memLoginRepo) UpdateLogin(l *providers.LoginRequest) error { return m.SaveLogin(l) }
func (m *memLoginRepo) FindLoginByID(id string) (*providers.LoginRequest, error) {
	if l, ok := m.rows[id]; ok {
		c := *l
		return &c, nil
	}
	return nil, nil
}
func (m *memLoginRepo) FindActiveLogin(orgID, provider, target, purpose string, userID *string) (*providers.LoginRequest, error) {
	return nil, nil
}
func (m *memLoginRepo) ClaimPendingLogin(orgID, workerUserID string, tokenLogins bool) (*providers.LoginRequest, error) {
	for _, l := range m.rows {
		if l.OrgID != orgID || l.Status != providers.LoginPending {
			continue
		}
		if l.Target == providers.LoginTargetUser && (l.RequestedBy == nil || *l.RequestedBy != workerUserID) {
			continue
		}
		if l.Purpose == providers.LoginPurposeToken && !tokenLogins {
			continue
		}
		l.Status = providers.LoginClaimed
		c := *l
		return &c, nil
	}
	return nil, nil
}

func tokenLoginFixture(t *testing.T) (*mux.Router, *memLoginRepo, *usertokens.DefaultService) {
	t.Helper()
	logins := &memLoginRepo{rows: map[string]*providers.LoginRequest{}}
	tokens := newTokenService(t)
	h := newTestHandler(t, func(h *Handler) {
		h.LoginService = providers.NewLoginService(logins)
		h.ProviderTokenService = tokens
		h.OrgService = memberOrgService()
	})
	router := mux.NewRouter()
	h.registerProviderLoginRoutes(router)
	return router, logins, tokens
}

func asMember(r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: "user-1"})
	return r.WithContext(context.WithValue(ctx, ctxActiveOrg, "org-1"))
}

func asRunnerOf(user string, r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), ctxWorkerOrg, "org-1")
	if user != "" {
		ctx = context.WithValue(ctx, ctxWorkerUser, user)
	}
	return r.WithContext(ctx)
}

func send(router *mux.Router, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, r)
	return rec
}

// A member's token request goes only to their own runner, and only to one
// that says it can mint; the token that runner hands in is saved for the
// member, and nobody else's runner can hand one in.
func TestACloudRunnerMintsTheMembersToken(t *testing.T) {
	router, logins, tokens := tokenLoginFixture(t)
	rec := send(router, asMember(httptest.NewRequest("POST", "/api/v1/provider-logins",
		strings.NewReader(`{"provider":"claude-code","target":"user","purpose":"token"}`))))
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"purpose":"token"`) {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	var id string
	for k := range logins.rows {
		id = k
	}

	// An older runner (no body) and a runner that cannot mint get nothing.
	if rec := send(router, asRunnerOf("user-1", httptest.NewRequest("POST", "/api/v1/provider-logins/claim", nil))); rec.Code != http.StatusNoContent {
		t.Fatalf("old runner claimed: %d %s", rec.Code, rec.Body)
	}
	if rec := send(router, asRunnerOf("user-1", httptest.NewRequest("POST", "/api/v1/provider-logins/claim",
		strings.NewReader(`{"token_logins":false}`)))); rec.Code != http.StatusNoContent {
		t.Fatalf("non-minting runner claimed: %d", rec.Code)
	}
	if rec := send(router, asRunnerOf("user-1", httptest.NewRequest("POST", "/api/v1/provider-logins/claim",
		strings.NewReader(`{"token_logins":true}`)))); rec.Code != http.StatusOK {
		t.Fatalf("minting runner did not claim: %d %s", rec.Code, rec.Body)
	}

	body := `{"token":"` + testSubscriptionToken + `"}`
	for _, other := range []string{"", "user-2"} { // a workspace key; another member's runner
		if rec := send(router, asRunnerOf(other, httptest.NewRequest("POST", "/api/v1/provider-logins/"+id+"/token",
			strings.NewReader(body)))); rec.Code != http.StatusConflict {
			t.Fatalf("runner %q handed a token in: %d", other, rec.Code)
		}
	}
	rec = send(router, asRunnerOf("user-1", httptest.NewRequest("POST", "/api/v1/provider-logins/"+id+"/token",
		strings.NewReader(body))))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "sk-ant") ||
		!strings.Contains(rec.Body.String(), `"status":"completed"`) {
		t.Fatalf("token: %d %s", rec.Code, rec.Body)
	}
	if st, _ := tokens.Status("user-1", "claude-code"); !st.Connected {
		t.Fatal("the minted token was not saved for the member")
	}
	// The request is done: a second token is refused.
	if rec := send(router, asRunnerOf("user-1", httptest.NewRequest("POST", "/api/v1/provider-logins/"+id+"/token",
		strings.NewReader(body)))); rec.Code != http.StatusConflict {
		t.Fatalf("a finished request took another token: %d", rec.Code)
	}
}

// An ordinary sign-in cannot be finished by handing a token in.
func TestAnOrdinarySignInTakesNoToken(t *testing.T) {
	router, logins, _ := tokenLoginFixture(t)
	if rec := send(router, asMember(httptest.NewRequest("POST", "/api/v1/provider-logins",
		strings.NewReader(`{"provider":"claude-code","target":"user"}`)))); rec.Code != http.StatusCreated {
		t.Fatalf("start: %d", rec.Code)
	}
	var id string
	for k := range logins.rows {
		id = k
	}
	if rec := send(router, asRunnerOf("user-1", httptest.NewRequest("POST", "/api/v1/provider-logins/"+id+"/token",
		strings.NewReader(`{"token":"`+testSubscriptionToken+`"}`)))); rec.Code != http.StatusConflict {
		t.Fatalf("an ordinary sign-in took a token: %d", rec.Code)
	}
}
