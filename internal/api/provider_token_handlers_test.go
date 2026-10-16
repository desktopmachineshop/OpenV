package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/providers/usertokens"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/workerproto"
)

type memTokenRepo struct{ rows map[string]*usertokens.Token }

func (m *memTokenRepo) Get(u, p string) (*usertokens.Token, error) { return m.rows[u+"/"+p], nil }
func (m *memTokenRepo) Upsert(t *usertokens.Token) error {
	m.rows[t.UserID+"/"+t.Provider] = t
	return nil
}
func (m *memTokenRepo) Delete(u, p string) error { delete(m.rows, u+"/"+p); return nil }

const testSubscriptionToken = "sk-ant-oat01-0123456789abcdefghijklmnopqrstuvwxyz-WXYZ"

func newTokenService(t *testing.T) *usertokens.DefaultService {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	s, err := usertokens.NewDefaultService(&memTokenRepo{rows: map[string]*usertokens.Token{}}, base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestProviderTokenIsSavedButNeverAnsweredBack(t *testing.T) {
	svc := newTokenService(t)
	h := newTestHandler(t, func(h *Handler) { h.ProviderTokenService = svc })
	router := mux.NewRouter()
	h.registerProviderTokenRoutes(router)
	user := &users.User{ID: "u-1"}
	do := func(method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/me/provider-tokens/claude-code", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), ctxUser, user))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	if rec := do("PUT", `{"token":"not-a-token"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad token: %d %s", rec.Code, rec.Body)
	}
	rec := do("PUT", `{"token":"`+testSubscriptionToken+`"}`)
	if rec.Code != http.StatusOK || bytes.Contains(rec.Body.Bytes(), []byte("sk-ant")) {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	rec = do("GET", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"connected":true`) ||
		!strings.Contains(rec.Body.String(), `"last4":"WXYZ"`) || strings.Contains(rec.Body.String(), "sk-ant") {
		t.Fatalf("status: %d %s", rec.Code, rec.Body)
	}
	if rec := do("DELETE", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec := do("GET", ""); !strings.Contains(rec.Body.String(), `"connected":false`) {
		t.Fatalf("after delete: %s", rec.Body)
	}
}

func TestClaimCarriesTheTokenOnlyToTheLaunchersOwnRunner(t *testing.T) {
	svc := newTokenService(t)
	if _, err := svc.Save("u-1", "claude-code", testSubscriptionToken); err != nil {
		t.Fatal(err)
	}
	h := newTestHandler(t, func(h *Handler) { h.ProviderTokenService = svc })
	launcher := "u-1"
	run := &agentruns.Run{ID: "run-1", OrgID: "org-1", LaunchedBy: &launcher}
	claude := &agents.Agent{Provider: "claude-code"}
	claimBy := func(workerUser string) *http.Request {
		req := httptest.NewRequest("POST", "/api/v1/agent-runs/claim", nil)
		if workerUser != "" {
			req = req.WithContext(context.WithValue(req.Context(), ctxWorkerUser, workerUser))
		}
		return req
	}
	got := h.resolveRunAuth(claimBy("u-1"), run, claude)
	if got["mode"] != workerproto.AuthSubscriptionToken || got["oauth_token"] != testSubscriptionToken {
		t.Fatalf("own runner: %v", got)
	}
	for _, other := range []string{"", "u-2"} { // a workspace key; another member's runner
		if got := h.resolveRunAuth(claimBy(other), run, claude); got["mode"] != "user-account" || got["oauth_token"] != "" {
			t.Fatalf("runner %q got %v", other, got)
		}
	}
	if got := h.resolveRunAuth(claimBy("u-1"), run, &agents.Agent{Provider: "codex-cli"}); got["oauth_token"] != "" {
		t.Fatalf("codex run got the Claude token: %v", got)
	}
}
