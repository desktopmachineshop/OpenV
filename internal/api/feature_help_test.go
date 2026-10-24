package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/help"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// fakeDocs is an OpenD docs site that answers every help request with the
// organization its embed token names, so a test can see which profile a
// workspace read.
func fakeDocs(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/api/entitlements/sync" {
			w.WriteHeader(http.StatusOK)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
		raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
		var claims struct {
			Org string `json:"org"`
		}
		_ = json.Unmarshal(raw, &claims)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key": r.URL.Query().Get("key"), "via": "key", "topicId": "topics/vv/index",
			"title": claims.Org, "shortdesc": "", "html": "<p>x</p>", "related": []any{},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func helpReq(t *testing.T, userID, query string) *http.Request {
	ctx := featuresReq(t, http.MethodGet, userID, "").Context()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/org-1/help/context?"+query, nil).WithContext(ctx)
	return mux.SetURLVars(r, map[string]string{"id": "org-1"})
}

// TestHelpContextReadsTheWorkspacesDocsLine: a stable business workspace
// on 0.2.0 reads the 0.2 business docs; a nightly personal workspace reads
// the docs of the release the server runs, 0.3, for the free plan.
func TestHelpContextReadsTheWorkspacesDocsLine(t *testing.T) {
	docs := fakeDocs(t)
	for _, tc := range []struct{ plan, stable, want string }{
		{orgs.PlanBusiness, "0.2.0", "openv-0-2-business"},
		{orgs.PlanSingle, "", "openv-0-3-free"},
	} {
		h, _ := featureFixture(t, tc.plan, tc.stable)
		h.HelpService = help.NewService(help.Config{URL: docs.URL, Secret: "s", AdminToken: "a"}, nil)
		w := httptest.NewRecorder()
		h.GetHelpContext(w, helpReq(t, "member", "key=vv"))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", tc.plan, w.Code, w.Body.String())
		}
		var topic help.Topic
		_ = json.Unmarshal(w.Body.Bytes(), &topic)
		if topic.Title != tc.want {
			t.Errorf("%s on %q read %q, want %q", tc.plan, tc.stable, topic.Title, tc.want)
		}
	}
}

// TestHelpIsOffWithoutADocsSite: no docs site configured answers 404, and
// a caller outside the workspace is refused before that.
func TestHelpIsOffWithoutADocsSite(t *testing.T) {
	h, _ := featureFixture(t, orgs.PlanBusiness, "0.2.0")
	w := httptest.NewRecorder()
	h.GetHelpContext(w, helpReq(t, "member", "key=vv"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	w = httptest.NewRecorder()
	h.GetHelpContext(w, helpReq(t, "stranger", "key=vv"))
	if w.Code == http.StatusOK || w.Code == http.StatusNotFound && strings.Contains(w.Body.String(), "help is not configured") {
		t.Fatalf("a stranger got past the membership check: %d %s", w.Code, w.Body.String())
	}
}

func TestHelpContextNeedsKeyOrPath(t *testing.T) {
	h, _ := featureFixture(t, orgs.PlanBusiness, "0.2.0")
	h.HelpService = help.NewService(help.Config{URL: fakeDocs(t).URL, Secret: "s"}, nil)
	w := httptest.NewRecorder()
	h.GetHelpContext(w, helpReq(t, "member", ""))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
