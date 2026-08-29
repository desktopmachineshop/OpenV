package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/embeddings"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// requestWithProjectVar attaches the {id} path var and a platform-admin user so
// requireProjectRole passes without a full member/org fixture.
func requestWithProjectVar(projectID string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/duplicates", nil)
	r = mux.SetURLVars(r, map[string]string{"id": projectID})
	return r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "admin", IsAdmin: true}))
}

// TestDuplicateCandidatesDisabled locks in that the endpoint answers cleanly
// (200, enabled=false, empty pairs, a note) rather than erroring when semantic
// embeddings are not configured — both when the service is nil and when it is
// present but disabled.
func TestDuplicateCandidatesDisabled(t *testing.T) {
	decode := func(t *testing.T, w *httptest.ResponseRecorder) duplicatesResponse {
		t.Helper()
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		var resp duplicatesResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v (body %q)", err, w.Body.String())
		}
		return resp
	}

	t.Run("nil service", func(t *testing.T) {
		h := &Handler{}
		w := httptest.NewRecorder()
		h.DuplicateCandidates(w, requestWithProjectVar("proj-1"))
		resp := decode(t, w)
		if resp.Enabled {
			t.Fatalf("enabled = true, want false")
		}
		if resp.Note == "" {
			t.Errorf("expected an explanatory note")
		}
		if resp.Pairs == nil || len(resp.Pairs) != 0 {
			t.Errorf("pairs = %+v, want empty non-nil slice", resp.Pairs)
		}
	})

	t.Run("disabled service", func(t *testing.T) {
		disabled := embeddings.NewService(fakeEmbedProvider{enabled: false}, &fakeEmbedStore{}, nil)
		h := &Handler{embeddingService: disabled}
		w := httptest.NewRecorder()
		h.DuplicateCandidates(w, requestWithProjectVar("proj-1"))
		resp := decode(t, w)
		if resp.Enabled {
			t.Fatalf("enabled = true, want false for a disabled service")
		}
	})
}
